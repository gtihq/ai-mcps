package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	playwright "github.com/mxschmitt/playwright-go"
)

var (
	// PROFILE_DIR and STORAGE_BACKUP mirror the Python module level paths.
	PROFILE_DIR    string
	STORAGE_BACKUP string

	// HEADLESS and MIN_INTERVAL are kept as the same names as Python so the
	// status tool and the tests read the same settings.
	HEADLESS     bool
	MIN_INTERVAL float64
	CALL_TIMEOUT float64

	// No --restore-last-session: it reopened every old tab on each launch.
	BROWSER_ARGS = []string{"--disable-blink-features=AutomationControlled"}
	IGNORE_ARGS  = []string{"--enable-automation"}
	USER_AGENT   string
)

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	if v := os.Getenv("TWITTER_MCP_PROFILE"); v != "" {
		PROFILE_DIR = v
	} else {
		PROFILE_DIR = filepath.Join(home, ".twitter-mcp", "chrome-profile")
	}
	STORAGE_BACKUP = filepath.Join(home, ".twitter-mcp", "storage_state.json")

	HEADLESS = os.Getenv("TWITTER_MCP_HEADLESS") != "0"
	MIN_INTERVAL = envFloat("TWITTER_MCP_MIN_INTERVAL", 4)
	CALL_TIMEOUT = envFloat("TWITTER_MCP_CALL_TIMEOUT", 120)

	// Headless Chromium reports "HeadlessChrome" in its user agent, which X
	// treats as a bot.
	var platform string
	switch runtime.GOOS {
	case "windows":
		platform = "Windows NT 10.0; Win64; x64"
	case "darwin":
		platform = "Macintosh; Intel Mac OS X 10_15_7"
	default:
		platform = "X11; Linux x86_64"
	}
	USER_AGENT = "Mozilla/5.0 (" + platform + ") AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
}

func envFloat(key string, fallback float64) float64 {
	v, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil {
		return fallback
	}
	return v
}

// getPersistentContext launches a browser on the persistent profile and
// returns (playwright, context). A fresh profile is seeded from the
// storage_state.json backup, like the Python helper.
func getPersistentContext(headless bool) (*playwright.Playwright, playwright.BrowserContext, error) {
	if err := os.MkdirAll(PROFILE_DIR, 0o755); err != nil {
		return nil, nil, &TwitterError{Kind: BROWSER_ERROR, Detail: "Could not launch Chromium: " + err.Error()}
	}
	_, statErr := os.Stat(filepath.Join(PROFILE_DIR, "Default"))
	freshProfile := statErr != nil

	pw, err := playwright.Run()
	if err != nil {
		return nil, nil, &TwitterError{Kind: BROWSER_ERROR, Detail: "Could not launch Chromium: " + firstLine(err.Error())}
	}
	opts := playwright.BrowserTypeLaunchPersistentContextOptions{
		Args:              BROWSER_ARGS,
		Headless:          playwright.Bool(headless),
		IgnoreDefaultArgs: IGNORE_ARGS,
		Viewport:          &playwright.Size{Width: 1280, Height: 900},
		UserAgent:         playwright.String(USER_AGENT),
	}
	ctx, err := pw.Chromium.LaunchPersistentContext(PROFILE_DIR, opts)
	if err != nil {
		_ = pw.Stop()
		text := err.Error()
		if strings.Contains(text, "existing browser session") || strings.Contains(text, "has been closed") || strings.Contains(text, "ProcessSingleton") {
			return nil, nil, &TwitterError{Kind: PROFILE_IN_USE, Detail: fmt.Sprintf("Another Chromium process already uses the profile folder %s.", PROFILE_DIR)}
		}
		return nil, nil, &TwitterError{Kind: BROWSER_ERROR, Detail: "Could not launch Chromium: " + firstLine(text)}
	}

	if freshProfile {
		if _, err := os.Stat(STORAGE_BACKUP); err == nil {
			seedCookiesFromBackup(ctx)
		}
	}
	return pw, ctx, nil
}

// seedCookiesFromBackup is non-critical: a failure is ignored like in Python.
func seedCookiesFromBackup(ctx playwright.BrowserContext) {
	data, err := os.ReadFile(STORAGE_BACKUP)
	if err != nil {
		return
	}
	var state struct {
		Cookies []playwright.OptionalCookie `json:"cookies"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	if len(state.Cookies) > 0 {
		_ = ctx.AddCookies(state.Cookies)
	}
}

// saveSession writes the current session back to storage_state.json as a
// backup. The profile folder remains the primary store.
func saveSession(ctx playwright.BrowserContext) {
	if ctx == nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(STORAGE_BACKUP), 0o755)
	_, _ = ctx.StorageState(playwright.BrowserContextStorageStateOptions{Path: playwright.String(STORAGE_BACKUP)})
}

func closeSession(pw *playwright.Playwright, ctx playwright.BrowserContext) {
	saveSession(ctx)
	if ctx != nil {
		_ = ctx.Close()
	}
	if pw != nil {
		_ = pw.Stop()
	}
}

// SharedSession owns the browser and serializes every tool call, one at a
// time, on one tab. The Python version used one background event loop and an
// asyncio.Lock; here callMu plays that role.
type SharedSession struct {
	callMu  sync.Mutex
	stateMu sync.RWMutex

	pw      *playwright.Playwright
	ctx     playwright.BrowserContext
	page    playwright.Page
	lastNav float64

	blockedUntil  float64
	blockedDetail string
}

var session = &SharedSession{}

// Run runs fn with the calls serialized and waits for the result, with the
// same overall timeout as Python's future.result(timeout=CALL_TIMEOUT).
func (s *SharedSession) Run(fn func() (string, error)) (string, error) {
	type outcome struct {
		value string
		err   error
	}
	ch := make(chan outcome, 1)
	go func() {
		s.callMu.Lock()
		defer s.callMu.Unlock()
		if err := s.checkRateLimit(); err != nil {
			ch <- outcome{err: err}
			return
		}
		value, err := fn()
		if err != nil {
			var typed *TwitterError
			if !errors.As(err, &typed) {
				text := err.Error()
				if strings.Contains(text, "has been closed") || strings.Contains(text, "Target closed") {
					_ = s.reset()
					err = &TwitterError{Kind: BROWSER_ERROR, Detail: "The browser closed during the call."}
				}
			}
		}
		ch <- outcome{value: value, err: err}
	}()

	timer := time.NewTimer(time.Duration(CALL_TIMEOUT * float64(time.Second)))
	defer timer.Stop()
	select {
	case out := <-ch:
		return out.value, out.err
	case <-timer.C:
		return "", &TwitterError{Kind: TIMEOUT, Detail: fmt.Sprintf("The tool call took longer than %d s and was cancelled.", int(CALL_TIMEOUT))}
	}
}

// checkRateLimit fails fast while the account is blocked.
func (s *SharedSession) checkRateLimit() error {
	s.stateMu.RLock()
	until, detail := s.blockedUntil, s.blockedDetail
	s.stateMu.RUnlock()
	if float64(time.Now().UnixNano())/1e9 < until {
		return &TwitterError{
			Kind:    RATE_LIMITED,
			Detail:  fmt.Sprintf("X rate limited this account (%s). Skipped the request to avoid extending the block.", detail),
			ResetAt: until,
		}
	}
	return nil
}

func (s *SharedSession) markRateLimited(resetAt float64, detail string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if resetAt > s.blockedUntil {
		s.blockedUntil = resetAt
	}
	s.blockedDetail = detail
}

// RateLimitState returns the circuit breaker state for twitter_status.
func (s *SharedSession) RateLimitState() (float64, string) {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.blockedUntil, s.blockedDetail
}

// Context returns the shared browser context, launching or relaunching it
// when needed.
func (s *SharedSession) Context() (playwright.BrowserContext, error) {
	if s.ctx != nil && s.page != nil && !s.page.IsClosed() {
		return s.ctx, nil
	}
	if err := s.reset(); err != nil {
		return nil, err
	}
	pw, ctx, err := getPersistentContext(HEADLESS)
	if err != nil {
		return nil, err
	}
	s.pw, s.ctx = pw, ctx
	pages := ctx.Pages()
	if len(pages) > 0 {
		s.page = pages[0]
	} else {
		s.page, err = ctx.NewPage()
		if err != nil {
			_ = s.reset()
			return nil, &TwitterError{Kind: BROWSER_ERROR, Detail: "Could not launch Chromium: " + err.Error()}
		}
	}
	return s.ctx, nil
}

// Page returns the single shared tab and closes any other tab that appeared.
func (s *SharedSession) Page() (playwright.Page, error) {
	ctx, err := s.Context()
	if err != nil {
		return nil, err
	}
	for _, extra := range ctx.Pages() {
		if extra != s.page {
			_ = extra.Close()
		}
	}
	return s.page, nil
}

// Pace keeps at least MIN_INTERVAL seconds, plus jitter, between navigations.
func (s *SharedSession) Pace() {
	now := float64(time.Now().UnixNano()) / 1e9
	wait := s.lastNav + MIN_INTERVAL + rand.Float64()*1.5 - now
	if wait > 0 {
		time.Sleep(time.Duration(wait * float64(time.Second)))
	}
	s.lastNav = float64(time.Now().UnixNano()) / 1e9
}

func (s *SharedSession) Save() {
	saveSession(s.ctx)
}

func (s *SharedSession) reset() error {
	ctx, pw := s.ctx, s.pw
	s.ctx, s.page, s.pw = nil, nil, nil
	var firstErr error
	if ctx != nil {
		if err := ctx.Close(); err != nil {
			firstErr = err
		}
	}
	if pw != nil {
		if err := pw.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Shutdown saves the session and closes the browser.
func (s *SharedSession) Shutdown() {
	if s.ctx == nil {
		return
	}
	s.Save()
	_ = s.reset()
}

func firstLine(text string) string {
	if text == "" {
		return ""
	}
	return strings.SplitN(text, "\n", 2)[0]
}
