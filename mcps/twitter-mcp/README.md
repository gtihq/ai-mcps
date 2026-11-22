<div align="center">

# Twitter/X MCP

> Read and post on X through a logged-in browser

**It drives one headless Chromium on a persistent profile and reuses a single tab for all 12 tools, matching the former Python server.**

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](#requirements)
[![Protocol](https://img.shields.io/badge/protocol-stdio%20JSON--RPC-blue.svg)](#overview)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20Windows-lightgrey.svg)](#requirements)

</div>

---

## Overview

An MCP server that reads and posts on X (Twitter) by driving a logged-in headless Chromium, with no API key. It is a Go port of the former Python server and speaks the same JSON-RPC 2.0 stdio protocol.

This package is the `twitter` MCP server. All 12 tools share one browser session: the first tool call launches one Chromium on a persistent profile and every later call reuses it on a single tab, one call at a time. Each full page load of x.com calls the rate-limited `Viewer` API, so the server navigates inside the loaded app instead of reloading.

Failures return one line starting with `Error [KIND]: ...`. `RATE_LIMITED` includes the reset time, and calls made before that time fail at once without contacting X. The kinds are defined in `x_state.go`. At least `TWITTER_MCP_MIN_INTERVAL` seconds pass between page loads, and `PAGE_ERROR` and `TIMEOUT` are retried twice with backoff.

The server does not bypass X limits, CAPTCHAs, or login checks. It needs an X account that you log in to once. Engagement counts come from the public FxTwitter API when it answers, and from the page otherwise.

## Requirements

- Go 1.25 or later on Linux or Windows.
- The Chromium build used by playwright-go.
- An X account with a one-time login to the persistent profile.
- No API key is required.

## Build and run

Install the Chromium build that playwright-go uses, then build the binary from this directory:

```bash
go run github.com/mxschmitt/playwright-go/cmd/playwright install chromium
go build -o twitter-mcp .
```

On Windows, use `go build -o twitter-mcp.exe .`. You can also run it without building:

```bash
go run .
```

## Register with your agent

Register the server with your agent. The command it runs is the built binary. For example, for OpenCode:

```bash
opencode mcp add twitter --global -- /absolute/path/to/ai-mcps/mcps/twitter-mcp/twitter-mcp
```

## Login

### One-time login

Log in once. Run this while no MCP server is running, because two browsers cannot share the profile:

```bash
./twitter-mcp --login
```

On Windows, use `twitter-mcp.exe --login` or `go run . --login`.

### Keepalive

```bash
./twitter-mcp --keepalive
```

It loads x.com once to refresh cookies, prints one status line and appends a JSON line to `data/twitter_keepalive.log` next to the executable. It exits 1 when the session expired.

## Tools

| Tool | Description |
|------|-------------|
| `twitter_status` | Report the MCP state without contacting X: rate-limit block and settings |
| `twitter_search` | Search tweets by keyword (latest tab) |
| `twitter_user` | Get a user's profile (bio, followers, following) |
| `twitter_user_tweets` | Get recent tweets from a user |
| `twitter_trending` | Get trending topics |
| `twitter_post` | Post a tweet |
| `twitter_reply` | Reply to a tweet |
| `twitter_like` | Like a tweet |
| `twitter_retweet` | Retweet a tweet |
| `twitter_feed` | Read the home feed |
| `twitter_notifications` | Read notifications and mentions |
| `twitter_bookmarks` | Read saved/bookmarked tweets |

Every tool returns a string. Tool names, descriptions, property order, defaults and error strings match the Python server.

## Environment

| Variable | Default | Purpose |
|----------|---------|---------|
| `TWITTER_MCP_PROFILE` | `~/.twitter-mcp/chrome-profile` | Chromium profile folder that holds the X login |
| `TWITTER_MCP_HEADLESS` | `1` | `0` shows the browser window |
| `TWITTER_MCP_MIN_INTERVAL` | `4` | Minimum seconds between page loads |
| `TWITTER_MCP_CALL_TIMEOUT` | `120` | Maximum seconds for one tool call |

The session cookies are also backed up to `~/.twitter-mcp/storage_state.json`. A new, empty profile is seeded from that file.

## Layout

| Path | Responsibility |
|------|----------------|
| `main.go` | Entry point, `--login` and `--keepalive` helpers |
| `server.go` | The 12 tool definitions, argument parsing and typed error formatting |
| `session.go` | Shared browser, one tab, serialized calls, pacing and rate-limit circuit breaker |
| `x_state.go` | Error kinds, classifier and typed error messages |
| `tools_read.go` | Search, profile, user tweets, trending, navigation and wait logic |
| `tools_feed.go` | Home feed, notifications and bookmarks |
| `tools_write.go` | Post, reply, like and retweet |
| `x_state_test.go` | Classifier and error message tests |
| `tools_test.go` | Tool schema and `twitter_status` tests, with no browser contact |
| `LICENSE` | License for this server |

## Development

Run the tests and the vet check from the repository root:

```bash
go test ./mcps/twitter-mcp/...
go vet ./mcps/twitter-mcp/...
```

The tests cover the classifier, typed errors, all 12 tool schemas and `twitter_status`. They do not touch the network or launch a browser.

See [../../README.md](../../README.md) for the full repository.
