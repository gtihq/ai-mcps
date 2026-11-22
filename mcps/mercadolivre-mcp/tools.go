package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gtihq/ai-mcps/internal/httpclient"
	"github.com/gtihq/ai-mcps/internal/mcpserver"
	"github.com/gtihq/ai-mcps/internal/omap"
)

const (
	defaultAPIBaseURL = "https://api.mercadolibre.com"
	searchBaseURL     = "https://lista.mercadolivre.com.br"
	ordersURL         = "https://myaccount.mercadolivre.com.br/my_purchases/list"
	requestTimeout    = 40 * time.Second

	notLoggedIn = "not logged in: run `python login.py` first"
)

var (
	// apiBaseURL is a package variable so tests can point the shipping call at
	// an httptest server. The public item and search URLs stay fixed.
	apiBaseURL = defaultAPIBaseURL

	// client mirrors httpx.AsyncClient(timeout=40). No extra headers are set,
	// exactly like the Python server.
	client = httpclient.New(nil, requestTimeout)

	// slugRE is the Python re.sub(r"[^a-z0-9]+", "-", s) pattern.
	slugRE = regexp.MustCompile(`[^a-z0-9]+`)
)

// Docstrings, verbatim, from the original Python server. The em dash in
// descSearchProducts is written as its Unicode escape so this repository keeps
// its "no em dash" rule while the wire description stays byte-for-byte equal.
const (
	descSearchProducts = "Keyword search on Mercado Livre (Brasil) with price, old price,\n" +
		"rating, review count and product link. Logged-out \u2014 no key, no login.\n\n" +
		"Note: runs a real browser, so each search takes a few seconds.\n"

	descShippingOptions = "Quote shipping (frete) options for a Mercado Livre item to a Brazilian\n" +
		"CEP. `item_id` is the MLB... id returned by search_products. Returns\n" +
		"delivery options with cost (BRL), method and estimated delivery date.\n" +
		"Ask the user for their CEP if not provided."

	descListOrders = "List the logged-in user's Mercado Livre purchases with shipping status.\n\n" +
		"Requires a logged-in session: run `python login.py` first. Uses a real\n" +
		"browser and the saved profile. Returns the purchases page text and any\n" +
		"detected order/pack ids and shipping statuses.\n"
)

// tools returns the three tools in the same order and shape as the Python
// server, including the full docstrings and argument schemas.
func tools() []*mcpserver.Tool {
	return []*mcpserver.Tool{
		{
			Name:        "search_products",
			Description: descSearchProducts,
			Properties: []mcpserver.NamedProperty{
				{Name: "query", Prop: mcpserver.Property{Type: "string", Title: "Query"}},
				{Name: "limit", Prop: mcpserver.Property{Type: "integer", Title: "Limit", Default: 20}},
			},
			Required: []string{"query"},
			Handler:  handleSearchProducts,
		},
		{
			Name:        "shipping_options",
			Description: descShippingOptions,
			Properties: []mcpserver.NamedProperty{
				{Name: "item_id", Prop: mcpserver.Property{Type: "string", Title: "Item Id"}},
				{Name: "cep", Prop: mcpserver.Property{Type: "string", Title: "Cep"}},
			},
			Required: []string{"item_id", "cep"},
			Handler:  handleShippingOptions,
		},
		{
			Name:        "list_orders",
			Description: descListOrders,
			Handler:     handleListOrders,
		},
	}
}

func handleSearchProducts(_ context.Context, args map[string]any) (any, error) {
	limit := argInt(args, "limit", 20)
	return searchProducts(argString(args, "query"), limit), nil
}

func handleShippingOptions(_ context.Context, args map[string]any) (any, error) {
	digits := cepDigits(args["cep"])
	if len(digits) != 8 {
		return errMap("CEP must have 8 digits"), nil
	}
	iid := strings.TrimSpace(argString(args, "item_id"))
	if !strings.HasPrefix(strings.ToUpper(iid), "MLB") {
		return errMap("item_id must be an MLB... id from search_products"), nil
	}

	resp, err := client.R().Get(shippingURL(iid, digits))
	if err != nil {
		return errMap(fmt.Sprintf("request failed: %s: %s", pyExcName(err), err.Error())), nil
	}
	if resp.StatusCode != http.StatusOK {
		return errMap(fmt.Sprintf("Mercado Livre API returned HTTP %d: %s",
			resp.StatusCode, truncateRunes(resp.String(), 300))), nil
	}
	data, err := decodeOrdered(resp.Bytes())
	if err != nil {
		return errMap("non-JSON response from Mercado Livre API"), nil
	}

	root := asM(data)
	return omap.From(
		"item_id", iid,
		"cep", digits,
		"destination", mget(root, "destination"),
		"options", mapOptions(mget(root, "options")),
	), nil
}

func handleListOrders(_ context.Context, _ map[string]any) (any, error) {
	profile := browserProfileDir()
	if _, err := os.Stat(profile); err != nil {
		return errMap(notLoggedIn), nil
	}
	return listOrders(profile), nil
}

// shippingURL mirrors the f-string in shipping_options.
func shippingURL(iid, digits string) string {
	return apiBaseURL + "/items/" + iid + "/shipping_options?zip_code=" + digits
}

// mapOptions mirrors the per-option dict in shipping_options. Key order and
// fallbacks match the Python source exactly.
func mapOptions(raw any) []any {
	options := make([]any, 0)
	for _, item := range asSlice(raw) {
		o := asM(item)
		edt := asM(mget(o, "estimated_delivery_time"))
		options = append(options, omap.From(
			"id", mget(o, "id"),
			"shipping_method_type", mget(o, "shipping_method_type"),
			"shipping_option_type", mget(o, "shipping_option_type"),
			"cost", mget(o, "cost"),
			"base_cost", mget(o, "base_cost"),
			"list_cost", mget(o, "list_cost"),
			"currency", mget(o, "currency_id"),
			"display", mget(o, "display"),
			"estimated_delivery_date", mget(edt, "date"),
			"shipping_hours", mget(edt, "shipping"),
			"handling_hours", mget(edt, "handling"),
		))
	}
	return options
}

func errMap(message string) *omap.M {
	return omap.From("error", message)
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
	if isFalsy(v) {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
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

// slug mirrors _slug. Non-ASCII letters (including accented Portuguese
// vowels) are not in [a-z0-9] and become hyphens, exactly like the Python
// regular expression. Runs of them collapse into a single hyphen.
func slug(query string) string {
	s := slugRE.ReplaceAllString(strings.TrimSpace(strings.ToLower(query)), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "busca"
	}
	return s
}

// searchURL mirrors the f-string in search_products.
func searchURL(s string) string {
	return searchBaseURL + "/" + s
}

// truncateRunes cuts by characters, like Python's str slicing.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// pyExcName stands in for Python's type(e).__name__, which has no direct Go
// equivalent. The concrete error type name is used when one is available.
func pyExcName(err error) string {
	if err == nil {
		return "Error"
	}
	t := reflect.TypeOf(err)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if name := t.Name(); name != "" {
		return name
	}
	return "Error"
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
