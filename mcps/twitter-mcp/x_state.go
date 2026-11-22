package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Error kinds returned to the agent. The names are stable because the
// x-research skill refers to them. Ported verbatim from x_state.py.
const (
	RATE_LIMITED      = "RATE_LIMITED"      // X answered HTTP 429; wait for the reset time
	LOGIN_REQUIRED    = "LOGIN_REQUIRED"    // session expired or logged out; run the login helper
	ACCOUNT_NOT_FOUND = "ACCOUNT_NOT_FOUND" // profile does not exist or is suspended
	PAGE_ERROR        = "PAGE_ERROR"        // X showed "Something went wrong"; usually transient
	SELECTOR_CHANGED  = "SELECTOR_CHANGED"  // data arrived but the expected elements are missing
	TIMEOUT           = "TIMEOUT"           // nothing usable rendered in time
	PROFILE_IN_USE    = "PROFILE_IN_USE"    // another browser already holds the profile folder
	BROWSER_ERROR     = "BROWSER_ERROR"     // the browser crashed or could not start
)

// RETRYABLE holds the errors worth one more attempt inside the same tool call.
var RETRYABLE = map[string]bool{PAGE_ERROR: true, TIMEOUT: true}

// DEFAULT_RATE_WINDOW is used when X sends HTTP 429 without a reset header.
const DEFAULT_RATE_WINDOW = 15 * 60

// HINTS is appended to every typed error message, exactly as in Python.
var HINTS = map[string]string{
	RATE_LIMITED:      "Do not retry or simplify the query before the reset; tell the user and fall back to web search.",
	LOGIN_REQUIRED:    "Ask the user to run login.py in the twitter-mcp folder, then retry.",
	ACCOUNT_NOT_FOUND: "Check the username spelling.",
	PAGE_ERROR:        "X failed to load the page after retries. Wait a minute before the next call.",
	SELECTOR_CHANGED:  "X likely changed its page layout; the MCP selectors need an update.",
	TIMEOUT:           "X did not render results in time. Wait a minute before the next call.",
	PROFILE_IN_USE:    "Close the other browser or MCP server process that uses ~/.twitter-mcp/chrome-profile, then retry.",
	BROWSER_ERROR:     "The MCP will relaunch the browser on the next call.",
}

// TwitterError is the typed failure returned by tools. ResetAt of zero means
// Python's None and therefore no "Limit resets at" clause.
type TwitterError struct {
	Kind    string
	Detail  string
	ResetAt float64
}

// Error implements the error interface.
func (e *TwitterError) Error() string { return e.message() }

// Message returns the string an agent sees for this error.
func (e *TwitterError) Message() string { return e.message() }

func (e *TwitterError) message() string {
	text := fmt.Sprintf("Error [%s]: %s", e.Kind, e.Detail)
	if e.ResetAt != 0 {
		now := float64(time.Now().UnixNano()) / 1e9
		wait := int64(e.ResetAt - now)
		if wait < 0 {
			wait = 0
		}
		clock := time.Unix(int64(e.ResetAt), 0).Format("15:04:05")
		text += fmt.Sprintf(" Limit resets at %s local time (in %d min %d s).", clock, wait/60, wait%60)
	}
	return text + " " + HINTS[e.Kind]
}

// XResponse is one {"op", "status", "reset"} entry recorded from an X API
// response. Reset keeps the raw JSON value so a missing header behaves like
// Python's None.
type XResponse struct {
	Op     string `json:"op"`
	Status int    `json:"status"`
	Reset  any    `json:"reset"`
}

// DOMState mirrors the dict returned by _DOM_STATE_JS.
type DOMState struct {
	URL      string `json:"url"`
	Ready    bool   `json:"ready"`
	Empty    bool   `json:"empty"`
	Login    bool   `json:"login"`
	Error    bool   `json:"error"`
	NotFound bool   `json:"not_found"`
}

// rateLimitReset returns the reset epoch of the first HTTP 429 response, and
// false when there was no 429. A 429 with an unparsable reset assumes the
// default 15 minute window, like rate_limit_reset in x_state.py.
func rateLimitReset(responses []XResponse) (float64, bool) {
	for _, r := range responses {
		if r.Status == 429 {
			if reset, ok := pyFloat(r.Reset); ok {
				return reset, true
			}
			return float64(time.Now().UnixNano())/1e9 + DEFAULT_RATE_WINDOW, true
		}
	}
	return 0, false
}

// pyFloat accepts the JSON representations of a number: a string, a float64,
// an int or a json.Number. Python's float(None) raises TypeError.
func pyFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// classify decides what the page shows. It returns "ready", "empty", an error
// kind, or "" to keep waiting (Python's None).
func classify(dom DOMState, responses []XResponse, dataOps []string, final bool) string {
	if dom.Ready {
		return "ready"
	}
	if strings.Contains(dom.URL, "/login") || strings.Contains(dom.URL, "/i/flow/") || dom.Login {
		return LOGIN_REQUIRED
	}
	if _, limited := rateLimitReset(responses); limited {
		return RATE_LIMITED
	}
	for _, r := range responses {
		if r.Status == 401 {
			return LOGIN_REQUIRED
		}
	}
	if dom.NotFound {
		return ACCOUNT_NOT_FOUND
	}
	if dom.Empty {
		return "empty"
	}
	if dom.Error {
		return PAGE_ERROR
	}
	if !final {
		return ""
	}
	for _, r := range responses {
		if r.Status != 200 {
			continue
		}
		for _, op := range dataOps {
			if r.Op == op {
				return SELECTOR_CHANGED
			}
		}
	}
	return TIMEOUT
}
