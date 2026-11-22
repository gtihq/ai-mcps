// Package httpclient wraps req with the Chrome TLS/HTTP fingerprint used by
// the Python servers through curl_cffi(impersonate="chrome").
package httpclient

import (
	"net/url"
	"time"

	"github.com/imroc/req/v3"
)

// New returns a client that impersonates Chrome, with the supplied default
// headers and timeout.
func New(headers map[string]string, timeout time.Duration) *req.Client {
	if timeout <= 0 {
		timeout = 40 * time.Second
	}
	c := req.C().ImpersonateChrome().SetTimeout(timeout)
	for k, v := range headers {
		c.SetCommonHeader(k, v)
	}
	return c
}

// Encode builds a query string in the same order as Python's params dict.
// Values are kept in the order given; callers pass keys in the desired order.
func Encode(pairs ...string) string {
	q := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		q.Add(pairs[i], pairs[i+1])
	}
	return q.Encode()
}

// ChromeHeaders returns the headers used by the KaBuM! and Zoom servers.
func ChromeHeaders(accept, origin, referer string) map[string]string {
	h := map[string]string{
		"accept":          accept,
		"accept-language": "pt-BR,pt;q=0.9,en-US;q=0.8,en;q=0.7",
		"user-agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	}
	if origin != "" {
		h["origin"] = origin
	}
	if referer != "" {
		h["referer"] = referer
	}
	return h
}
