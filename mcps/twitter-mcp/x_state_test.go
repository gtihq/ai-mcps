package main

import (
	"math"
	"strings"
	"testing"
	"time"
)

var searchOps = []string{"SearchTimeline"}

func TestClassify(t *testing.T) {
	cases := []struct {
		name      string
		dom       DOMState
		responses []XResponse
		dataOps   []string
		final     bool
		want      string
	}{
		{
			name:      "results on the page win over everything",
			dom:       DOMState{Ready: true, Error: true},
			responses: []XResponse{{Op: "Viewer", Status: 429, Reset: "1"}},
			dataOps:   searchOps,
			want:      "ready",
		},
		{
			name: "viewer 429 with blank page is rate limited",
			// Observed on 2026-10-29: X returns 429 on Viewer and renders an empty app shell.
			dom: DOMState{URL: "https://x.com/search?q=CVE"},
			responses: []XResponse{
				{Op: "Viewer", Status: 429, Reset: "1793289400"},
				{Op: "timeline.json", Status: 200, Reset: nil},
			},
			dataOps: searchOps,
			want:    RATE_LIMITED,
		},
		{
			name:    "login redirect is login required",
			dom:     DOMState{URL: "https://x.com/i/flow/login?redirect_after_login=%2Fsearch"},
			dataOps: searchOps,
			want:    LOGIN_REQUIRED,
		},
		{
			name:      "401 is login required",
			dom:       DOMState{URL: "https://x.com/home"},
			responses: []XResponse{{Op: "Viewer", Status: 401, Reset: nil}},
			dataOps:   searchOps,
			want:      LOGIN_REQUIRED,
		},
		{
			name:    "empty state is an empty result not an error",
			dom:     DOMState{Empty: true},
			dataOps: searchOps,
			want:    "empty",
		},
		{
			name:    "missing account beats empty state",
			dom:     DOMState{Empty: true, NotFound: true},
			dataOps: searchOps,
			want:    ACCOUNT_NOT_FOUND,
		},
		{
			name:    "something went wrong is a page error",
			dom:     DOMState{Error: true},
			dataOps: searchOps,
			want:    PAGE_ERROR,
		},
		{
			name:    "keeps waiting while nothing is decided",
			dataOps: searchOps,
			want:    "",
		},
		{
			name:      "data arrived but no elements means selectors changed",
			responses: []XResponse{{Op: "SearchTimeline", Status: 200, Reset: nil}},
			dataOps:   searchOps,
			final:     true,
			want:      SELECTOR_CHANGED,
		},
		{
			name:    "nothing at all is a timeout",
			dataOps: searchOps,
			final:   true,
			want:    TIMEOUT,
		},
		{
			name:    "login form without the primary column is login required",
			dom:     DOMState{Login: true},
			dataOps: searchOps,
			want:    LOGIN_REQUIRED,
		},
		{
			name:    "a page error is returned before the final deadline",
			dom:     DOMState{Error: true},
			dataOps: searchOps,
			want:    PAGE_ERROR,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.dom, tc.responses, tc.dataOps, tc.final); got != tc.want {
				t.Fatalf("classify = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRateLimitReset(t *testing.T) {
	t.Run("reset header is used", func(t *testing.T) {
		reset, ok := rateLimitReset([]XResponse{{Op: "Viewer", Status: 429, Reset: "1793289400"}})
		if !ok || reset != 1793289400 {
			t.Fatalf("rateLimitReset = (%v, %v), want (1793289400, true)", reset, ok)
		}
	})

	t.Run("missing header assumes a 15 minute window", func(t *testing.T) {
		reset, ok := rateLimitReset([]XResponse{{Op: "Viewer", Status: 429, Reset: nil}})
		want := float64(time.Now().UnixNano())/1e9 + 900
		if !ok || math.Abs(reset-want) > 5 {
			t.Fatalf("rateLimitReset = (%v, %v), want about %v", reset, ok, want)
		}
	})

	t.Run("no 429 means no reset", func(t *testing.T) {
		if _, ok := rateLimitReset([]XResponse{{Op: "Viewer", Status: 200, Reset: "1"}}); ok {
			t.Fatal("expected no reset for a successful response")
		}
	})
}

func TestTwitterErrorMessage(t *testing.T) {
	t.Run("message names the kind and the reset", func(t *testing.T) {
		text := (&TwitterError{Kind: RATE_LIMITED, Detail: "X rate limited this account.", ResetAt: float64(time.Now().Unix()) + 120}).Message()
		if !strings.HasPrefix(text, "Error [RATE_LIMITED]: ") {
			t.Fatalf("unexpected prefix: %s", text)
		}
		if !strings.Contains(text, "Limit resets at") {
			t.Fatalf("missing reset clause: %s", text)
		}
		if !strings.Contains(text, "fall back to web search") {
			t.Fatalf("missing hint: %s", text)
		}
	})

	t.Run("reset of zero omits the reset clause", func(t *testing.T) {
		text := (&TwitterError{Kind: LOGIN_REQUIRED, Detail: "Session expired"}).Message()
		want := "Error [LOGIN_REQUIRED]: Session expired " + HINTS[LOGIN_REQUIRED]
		if text != want {
			t.Fatalf("message = %q, want %q", text, want)
		}
	})

	t.Run("every kind carries its hint", func(t *testing.T) {
		kinds := []string{
			RATE_LIMITED, LOGIN_REQUIRED, ACCOUNT_NOT_FOUND, PAGE_ERROR,
			SELECTOR_CHANGED, TIMEOUT, PROFILE_IN_USE, BROWSER_ERROR,
		}
		for _, kind := range kinds {
			text := (&TwitterError{Kind: kind, Detail: "detail"}).Message()
			if !strings.HasSuffix(text, " "+HINTS[kind]) {
				t.Fatalf("message for %s does not end with its hint: %s", kind, text)
			}
		}
	})

	t.Run("unknown kind keeps the trailing space", func(t *testing.T) {
		text := (&TwitterError{Kind: "UNKNOWN", Detail: "detail"}).Message()
		if text != "Error [UNKNOWN]: detail " {
			t.Fatalf("message = %q", text)
		}
	})

	t.Run("reset wait is clamped at zero", func(t *testing.T) {
		text := (&TwitterError{Kind: RATE_LIMITED, Detail: "detail", ResetAt: float64(time.Now().Unix()) - 600}).Message()
		if !strings.Contains(text, "(in 0 min 0 s).") {
			t.Fatalf("unexpected message: %s", text)
		}
	})
}

func TestRetryable(t *testing.T) {
	for _, kind := range []string{PAGE_ERROR, TIMEOUT} {
		if !RETRYABLE[kind] {
			t.Fatalf("%s should be retryable", kind)
		}
	}
	for _, kind := range []string{RATE_LIMITED, LOGIN_REQUIRED, ACCOUNT_NOT_FOUND, SELECTOR_CHANGED, PROFILE_IN_USE, BROWSER_ERROR} {
		if RETRYABLE[kind] {
			t.Fatalf("%s should not be retryable", kind)
		}
	}
}
