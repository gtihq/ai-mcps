package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gtihq/ai-mcps/internal/httpclient"
	"github.com/gtihq/ai-mcps/internal/mcpserver"
	"github.com/gtihq/ai-mcps/internal/omap"
	"github.com/imroc/req/v3"
)

const (
	defaultBaseURL = "https://servicespub.prod.api.aws.grupokabum.com.br"
	defaultSiteURL = "https://www.kabum.com.br"
	requestTimeout = 40 * time.Second
)

var (
	// baseURL and siteURL are package variables so tests can point the tools
	// at an httptest server without touching any other package.
	baseURL = defaultBaseURL
	siteURL = defaultSiteURL
	client  = newClient()
)

// newClient mirrors curl_cffi.Session(impersonate="chrome") with the same
// headers and the same 40 second timeout.
func newClient() *req.Client {
	return httpclient.New(httpclient.ChromeHeaders(
		"application/json, text/plain, */*",
		siteURL,
		siteURL+"/",
	), requestTimeout)
}

// Docstrings, verbatim, from the original Python server.
const (
	descSearchProducts = "Keyword search on KaBuM! with live prices (BRL), Pix/discount price,\n" +
		"stock, rating and product links.\n\n" +
		"sort: most_searched, lowest_price, biggest_price, best_rated,\n" +
		"      most_recent, biggest_discount.\n"

	descProductDetails = "Full detail for one KaBuM! product by its numeric id (the number in a\n" +
		"kabum.com.br/produto/<id>/... URL)."

	descZipLookup = "Resolve a Brazilian CEP (postal code) to its address via KaBuM!'s\n" +
		"shipping service. Useful to confirm a CEP before quoting freight."

	descShippingQuote = "Quote shipping (frete) options and prices for a KaBuM! product to a\n" +
		"Brazilian CEP. Returns delivery options with price (BRL), deadline in days\n" +
		"and estimated arrival date. Ask the user for their CEP if not provided."
)

// sortMap mirrors _SORT. It maps a public name to the value sent to the API.
var sortMap = map[string]string{
	"most_searched":    "most_searched",
	"lowest_price":     "lowest_price",
	"biggest_price":    "biggest_price",
	"best_rated":       "best_rated",
	"most_recent":      "most_recent",
	"biggest_discount": "biggest_discount",
}

// tools returns the four tools in the same order as the Python server.
func tools() []*mcpserver.Tool {
	return []*mcpserver.Tool{
		{
			Name:        "search_products",
			Description: descSearchProducts,
			Properties: []mcpserver.NamedProperty{
				{Name: "query", Prop: mcpserver.Property{Type: "string", Title: "Query"}},
				{Name: "page", Prop: mcpserver.Property{Type: "integer", Title: "Page", Default: 1}},
				{Name: "page_size", Prop: mcpserver.Property{Type: "integer", Title: "Page Size", Default: 20}},
				{Name: "sort", Prop: mcpserver.Property{Type: "string", Title: "Sort", Default: "most_searched"}},
			},
			Required: []string{"query"},
			Handler:  handleSearchProducts,
		},
		{
			Name:        "product_details",
			Description: descProductDetails,
			Properties: []mcpserver.NamedProperty{
				{Name: "product_id", Prop: mcpserver.Property{Type: "string", Title: "Product Id"}},
			},
			Required: []string{"product_id"},
			Handler:  handleProductDetails,
		},
		{
			Name:        "zip_lookup",
			Description: descZipLookup,
			Properties: []mcpserver.NamedProperty{
				{Name: "cep", Prop: mcpserver.Property{Type: "string", Title: "Cep"}},
			},
			Required: []string{"cep"},
			Handler:  handleZipLookup,
		},
		{
			Name:        "shipping_quote",
			Description: descShippingQuote,
			Properties: []mcpserver.NamedProperty{
				{Name: "product_id", Prop: mcpserver.Property{Type: "string", Title: "Product Id"}},
				{Name: "cep", Prop: mcpserver.Property{Type: "string", Title: "Cep"}},
				{Name: "quantity", Prop: mcpserver.Property{Type: "integer", Title: "Quantity", Default: 1}},
			},
			Required: []string{"product_id", "cep"},
			Handler:  handleShippingQuote,
		},
	}
}

func handleSearchProducts(_ context.Context, args map[string]any) (any, error) {
	query := argString(args, "query")

	page := argInt(args, "page", 1)
	if page < 1 {
		page = 1
	}
	pageSize := argInt(args, "page_size", 20)
	if pageSize < 1 {
		pageSize = 1
	}
	if pageSize > 60 {
		pageSize = 60
	}

	sort := "most_searched"
	if raw, ok := args["sort"]; ok && raw != nil {
		if s, ok := raw.(string); ok {
			sort = s
		}
	}
	sortVal := sortMap[sort]
	if sortVal == "" {
		sortVal = "most_searched"
	}

	data := get("/catalog/v2/products",
		"query", query,
		"page_number", strconv.Itoa(page),
		"page_size", strconv.Itoa(pageSize),
		"facet_filters", "",
		"sort", sortVal,
		"variant", "catalog",
	)
	if m := asM(data); m != nil {
		if _, ok := m.Get("error"); ok {
			return m, nil
		}
	}

	root := asM(data)
	meta := asM(mget(root, "meta"))
	items := asSlice(mget(root, "data"))
	pageObj := asM(mget(meta, "page"))

	respPage := any(page)
	if pageObj != nil {
		if v, ok := pageObj.Get("number"); ok {
			respPage = v
		}
	}
	respSize := any(pageSize)
	if pageObj != nil {
		if v, ok := pageObj.Get("size"); ok {
			respSize = v
		}
	}

	results := make([]any, 0, len(items))
	for _, item := range items {
		results = append(results, product(item))
	}

	return omap.From(
		"query", query,
		"sort", sort,
		"page", respPage,
		"page_size", respSize,
		"total", mget(meta, "total_items_count"),
		"total_pages", mget(meta, "total_pages_count"),
		"count", len(items),
		"results", results,
	), nil
}

func handleProductDetails(_ context.Context, args map[string]any) (any, error) {
	productID := argString(args, "product_id")
	data := get("/catalog/v2/products/" + productID)
	m := asM(data)
	if m == nil {
		return errMap(fmt.Sprintf("could not parse product %s", productID)), nil
	}
	if _, ok := m.Get("error"); ok {
		return m, nil
	}
	// Single-product responses are the JSON:API resource directly.
	if _, ok := m.Get("attributes"); ok {
		return omap.From("result", product(m)), nil
	}
	if raw, ok := m.Get("data"); ok {
		if arr := asSlice(raw); len(arr) > 0 {
			return omap.From("result", product(arr[0])), nil
		}
		if inner := asM(raw); inner != nil {
			return omap.From("result", product(inner)), nil
		}
	}
	return errMap(fmt.Sprintf("could not parse product %s", productID)), nil
}

func handleZipLookup(_ context.Context, args map[string]any) (any, error) {
	digits := cepDigits(args["cep"])
	if len(digits) != 8 {
		return errMap("CEP must have 8 digits"), nil
	}
	return get("/shipping/v1/zip-query?zip_code=" + digits), nil
}

func handleShippingQuote(_ context.Context, args map[string]any) (any, error) {
	digits := cepDigits(args["cep"])
	if len(digits) != 8 {
		return errMap("CEP must have 8 digits"), nil
	}
	pid, err := strconv.ParseInt(strings.TrimSpace(argString(args, "product_id")), 10, 64)
	if err != nil {
		return errMap("product_id must be numeric"), nil
	}
	qty := argInt(args, "quantity", 1)
	if qty < 1 {
		qty = 1
	}

	token := sessionToken()
	if isFalsy(token) {
		return errMap("could not obtain KaBuM! session token"), nil
	}

	body := omap.From(
		"zip_code", digits,
		"sellers", []any{
			omap.From(
				"seller_name", "KaBuM!",
				"seller_id", "0",
				"products", []any{
					omap.From("code", pid, "quantity", qty, "offer_id", 0),
				},
			),
		},
		"client", omap.From(
			"id", "",
			"session", token,
			"type", "F",
			"is_prime", false,
		),
		"store", 5,
		"origin", "product",
	)

	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		Post(baseURL + "/shipping/v4/quotation")
	if err != nil {
		return omap.From("error", err.Error()), nil
	}
	if resp.StatusCode != 200 {
		return errMap(fmt.Sprintf("KaBuM! shipping API returned HTTP %d", resp.StatusCode)), nil
	}
	data, err := decodeOrdered(resp.Bytes())
	if err != nil {
		return errMap("non-JSON response from KaBuM! shipping API"), nil
	}

	root := asM(data)
	quotes := make([]any, 0)
	for _, q := range asSlice(mget(root, "quotes")) {
		qm := asM(q)
		deliveries := make([]any, 0)
		for _, d := range asSlice(mget(qm, "deliveries")) {
			dm := asM(d)
			arrival := mget(dm, "arrival_date")
			if isFalsy(arrival) {
				arrival = mget(dm, "expected_arrival_date")
			}
			deliveries = append(deliveries, omap.From(
				"name", mget(dm, "name"),
				"type", mget(dm, "type"),
				"price", mget(dm, "price"),
				"original_price", mget(dm, "original_price"),
				"max_days", mget(dm, "max_days"),
				"arrival_date", arrival,
			))
		}
		quotes = append(quotes, omap.From(
			"seller", mget(qm, "seller_name"),
			"deliveries", deliveries,
		))
	}

	return omap.From(
		"cep", digits,
		"product_id", pid,
		"quantity", qty,
		"quotes", quotes,
	), nil
}

func sessionToken() any {
	data := get("/lg/v1/session/new-token")
	if m := asM(data); m != nil {
		return mget(m, "session")
	}
	return nil
}

// get mirrors the Python _get helper, including its two error dicts.
func get(path string, pairs ...string) any {
	target := baseURL + path
	if len(pairs) > 0 {
		target += "?" + encodePairs(pairs...)
	}
	resp, err := client.R().Get(target)
	if err != nil {
		return omap.From("error", err.Error())
	}
	if resp.StatusCode != 200 {
		return errMap(fmt.Sprintf("KaBuM! API returned HTTP %d", resp.StatusCode))
	}
	data, err := decodeOrdered(resp.Bytes())
	if err != nil {
		return errMap("non-JSON response from KaBuM! API")
	}
	return data
}

// product mirrors the Python _product helper. Key order matches exactly.
func product(item any) *omap.M {
	it := asM(item)
	attrs := asM(mget(it, "attributes"))
	pid := mget(it, "id")

	slug := asString(mget(attrs, "product_link"))
	link := siteURL + "/produto/" + pyStr(pid)
	if slug != "" {
		link += "/" + slug
	}

	images := mget(attrs, "images")
	var image any
	if list := asSlice(images); len(list) > 0 {
		image = list[0]
	}
	if images == nil {
		images = []any{}
	}

	var manufacturerName any
	if manufacturer := asM(mget(attrs, "manufacturer")); manufacturer != nil {
		manufacturerName = mget(manufacturer, "name")
	}

	return omap.From(
		"id", pid,
		"name", mget(attrs, "title"),
		"price", mget(attrs, "price"),
		"price_with_discount", mget(attrs, "price_with_discount"),
		"old_price", mget(attrs, "old_price"),
		"discount_percentage", mget(attrs, "discount_percentage"),
		"available", mget(attrs, "available"),
		"stock", mget(attrs, "stock"),
		"rating", mget(attrs, "average_of_ratings"),
		"rating_count", mget(attrs, "number_of_ratings"),
		"manufacturer", manufacturerName,
		"image", image,
		"images", images,
		"category", mget(attrs, "menu"),
		"max_installment", mget(attrs, "max_installment"),
		"is_marketplace", mget(attrs, "is_marketplace"),
		"has_free_shipping", mget(attrs, "has_free_shipping"),
		"warranty", mget(attrs, "warranty"),
		"url", link,
	)
}

func errMap(message string) *omap.M {
	return omap.From("error", message)
}

// pyStr matches Python's str() for the values that reach a product URL.
func pyStr(v any) string {
	if v == nil {
		return "None"
	}
	return fmt.Sprint(v)
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
		n, _ := x.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
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

// cepDigits mirrors "".join(c for c in str(cep) if c.isdigit()).
func cepDigits(v any) string {
	s := argString(map[string]any{"cep": v}, "cep")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
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
