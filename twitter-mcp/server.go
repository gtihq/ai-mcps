package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
)

// Docstrings from server.py, verbatim. FastMCP keeps the trailing newline of
// the ones whose closing quotes sat on a separate line.
const (
	descTwitterStatus        = "Report the MCP state without contacting X: rate-limit block and settings.\nCall this after an error, before retrying."
	descTwitterSearch        = "Search Twitter/X for tweets by keyword (latest tab). Run searches one at a time.\nArgs:\n    query: Search query, X search operators allowed\n    max_results: Max tweets to return (default 10)\n"
	descTwitterUser          = "Get a Twitter/X user's profile (bio, followers, following).\nArgs:\n    username: Twitter username (without @)\n"
	descTwitterUserTweets    = "Get recent tweets from a Twitter/X user.\nArgs:\n    username: Twitter username (without @)\n    max_results: Max tweets (default 10)\n"
	descTwitterTrending      = "Get trending topics on Twitter/X."
	descTwitterPost          = "Post a tweet. Requires a logged-in browser profile (run login.py once).\nArgs:\n    text: Tweet text (max 280 chars)\n"
	descTwitterReply         = "Reply to a tweet. Requires a logged-in browser profile.\nArgs:\n    tweet_url: URL of the tweet to reply to\n    text: Reply text\n"
	descTwitterLike          = "Like a tweet. Requires a logged-in browser profile.\nArgs:\n    tweet_url: URL of the tweet to like\n"
	descTwitterRetweet       = "Retweet a tweet. Requires a logged-in browser profile.\nArgs:\n    tweet_url: URL of the tweet to retweet\n"
	descTwitterFeed          = "Read your Twitter/X home feed. Requires a logged-in browser profile.\nArgs:\n    max_results: Max tweets to return (default 20)\n"
	descTwitterNotifications = "Read your Twitter/X notifications and mentions. Requires a logged-in browser profile."
	descTwitterBookmarks     = "Read your saved/bookmarked tweets on Twitter/X. Requires a logged-in browser profile.\nArgs:\n    max_results: Max bookmarks to return (default 20)\n"
)

// tools returns the 12 tools in the exact order and shape of server.py.
func tools() []*mcpserver.Tool {
	return []*mcpserver.Tool{
		{
			Name:         "twitter_status",
			OutputSchema: true,
			Description:  descTwitterStatus,
			Handler:      handleStatus,
		},
		{
			Name:         "twitter_search",
			OutputSchema: true,
			Description:  descTwitterSearch,
			Properties: []mcpserver.NamedProperty{
				{Name: "query", Prop: mcpserver.Property{Type: "string", Title: "Query"}},
				{Name: "max_results", Prop: mcpserver.Property{Type: "integer", Title: "Max Results", Default: 10}},
			},
			Required: []string{"query"},
			Handler:  handleSearch,
		},
		{
			Name:         "twitter_user",
			OutputSchema: true,
			Description:  descTwitterUser,
			Properties: []mcpserver.NamedProperty{
				{Name: "username", Prop: mcpserver.Property{Type: "string", Title: "Username"}},
			},
			Required: []string{"username"},
			Handler:  handleUser,
		},
		{
			Name:         "twitter_user_tweets",
			OutputSchema: true,
			Description:  descTwitterUserTweets,
			Properties: []mcpserver.NamedProperty{
				{Name: "username", Prop: mcpserver.Property{Type: "string", Title: "Username"}},
				{Name: "max_results", Prop: mcpserver.Property{Type: "integer", Title: "Max Results", Default: 10}},
			},
			Required: []string{"username"},
			Handler:  handleUserTweets,
		},
		{
			Name:         "twitter_trending",
			OutputSchema: true,
			Description:  descTwitterTrending,
			Handler:      handleTrending,
		},
		{
			Name:         "twitter_post",
			OutputSchema: true,
			Description:  descTwitterPost,
			Properties: []mcpserver.NamedProperty{
				{Name: "text", Prop: mcpserver.Property{Type: "string", Title: "Text"}},
			},
			Required: []string{"text"},
			Handler:  handlePost,
		},
		{
			Name:         "twitter_reply",
			OutputSchema: true,
			Description:  descTwitterReply,
			Properties: []mcpserver.NamedProperty{
				{Name: "tweet_url", Prop: mcpserver.Property{Type: "string", Title: "Tweet Url"}},
				{Name: "text", Prop: mcpserver.Property{Type: "string", Title: "Text"}},
			},
			Required: []string{"tweet_url", "text"},
			Handler:  handleReply,
		},
		{
			Name:         "twitter_like",
			OutputSchema: true,
			Description:  descTwitterLike,
			Properties: []mcpserver.NamedProperty{
				{Name: "tweet_url", Prop: mcpserver.Property{Type: "string", Title: "Tweet Url"}},
			},
			Required: []string{"tweet_url"},
			Handler:  handleLike,
		},
		{
			Name:         "twitter_retweet",
			OutputSchema: true,
			Description:  descTwitterRetweet,
			Properties: []mcpserver.NamedProperty{
				{Name: "tweet_url", Prop: mcpserver.Property{Type: "string", Title: "Tweet Url"}},
			},
			Required: []string{"tweet_url"},
			Handler:  handleRetweet,
		},
		{
			Name:         "twitter_feed",
			OutputSchema: true,
			Description:  descTwitterFeed,
			Properties: []mcpserver.NamedProperty{
				{Name: "max_results", Prop: mcpserver.Property{Type: "integer", Title: "Max Results", Default: 20}},
			},
			Handler: handleFeed,
		},
		{
			Name:         "twitter_notifications",
			OutputSchema: true,
			Description:  descTwitterNotifications,
			Handler:      handleNotifications,
		},
		{
			Name:         "twitter_bookmarks",
			OutputSchema: true,
			Description:  descTwitterBookmarks,
			Properties: []mcpserver.NamedProperty{
				{Name: "max_results", Prop: mcpserver.Property{Type: "integer", Title: "Max Results", Default: 20}},
			},
			Handler: handleBookmarks,
		},
	}
}

// call is the equivalent of server.py's _call: it runs the blocking tool on
// the shared session and converts failures into typed error strings.
func call(fn func() (string, error)) string {
	value, err := session.Run(fn)
	if err != nil {
		return formatError(err)
	}
	return value
}

func formatError(err error) string {
	var typed *TwitterError
	if errors.As(err, &typed) {
		return typed.Message()
	}
	if err == nil {
		return ""
	}
	detail := firstLine(err.Error())
	if detail == "" {
		detail = fmt.Sprintf("%T", err)
	}
	return (&TwitterError{Kind: BROWSER_ERROR, Detail: detail}).Message()
}

func argString(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func argInt(args map[string]any, key string, fallback int) int {
	v, ok := args[key].(float64)
	if !ok {
		return fallback
	}
	return int(v)
}

func handleStatus(context.Context, map[string]any) (any, error) {
	lines := []string{
		fmt.Sprintf("Headless: %s", pyBool(HEADLESS)),
		fmt.Sprintf("Minimum seconds between page loads: %s", pyFloatString(MIN_INTERVAL)),
	}
	until, detail := session.RateLimitState()
	if now := float64(time.Now().UnixNano()) / 1e9; now < until {
		clock := time.Unix(int64(until), 0).Format("15:04:05")
		lines = append([]string{fmt.Sprintf("RATE_LIMITED until %s local time (%s). Calls fail fast until then.", clock, detail)}, lines...)
	} else {
		lines = append([]string{"Not rate limited."}, lines...)
	}
	return strings.Join(lines, "\n"), nil
}

func handleSearch(_ context.Context, args map[string]any) (any, error) {
	query := argString(args, "query")
	limit := argInt(args, "max_results", 10)
	return call(func() (string, error) { return searchTweets(query, limit) }), nil
}

func handleUser(_ context.Context, args map[string]any) (any, error) {
	username := argString(args, "username")
	return call(func() (string, error) { return getUserProfile(username) }), nil
}

func handleUserTweets(_ context.Context, args map[string]any) (any, error) {
	username := argString(args, "username")
	limit := argInt(args, "max_results", 10)
	return call(func() (string, error) { return getUserTweets(username, limit) }), nil
}

func handleTrending(context.Context, map[string]any) (any, error) {
	return call(getTrending), nil
}

func handlePost(_ context.Context, args map[string]any) (any, error) {
	text := argString(args, "text")
	return call(func() (string, error) { return postTweet(text) }), nil
}

func handleReply(_ context.Context, args map[string]any) (any, error) {
	url := argString(args, "tweet_url")
	text := argString(args, "text")
	return call(func() (string, error) { return replyToTweet(url, text) }), nil
}

func handleLike(_ context.Context, args map[string]any) (any, error) {
	url := argString(args, "tweet_url")
	return call(func() (string, error) { return likeTweet(url) }), nil
}

func handleRetweet(_ context.Context, args map[string]any) (any, error) {
	url := argString(args, "tweet_url")
	return call(func() (string, error) { return retweet(url) }), nil
}

func handleFeed(_ context.Context, args map[string]any) (any, error) {
	limit := argInt(args, "max_results", 20)
	return call(func() (string, error) { return readFeed(limit) }), nil
}

func handleNotifications(context.Context, map[string]any) (any, error) {
	return call(readNotifications), nil
}

func handleBookmarks(_ context.Context, args map[string]any) (any, error) {
	limit := argInt(args, "max_results", 20)
	return call(func() (string, error) { return readBookmarks(limit) }), nil
}

// pyBool and pyFloatString reproduce Python's f-string rendering of a bool
// and a float, which is what twitter_status prints.
func pyBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

func pyFloatString(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
