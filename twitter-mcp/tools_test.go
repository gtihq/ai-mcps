package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
)

func expectedTool(name, description, properties, required string) string {
	out := `{"name":"` + name + `","description":` + strconv.Quote(description) + `,"inputSchema":{"properties":{` + properties + `}`
	if required != "" {
		out += `,"required":[` + required + `]`
	}
	out += `,"title":"` + name + `Arguments","type":"object"},` +
		`"outputSchema":{"properties":{"result":{"title":"Result","type":"string"}},` +
		`"required":["result"],"title":"` + name + `Output","type":"object"}}`
	return out
}

func TestToolSchemas(t *testing.T) {
	s := mcpserver.New("twitter-mcp", tools()...)
	line := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))

	parts := []string{
		expectedTool("twitter_status", descTwitterStatus, "", ""),
		expectedTool("twitter_search", descTwitterSearch,
			`"query":{"title":"Query","type":"string"},"max_results":{"default":10,"title":"Max Results","type":"integer"}`,
			`"query"`),
		expectedTool("twitter_user", descTwitterUser,
			`"username":{"title":"Username","type":"string"}`, `"username"`),
		expectedTool("twitter_user_tweets", descTwitterUserTweets,
			`"username":{"title":"Username","type":"string"},"max_results":{"default":10,"title":"Max Results","type":"integer"}`,
			`"username"`),
		expectedTool("twitter_trending", descTwitterTrending, "", ""),
		expectedTool("twitter_post", descTwitterPost,
			`"text":{"title":"Text","type":"string"}`, `"text"`),
		expectedTool("twitter_reply", descTwitterReply,
			`"tweet_url":{"title":"Tweet Url","type":"string"},"text":{"title":"Text","type":"string"}`,
			`"tweet_url","text"`),
		expectedTool("twitter_like", descTwitterLike,
			`"tweet_url":{"title":"Tweet Url","type":"string"}`, `"tweet_url"`),
		expectedTool("twitter_retweet", descTwitterRetweet,
			`"tweet_url":{"title":"Tweet Url","type":"string"}`, `"tweet_url"`),
		expectedTool("twitter_feed", descTwitterFeed,
			`"max_results":{"default":20,"title":"Max Results","type":"integer"}`, ""),
		expectedTool("twitter_notifications", descTwitterNotifications, "", ""),
		expectedTool("twitter_bookmarks", descTwitterBookmarks,
			`"max_results":{"default":20,"title":"Max Results","type":"integer"}`, ""),
	}
	want := `{"jsonrpc":"2.0","id":1,"result":{"tools":[` + strings.Join(parts, ",") + `]}}` + "\n"
	if line != want {
		t.Fatalf("tools/list mismatch\n got: %s\nwant: %s", line, want)
	}
}

func callStatusText(t *testing.T, s *mcpserver.Server) string {
	t.Helper()
	line := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"twitter_status","arguments":{}}}`))
	var resp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("bad response %s: %v", line, err)
	}
	if resp.Result.IsError {
		t.Fatalf("twitter_status returned an error: %s", line)
	}
	if len(resp.Result.Content) != 1 || resp.Result.Content[0].Type != "text" {
		t.Fatalf("unexpected content: %s", line)
	}
	return resp.Result.Content[0].Text
}

func setRateLimitState(until float64, detail string) {
	session.stateMu.Lock()
	defer session.stateMu.Unlock()
	session.blockedUntil = until
	session.blockedDetail = detail
}

func TestTwitterStatus(t *testing.T) {
	oldHeadless, oldInterval := HEADLESS, MIN_INTERVAL
	oldUntil, oldDetail := session.RateLimitState()
	t.Cleanup(func() {
		HEADLESS, MIN_INTERVAL = oldHeadless, oldInterval
		setRateLimitState(oldUntil, oldDetail)
	})

	s := mcpserver.New("twitter-mcp", tools()...)

	t.Run("not rate limited", func(t *testing.T) {
		HEADLESS, MIN_INTERVAL = true, 4
		setRateLimitState(0, "")
		want := "Not rate limited.\nHeadless: True\nMinimum seconds between page loads: 4.0"
		if got := callStatusText(t, s); got != want {
			t.Fatalf("status mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("rate limited clock and settings", func(t *testing.T) {
		HEADLESS, MIN_INTERVAL = false, 0.5
		reset := float64(time.Now().Unix() + 120)
		clock := time.Unix(int64(reset), 0).Format("15:04:05")
		setRateLimitState(reset, "HTTP 429 on Viewer")
		want := "RATE_LIMITED until " + clock + " local time (HTTP 429 on Viewer). Calls fail fast until then." +
			"\nHeadless: False\nMinimum seconds between page loads: 0.5"
		if got := callStatusText(t, s); got != want {
			t.Fatalf("status mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("expired block is not reported", func(t *testing.T) {
		HEADLESS, MIN_INTERVAL = true, 4
		setRateLimitState(float64(time.Now().Unix()-60), "old block")
		want := "Not rate limited.\nHeadless: True\nMinimum seconds between page loads: 4.0"
		if got := callStatusText(t, s); got != want {
			t.Fatalf("status mismatch\n got: %q\nwant: %q", got, want)
		}
	})
}

func TestFormatError(t *testing.T) {
	typed := formatError(&TwitterError{Kind: RATE_LIMITED, Detail: "blocked"})
	if typed != "Error [RATE_LIMITED]: blocked "+HINTS[RATE_LIMITED] {
		t.Fatalf("typed error = %q", typed)
	}
	generic := formatError(&testError{"first line\nsecond line"})
	want := "Error [BROWSER_ERROR]: first line " + HINTS[BROWSER_ERROR]
	if generic != want {
		t.Fatalf("generic error = %q, want %q", generic, want)
	}
}

type testError struct{ text string }

func (e *testError) Error() string { return e.text }

func TestPureFormattingHelpers(t *testing.T) {
	t.Run("pyQuote leaves only the Python default safe characters", func(t *testing.T) {
		got := pyQuote("CVE-2026 from:@user/a&b")
		want := "CVE-2026%20from%3A%40user/a%26b"
		if got != want {
			t.Fatalf("pyQuote = %q, want %q", got, want)
		}
	})

	t.Run("parseCount", func(t *testing.T) {
		cases := map[string]string{
			"": "0", "12 Replies. Reply": "12", " 1.2K Likes": "1.2K", "no number": "0",
		}
		for label, want := range cases {
			if got := parseCount(label); got != want {
				t.Fatalf("parseCount(%q) = %q, want %q", label, got, want)
			}
		}
	})

	t.Run("formatTweet", func(t *testing.T) {
		item := &tweet{
			Name: "Author", Handle: "@author", Text: "hello", Time: "2026-11-22T00:00:00Z",
			Views: "n/a", Likes: "3", Retweets: "2", Replies: "1", URL: "https://x.com/a/status/1",
		}
		want := "1. Author: Author\n   Handle: @author\n   Text: hello\n   Timestamp: 2026-11-22T00:00:00Z\n" +
			"   Views: n/a | Likes: 3 | Retweets: 2 | Replies (comments): 1\n   URL: https://x.com/a/status/1\n"
		if got := formatTweet(1, item, true); got != want {
			t.Fatalf("formatTweet mismatch\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("pyJSONArray uses Python separators and keeps unicode", func(t *testing.T) {
		got := pyJSONArray([][]jsonKV{
			{{key: "author", val: "José"}, {key: "text", val: "a < b"}},
		})
		want := `[{"author": "José", "text": "a < b"}]`
		if got != want {
			t.Fatalf("pyJSONArray = %q, want %q", got, want)
		}
		if got := pyJSONArray(nil); got != "[]" {
			t.Fatalf("empty array = %q", got)
		}
	})
}
