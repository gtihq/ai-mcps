package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	playwright "github.com/mxschmitt/playwright-go"
)

const (
	// WAIT_SECONDS is how long to wait for a page to show results or a known state.
	WAIT_SECONDS = 20
	// FX_CONCURRENCY bounds the parallel FxTwitter requests.
	FX_CONCURRENCY = 5
)

// RETRY_BACKOFF holds the waits before attempts 2 and 3 on transient failures.
var RETRY_BACKOFF = []float64{5, 15}

// Elements that can carry over from the previous page during in-app navigation.
const staleSelectors = `[data-testid="tweet"], [data-testid="emptyState"], [data-testid="trend"], [data-testid="UserName"]`

const domStateJS = `(ready) => {
  const fresh = s => [...document.querySelectorAll(s)].filter(e => !e.hasAttribute('data-xmcp-stale')).length;
  const col = document.querySelector('[data-testid="primaryColumn"]');
  const text = col ? col.innerText : '';
  return {
    url: location.href,
    ready: fresh(ready) > 0,
    empty: fresh('[data-testid="emptyState"]') > 0,
    login: !!document.querySelector('input[autocomplete="username"], [data-testid="loginButton"]') && !col,
    error: /Something went wrong|Try reloading|Rate limit exceeded/i.test(text),
    not_found: /This account doesn.t exist|Account suspended|Hmm\.\.\.this page doesn.t exist/i.test(text),
  };
}`

const extractTweetsJS = `() => [...document.querySelectorAll('[data-testid="tweet"]:not([data-xmcp-stale])')].map(el => {
  const q = s => el.querySelector(s);
  const label = s => (q(s) && q(s).getAttribute('aria-label')) || '';
  const userName = q('[data-testid="User-Name"]');
  let handle = '';
  if (userName) {
    for (const sp of userName.querySelectorAll('span')) {
      const t = sp.innerText.trim();
      if (t.startsWith('@')) { handle = t; break; }
    }
  }
  const time = q('time');
  const link = (time && time.closest('a')) || q('a[href*="/status/"]');
  const href = (link && link.getAttribute('href')) || '';
  return {
    name: userName ? ((userName.querySelector('span') || {}).innerText || '') : '',
    handle,
    text: (q('[data-testid="tweetText"]') || {}).innerText || '',
    time: time ? time.getAttribute('datetime') : '',
    reply: label('[data-testid="reply"]'),
    retweet: label('[data-testid="retweet"], [data-testid="unretweet"]'),
    like: label('[data-testid="like"], [data-testid="unlike"]'),
    href,
  };
})`

const appLoadedJS = `() => !!document.querySelector('[data-testid="primaryColumn"]')`

const inAppNavJS = `([url, stale]) => {
  document.querySelectorAll(stale).forEach(e => e.setAttribute('data-xmcp-stale', ''));
  history.pushState({}, '', url);
  window.dispatchEvent(new PopStateEvent('popstate', {state: {}}));
}`

const profileJS = `() => {
    const txt = s => { const e = document.querySelector(s); return e ? e.innerText : ''; };
    const nameEl = document.querySelector('[data-testid="UserName"] span');
    return {
        name: nameEl ? nameEl.innerText : '',
        bio: txt('[data-testid="UserDescription"]'),
        followers: txt('a[href$="/verified_followers"] span span') || txt('a[href$="/followers"] span span'),
        following: txt('a[href$="/following"] span span'),
    };
}`

const trendTextsJS = `() => [...document.querySelectorAll('[data-testid="trend"]:not([data-xmcp-stale])')].map(e => e.innerText)`

var (
	statusIDRE   = regexp.MustCompile(`/status/(\d+)`)
	countRE      = regexp.MustCompile(`^\s*([\d.,]+[KMB]?)`)
	digitsOnlyRE = regexp.MustCompile(`^\d+$`)
	postRE       = regexp.MustCompile(`(?i)post`)
	fxClient     = &http.Client{Timeout: 6 * time.Second}
)

// tweet mirrors the dict built by _EXTRACT_TWEETS_JS plus the fields added by
// _with_stats.
type tweet struct {
	Name     string `json:"name"`
	Handle   string `json:"handle"`
	Text     string `json:"text"`
	Time     string `json:"time"`
	Reply    string `json:"reply"`
	Retweet  string `json:"retweet"`
	Like     string `json:"like"`
	Href     string `json:"href"`
	ID       string `json:"id"`
	Replies  string `json:"replies"`
	Retweets string `json:"retweets"`
	Likes    string `json:"likes"`
	Views    string `json:"views"`
	URL      string `json:"url"`
}

type profileData struct {
	Name      string `json:"name"`
	Bio       string `json:"bio"`
	Followers string `json:"followers"`
	Following string `json:"following"`
}

// responseCollector records X API responses. Playwright delivers events on
// its own goroutines, so access is guarded by a mutex, like Python's list
// read from the event loop.
type responseCollector struct {
	mu    sync.Mutex
	items []XResponse
}

func (c *responseCollector) add(r playwright.Response) {
	url := r.URL()
	if !strings.Contains(url, "/i/api/") {
		return
	}
	op := strings.SplitN(url, "?", 2)[0]
	op = strings.TrimRight(op, "/")
	parts := strings.Split(op, "/")
	op = parts[len(parts)-1]
	var reset any
	if header, err := r.HeaderValue("x-rate-limit-reset"); err == nil && header != "" {
		reset = header
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = append(c.items, XResponse{Op: op, Status: r.Status(), Reset: reset})
}

func (c *responseCollector) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = nil
}

func (c *responseCollector) snapshot() []XResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]XResponse, len(c.items))
	copy(out, c.items)
	return out
}

// decodeValue converts a value returned by page.Evaluate into a typed value.
func decodeValue(v any, out any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func evalBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func appLoaded(page playwright.Page) bool {
	if !strings.HasPrefix(page.URL(), "https://x.com/") {
		return false
	}
	value, err := page.Evaluate(appLoadedJS)
	if err != nil {
		return false
	}
	return evalBool(value)
}

// goTo navigates to url. Inside a loaded x.com app it uses in-app navigation,
// because a full page load re-fetches the rate-limited Viewer API.
func goTo(page playwright.Page, url string, fullReload bool) error {
	if !fullReload && appLoaded(page) {
		_, err := page.Evaluate(inAppNavJS, []any{url, staleSelectors})
		return err
	}
	_, err := page.Goto(url, playwright.PageGotoOptions{
		Timeout:   playwright.Float(30000),
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	if err != nil && strings.Contains(err.Error(), "Timeout") {
		// The state wait below decides what happened.
		return nil
	}
	return err
}

func waitState(page playwright.Page, ready string, responses *responseCollector, dataOps []string) string {
	deadline := time.Now().Add(WAIT_SECONDS * time.Second)
	for {
		final := !time.Now().Before(deadline)
		dom := DOMState{}
		if value, err := page.Evaluate(domStateJS, ready); err == nil {
			_ = decodeValue(value, &dom)
		}
		if state := classify(dom, responses.snapshot(), dataOps, final); state != "" {
			return state
		}
		time.Sleep(400 * time.Millisecond)
	}
}

// openPage opens url on the shared tab and waits until it shows results. It
// returns nil with no error when X shows an empty result.
func openPage(url, ready string, dataOps []string) (playwright.Page, error) {
	page, err := session.Page()
	if err != nil {
		return nil, err
	}
	responses := &responseCollector{}
	onResponse := func(r playwright.Response) { responses.add(r) }
	page.OnResponse(onResponse)
	defer func() {
		page.RemoveListener("response", onResponse)
		session.Save()
	}()

	attempts := len(RETRY_BACKOFF) + 1
	for attempt := 0; attempt < attempts; attempt++ {
		session.Pace()
		responses.clear()
		if err := goTo(page, url, attempt > 0); err != nil {
			return nil, err
		}
		state := waitState(page, ready, responses, dataOps)
		switch state {
		case "ready":
			return page, nil
		case "empty":
			return nil, nil
		case RATE_LIMITED:
			seen := map[string]bool{}
			for _, r := range responses.snapshot() {
				if r.Status == 429 {
					seen[r.Op] = true
				}
			}
			ops := make([]string, 0, len(seen))
			for op := range seen {
				ops = append(ops, op)
			}
			sortStrings(ops)
			detail := "HTTP 429 on " + strings.Join(ops, ", ")
			resetAt, _ := rateLimitReset(responses.snapshot())
			session.markRateLimited(resetAt, detail)
			return nil, &TwitterError{Kind: RATE_LIMITED, Detail: fmt.Sprintf("X rate limited this account (%s).", detail), ResetAt: resetAt}
		}
		if RETRYABLE[state] && attempt < attempts-1 {
			time.Sleep(time.Duration(RETRY_BACKOFF[attempt] * float64(time.Second)))
			continue
		}
		return nil, &TwitterError{Kind: state, Detail: describe(state, url, attempt+1)}
	}
	return nil, &TwitterError{Kind: TIMEOUT, Detail: describe(TIMEOUT, url, attempts)}
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func describe(state, url string, attempts int) string {
	switch state {
	case PAGE_ERROR:
		return fmt.Sprintf("X showed 'Something went wrong' for %s (%d attempts).", url, attempts)
	case TIMEOUT:
		return fmt.Sprintf("No results rendered for %s within %d s (%d attempts).", url, WAIT_SECONDS, attempts)
	default:
		return fmt.Sprintf("Failed to load %s.", url)
	}
}

// fetchFxStats fetches engagement stats from FxTwitter, ignoring failures.
func fetchFxStats(username, tweetID string, sem chan struct{}) map[string]any {
	if username == "" || tweetID == "" {
		return map[string]any{}
	}
	sem <- struct{}{}
	defer func() { <-sem }()

	url := fmt.Sprintf("https://api.fxtwitter.com/%s/status/%s", strings.TrimLeft(username, "@"), tweetID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return map[string]any{}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := fxClient.Do(req)
	if err != nil {
		return map[string]any{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return map[string]any{}
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	var payload struct {
		Tweet map[string]any `json:"tweet"`
	}
	if err := dec.Decode(&payload); err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	for _, key := range []string{"views", "likes", "retweets", "replies"} {
		if value, ok := payload.Tweet[key]; ok && value != nil {
			out[key] = value
		}
	}
	return out
}

// parseCount extracts the leading number from an aria-label such as
// "12 Replies. Reply".
func parseCount(label string) string {
	if label == "" {
		return "0"
	}
	if m := countRE.FindStringSubmatch(label); len(m) > 1 {
		return m[1]
	}
	return "0"
}

func statValue(stats map[string]any, key, fallback string) string {
	value, ok := stats[key]
	if !ok || value == nil {
		return fallback
	}
	switch v := value.(type) {
	case json.Number:
		return v.String()
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

// collectTweets reads tweets from the page, scrolling until maxResults unique
// tweets or maxScrolls. Insertion order is preserved, like Python's dict.
func collectTweets(page playwright.Page, maxResults, maxScrolls int) ([]*tweet, error) {
	tweets := []*tweet{}
	seen := map[string]bool{}
	for scroll := 0; scroll <= maxScrolls; scroll++ {
		value, err := page.Evaluate(extractTweetsJS)
		if err != nil {
			return nil, err
		}
		batch := []*tweet{}
		if err := decodeValue(value, &batch); err != nil {
			return nil, err
		}
		for _, t := range batch {
			key := t.Href
			if key == "" {
				key = t.Handle + t.Time + truncateRunes(t.Text, 40)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			tweets = append(tweets, t)
		}
		if len(tweets) >= maxResults || scroll == maxScrolls {
			break
		}
		if _, err := page.Evaluate("window.scrollBy(0, document.body.scrollHeight)"); err != nil {
			return nil, err
		}
		page.WaitForTimeout(1500)
	}
	if maxResults < len(tweets) {
		tweets = tweets[:maxResults]
	}
	return tweets, nil
}

func withStats(tweets []*tweet, defaultHandle string) []*tweet {
	sem := make(chan struct{}, FX_CONCURRENCY)
	stats := make([]map[string]any, len(tweets))
	var wg sync.WaitGroup
	for i, t := range tweets {
		if m := statusIDRE.FindStringSubmatch(t.Href); len(m) > 1 {
			t.ID = m[1]
		} else {
			t.ID = ""
		}
		handle := t.Handle
		if handle == "" {
			handle = defaultHandle
		}
		handle = strings.TrimLeft(handle, "@")
		wg.Add(1)
		go func(index int, user, id string) {
			defer wg.Done()
			stats[index] = fetchFxStats(user, id, sem)
		}(i, handle, t.ID)
	}
	wg.Wait()
	for i, t := range tweets {
		t.Replies = statValue(stats[i], "replies", parseCount(t.Reply))
		t.Retweets = statValue(stats[i], "retweets", parseCount(t.Retweet))
		t.Likes = statValue(stats[i], "likes", parseCount(t.Like))
		t.Views = statValue(stats[i], "views", "n/a")
		if strings.HasPrefix(t.Href, "/") {
			t.URL = "https://x.com" + t.Href
		} else {
			t.URL = t.Href
		}
	}
	return tweets
}

func formatTweet(i int, t *tweet, withHandle bool) string {
	name := t.Name
	if name == "" {
		name = "Unknown"
	}
	lines := []string{fmt.Sprintf("%d. Author: %s", i, name)}
	if withHandle {
		handle := t.Handle
		if handle == "" {
			handle = "Unknown"
		}
		lines = append(lines, "   Handle: "+handle)
	}
	timestamp := t.Time
	if timestamp == "" {
		timestamp = "Unknown"
	}
	lines = append(lines,
		"   Text: "+t.Text,
		"   Timestamp: "+timestamp,
		fmt.Sprintf("   Views: %s | Likes: %s | Retweets: %s | Replies (comments): %s", t.Views, t.Likes, t.Retweets, t.Replies),
	)
	if t.URL != "" {
		lines = append(lines, "   URL: "+t.URL)
	}
	return strings.Join(lines, "\n") + "\n"
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n])
	}
	return s
}

// pyQuote mirrors urllib.parse.quote with its default safe="/".
func pyQuote(s string) string {
	const hexchars = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '_' || c == '.' || c == '-' || c == '~' || c == '/' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexchars[c>>4])
		b.WriteByte(hexchars[c&0x0f])
	}
	return b.String()
}

func searchTweets(query string, maxResults int) (string, error) {
	url := "https://x.com/search?q=" + pyQuote(query) + "&f=live"
	page, err := openPage(url, `[data-testid="tweet"]`, []string{"SearchTimeline"})
	if err != nil {
		return "", err
	}
	if page == nil {
		return "No tweets found for query: " + query, nil
	}
	tweets, err := collectTweets(page, maxResults, 2)
	if err != nil {
		return "", err
	}
	tweets = withStats(tweets, "")
	if len(tweets) == 0 {
		return "No tweets found for query: " + query, nil
	}
	parts := make([]string, 0, len(tweets))
	for i, t := range tweets {
		parts = append(parts, formatTweet(i+1, t, true))
	}
	return strings.Join(parts, "\n"), nil
}

func getUserProfile(username string) (string, error) {
	username = strings.TrimLeft(username, "@")
	page, err := openPage("https://x.com/"+username, `[data-testid="UserName"]`, []string{"UserByScreenName"})
	if err != nil {
		return "", err
	}
	if page == nil {
		return "No profile found for user: " + username, nil
	}
	value, err := page.Evaluate(profileJS)
	if err != nil {
		return "", err
	}
	data := profileData{}
	if err := decodeValue(value, &data); err != nil {
		return "", err
	}
	name := data.Name
	if name == "" {
		name = username
	}
	bio := data.Bio
	if bio == "" {
		bio = "No bio"
	}
	followers := data.Followers
	if followers == "" {
		followers = "?"
	}
	following := data.Following
	if following == "" {
		following = "?"
	}
	return fmt.Sprintf("Name: %s\nUsername: @%s\nBio: %s\nFollowers: %s\nFollowing: %s", name, username, bio, followers, following), nil
}

func getUserTweets(username string, maxResults int) (string, error) {
	username = strings.TrimLeft(username, "@")
	page, err := openPage("https://x.com/"+username, `[data-testid="tweet"]`, []string{"UserTweets", "UserByScreenName"})
	if err != nil {
		return "", err
	}
	if page == nil {
		return "No tweets found for user: " + username, nil
	}
	tweets, err := collectTweets(page, maxResults, 5)
	if err != nil {
		return "", err
	}
	tweets = withStats(tweets, username)
	if len(tweets) == 0 {
		return "No tweets found for user: " + username, nil
	}
	parts := make([]string, 0, len(tweets))
	for i, t := range tweets {
		parts = append(parts, formatTweet(i+1, t, false))
	}
	return fmt.Sprintf("Tweets from %s:\n\n", username) + strings.Join(parts, "\n"), nil
}

func getTrending() (string, error) {
	page, err := openPage(
		"https://x.com/explore/tabs/trending",
		`[data-testid="trend"]`,
		[]string{"ExplorePage", "GenericTimelineById"},
	)
	if err != nil {
		return "", err
	}
	if page == nil {
		return "No trending topics shown.", nil
	}
	value, err := page.Evaluate(trendTextsJS)
	if err != nil {
		return "", err
	}
	texts := []string{}
	if err := decodeValue(value, &texts); err != nil {
		return "", err
	}
	trends := []string{}
	for _, fullText := range texts {
		lines := []string{}
		for _, raw := range strings.Split(fullText, "\n") {
			line := strings.TrimSpace(raw)
			if line != "" && line != "·" {
				lines = append(lines, line)
			}
		}
		filtered := []string{}
		for _, line := range lines {
			if len([]rune(line)) > 1 && line != "Trending" && line != "Show more" {
				filtered = append(filtered, line)
			}
		}
		content := []string{}
		for _, line := range filtered {
			if digitsOnlyRE.MatchString(line) || strings.Contains(line, "Trending") {
				continue
			}
			content = append(content, line)
		}
		if len(content) == 0 {
			continue
		}
		entry := fmt.Sprintf("%d. %s", len(trends)+1, content[0])
		if len(content) > 1 && postRE.MatchString(content[1]) {
			entry += " (" + content[1] + ")"
		}
		trends = append(trends, entry)
		if len(trends) >= 15 {
			break
		}
	}
	if len(trends) == 0 {
		return "No trending topics shown.", nil
	}
	return "Trending Topics:\n" + strings.Join(trends, "\n"), nil
}
