package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/internal/httpclient"
	"github.com/gtihq/ai-mcps/internal/mcpserver"
	"github.com/gtihq/ai-mcps/internal/omap"
	"github.com/imroc/req/v3"
)

const (
	defaultBaseURL = "https://www.zoom.com.br"
	requestTimeout = 40 * time.Second
	currency       = "BRL"
)

var (
	// requestBaseURL is a package variable so tests can point the HTTP call
	// at an httptest server. Product URLs always use defaultBaseURL, exactly
	// like the Python BASE constant, which the server never changes.
	requestBaseURL = defaultBaseURL

	// client mirrors curl_cffi.Session(impersonate="chrome"). It is created
	// once, like the Python module level session.
	client = newClient()

	// nextDataRE is the exact Python pattern, with re.S (DOTALL) expressed
	// as the Go (?s) flag. FindStringSubmatch returns the first match, like
	// re.search.
	nextDataRE = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)
)

// newClient mirrors curl_cffi.Session(impersonate="chrome") with the exact
// headers from the Python server and the same 40 second timeout.
func newClient() *req.Client {
	return httpclient.New(map[string]string{
		"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"accept-language": "pt-BR,pt;q=0.9,en-US;q=0.8,en;q=0.7",
		"user-agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	}, requestTimeout)
}

// descSearchProducts is the docstring from the original Python server, verbatim.
const descSearchProducts = "Search products on Zoom (price comparator) with the best price and the\n" +
	"list of stores selling each product. Aggregates KaBuM!, Mercado Livre,\n" +
	"Amazon, Magazine Luiza, Americanas and others.\n\n" +
	"Returns, per product: name, best price (R$), best store, all stores,\n" +
	"store count, rating and Zoom product URL.\n\n" +
	"Shipping (frete) is not part of Zoom's comparison data: it varies by\n" +
	"store, so the buyer must check it on the store page (use the product\n" +
	"URL) or ask for a CEP-specific quote on the chosen store's MCP.\n"

// tools returns the single tool in the same order and shape as the Python
// server, including its full docstring and argument schema.
func tools() []*mcpserver.Tool {
	return []*mcpserver.Tool{
		{
			Name:        "search_products",
			Description: descSearchProducts,
			Properties: []mcpserver.NamedProperty{
				{Name: "query", Prop: mcpserver.Property{Type: "string", Title: "Query"}},
				{Name: "page", Prop: mcpserver.Property{Type: "integer", Title: "Page", Default: 1}},
			},
			Required: []string{"query"},
			Handler:  handleSearchProducts,
		},
	}
}

func handleSearchProducts(_ context.Context, args map[string]any) (any, error) {
	query := argString(args, "query")
	page := argInt(args, "page", 1)
	if page < 1 {
		page = 1
	}

	// curl_cffi params={"q": query, "page": page} keep the dict insertion
	// order, so q comes before page.
	target := requestBaseURL + "/search?" + encodePairs("q", query, "page", strconv.Itoa(page))
	resp, err := client.R().Get(target)
	if err != nil {
		return errMap(err.Error()), nil
	}
	if resp.StatusCode != 200 {
		return errMap(fmt.Sprintf("Zoom returned HTTP %d", resp.StatusCode)), nil
	}

	nd := nextData(resp.String())
	if nd == nil {
		return errMap("could not parse Zoom __NEXT_DATA__ (possible anti-bot challenge)"), nil
	}

	props := asM(mget(nd, "props"))
	irs := asM(mget(props, "initialReduxState"))
	hits := asM(mget(irs, "hits"))
	items := asSlice(mget(hits, "hits"))

	var total any
	if pag := asM(mget(hits, "pagination")); pag != nil {
		total = mget(pag, "nbHits")
	}

	results := make([]any, 0, len(items))
	for _, h := range items {
		if asM(h) != nil {
			results = append(results, product(h))
		}
	}

	return omap.From(
		"query", query,
		"page", page,
		"total", total,
		"count", len(items),
		"results", results,
	), nil
}

// nextData mirrors the Python _next_data helper: regex search with DOTALL,
// then json.loads of the first capture group. An absent or invalid payload
// is reported as nil, which the handler turns into the anti-bot error.
func nextData(html string) *omap.M {
	m := nextDataRE.FindStringSubmatch(html)
	if m == nil {
		return nil
	}
	data, err := decodeOrdered([]byte(m[1]))
	if err != nil {
		return nil
	}
	return asM(data)
}

// product mirrors the Python _product helper. Key order matches exactly.
func product(item any) *omap.M {
	h := asM(item)

	merchants := make([]any, 0)
	for _, raw := range asSlice(mget(h, "merchants")) {
		m := asM(raw)
		if m == nil {
			continue
		}
		name := mget(m, "name")
		if isFalsy(name) {
			continue
		}
		merchants = append(merchants, name)
	}

	var bestStore any
	if offer := asM(mget(h, "bestOffer")); offer != nil {
		bestStore = mget(offer, "merchantName")
	}

	return omap.From(
		"name", pyOr(mget(h, "name"), mget(h, "shortName")),
		"price", mget(h, "price"),
		"currency", currency,
		"best_store", bestStore,
		"stores", merchants,
		"store_count", mget(h, "storeCount"),
		"rating", pyOr(mget(h, "preciseRating"), mget(h, "rating")),
		"rating_count", pyOr(mget(h, "ratingTotal"), mget(h, "countOfComments")),
		"discount_percent", mget(h, "loweringPercentage"),
		"category", mget(h, "categoryName"),
		"image", mget(h, "image"),
		"url", defaultBaseURL+asString(mget(h, "url")),
		"badges", pyOr(mget(h, "stamps"), []any{}),
	)
}

func errMap(message string) *omap.M {
	return omap.From("error", message)
}

// pyOr mirrors Python's "a or b": b is used only when a is falsy.
func pyOr(a, b any) any {
	if !isFalsy(a) {
		return a
	}
	return b
}

func mget(m *omap.M, key string) any {
	if m == nil {
		return nil
	}
	v, _ := m.Get(key)
	return v
}

func asM(v any) *omap.M {
	switch x := v.(type) {
	case *omap.M:
		return x
	case map[string]any:
		return omap.Wrap(x)
	default:
		return nil
	}
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func isFalsy(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return x == ""
	case bool:
		return !x
	case float64:
		return x == 0
	case int:
		return x == 0
	case int64:
		return x == 0
	case json.Number:
		return x.String() == "0"
	case []any:
		return len(x) == 0
	case *omap.M:
		return x.Len() == 0
	}
	return false
}

func argString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func argInt(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return def
		}
		return int(f)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return def
		}
		return n
	case bool:
		if x {
			return 1
		}
		return 0
	default:
		return def
	}
}

// encodePairs keeps the exact key order of the Python params dict. url.Values
// would sort the keys alphabetically, which is not what curl_cffi does.
func encodePairs(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(pairs[i]))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(pairs[i+1]))
	}
	return b.String()
}

// decodeOrdered decodes JSON into *omap.M objects so key order, including
// nested objects, survives exactly as in Python. UseNumber preserves integer
// and float spellings instead of collapsing everything into float64.
func decodeOrdered(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return decodeValue(dec)
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	switch d {
	case '{':
		m := omap.New()
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, fmt.Errorf("omap: expected string key, got %v", keyTok)
			}
			value, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			m.Set(key, value)
		}
		_, err = dec.Token()
		return m, err
	case '[':
		values := make([]any, 0)
		for dec.More() {
			value, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		_, err = dec.Token()
		return values, err
	default:
		return nil, fmt.Errorf("unexpected delimiter %v", d)
	}
}
