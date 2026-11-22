package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
	playwright "github.com/mxschmitt/playwright-go"
)

// serve points the package-level apiBaseURL at a test server for one test.
func serve(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	old := apiBaseURL
	apiBaseURL = srv.URL
	t.Cleanup(func() {
		apiBaseURL = old
		srv.Close()
	})
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantJSON(t *testing.T, got any, want string) {
	t.Helper()
	if s := toJSON(t, got); s != want {
		t.Fatalf("JSON mismatch\n got: %s\nwant: %s", s, want)
	}
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func decodeTest(t *testing.T, s string) any {
	t.Helper()
	v, err := decodeOrdered([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestToolSchemas(t *testing.T) {
	s := mcpserver.New("mercadolivre", tools()...)
	line := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	want := `{"jsonrpc":"2.0","id":1,"result":{"tools":[` +
		`{"name":"search_products","description":` + jsonString(t, descSearchProducts) + `,"inputSchema":{"properties":{` +
		`"query":{"title":"Query","type":"string"},` +
		`"limit":{"default":20,"title":"Limit","type":"integer"}},` +
		`"required":["query"],"title":"search_productsArguments","type":"object"}},` +
		`{"name":"shipping_options","description":` + jsonString(t, descShippingOptions) + `,"inputSchema":{"properties":{` +
		`"item_id":{"title":"Item Id","type":"string"},` +
		`"cep":{"title":"Cep","type":"string"}},` +
		`"required":["item_id","cep"],"title":"shipping_optionsArguments","type":"object"}},` +
		`{"name":"list_orders","description":` + jsonString(t, descListOrders) + `,"inputSchema":{"properties":{},` +
		`"title":"list_ordersArguments","type":"object"}}` +
		`]}}` + "\n"
	if line != want {
		t.Fatalf("tools/list mismatch\n got: %s\nwant: %s", line, want)
	}
}

func TestSlug(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"Notebook Gamer", "notebook-gamer"},
		{"  iphone 15  pro  ", "iphone-15-pro"},
		{"---", "busca"},
		{"   ", "busca"},
		{"", "busca"},
		{"fone de ouvido", "fone-de-ouvido"},
		// Accented letters are outside [a-z0-9], so the Python re.sub turns
		// each one into a hyphen. No accent survives in the slug.
		{"ação", "a-o"},
		{"São Paulo", "s-o-paulo"},
		{"AÇÃO", "a-o"},
		{"tv 4k@50\"", "tv-4k-50"},
	}
	for _, tc := range cases {
		if got := slug(tc.query); got != tc.want {
			t.Errorf("slug(%q) = %q, want %q", tc.query, got, tc.want)
		}
		if got := slug(tc.query); strings.ContainsAny(got, "áéíóúâêôãõçÁÉÍÓÚÂÊÔÃÕÇ") {
			t.Errorf("slug(%q) kept an accent: %q", tc.query, got)
		}
		if got := searchURL(slug(tc.query)); !strings.HasPrefix(got, "https://lista.mercadolivre.com.br/") {
			t.Errorf("unexpected search URL %q", got)
		}
	}
	if got := searchURL("notebook-gamer"); got != "https://lista.mercadolivre.com.br/notebook-gamer" {
		t.Fatalf("search URL = %q", got)
	}
}

func TestShippingValidation(t *testing.T) {
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no HTTP request expected, got %s %s", r.Method, r.URL)
	})

	cases := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"short cep", map[string]any{"item_id": "MLB123", "cep": "123"}, "CEP must have 8 digits"},
		{"letters cep", map[string]any{"item_id": "MLB123", "cep": "abcdefgh"}, "CEP must have 8 digits"},
		{"long cep", map[string]any{"item_id": "MLB123", "cep": "010010000"}, "CEP must have 8 digits"},
		{"bad item", map[string]any{"item_id": "MLA123", "cep": "01001000"}, "item_id must be an MLB... id from search_products"},
		{"empty item", map[string]any{"cep": "01001000"}, "item_id must be an MLB... id from search_products"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := handleShippingOptions(context.Background(), tc.args)
			if err != nil {
				t.Fatal(err)
			}
			wantJSON(t, got, `{"error":`+strconv.Quote(tc.wantErr)+`}`)
		})
	}
}

func TestShippingURL(t *testing.T) {
	var gotPath, gotQuery string
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{}`)
	})
	if got := shippingURL("MLB123", "01001000"); !strings.HasSuffix(got, "/items/MLB123/shipping_options?zip_code=01001000") {
		t.Fatalf("shipping URL = %q", got)
	}
	if _, err := handleShippingOptions(context.Background(), map[string]any{
		"item_id": "MLB123", "cep": "01001-000",
	}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/items/MLB123/shipping_options" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "zip_code=01001000" {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestMapOptions(t *testing.T) {
	const body = `{"destination":{"city":"Sao Paulo"},"options":[` +
		`{"id":123,"shipping_method_type":"normal","shipping_option_type":"standard",` +
		`"cost":15.5,"base_cost":10,"list_cost":20,"currency_id":"BRL","display":"Frete normal",` +
		`"estimated_delivery_time":{"date":"2026-11-30","shipping":24,"handling":12}},` +
		`{"id":null,"estimated_delivery_time":null},` +
		`{}` +
		`]}`

	wantKeys := "id,shipping_method_type,shipping_option_type,cost,base_cost,list_cost,currency,display,estimated_delivery_date,shipping_hours,handling_hours"
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})
	got, err := handleShippingOptions(context.Background(), map[string]any{
		"item_id": "MLB123", "cep": "01001000",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := asM(got)
	if gotID := mget(m, "item_id"); gotID != "MLB123" {
		t.Errorf("item_id = %v", gotID)
	}
	if gotCEP := mget(m, "cep"); gotCEP != "01001000" {
		t.Errorf("cep = %v", gotCEP)
	}
	wantJSON(t, mget(m, "destination"), `{"city":"Sao Paulo"}`)

	options := asSlice(mget(m, "options"))
	if len(options) != 3 {
		t.Fatalf("want 3 options, got %d", len(options))
	}
	for i, opt := range options {
		om := asM(opt)
		if keys := strings.Join(om.Keys(), ","); keys != wantKeys {
			t.Errorf("option %d key order = %q, want %q", i, keys, wantKeys)
		}
	}
	wantJSON(t, options[0], `{"id":123,"shipping_method_type":"normal","shipping_option_type":"standard",`+
		`"cost":15.5,"base_cost":10,"list_cost":20,"currency":"BRL","display":"Frete normal",`+
		`"estimated_delivery_date":"2026-11-30","shipping_hours":24,"handling_hours":12}`)
	wantJSON(t, options[1], `{"id":null,"shipping_method_type":null,"shipping_option_type":null,`+
		`"cost":null,"base_cost":null,"list_cost":null,"currency":null,"display":null,`+
		`"estimated_delivery_date":null,"shipping_hours":null,"handling_hours":null}`)
	wantJSON(t, options[2], `{"id":null,"shipping_method_type":null,"shipping_option_type":null,`+
		`"cost":null,"base_cost":null,"list_cost":null,"currency":null,"display":null,`+
		`"estimated_delivery_date":null,"shipping_hours":null,"handling_hours":null}`)
}

func TestMapOptionsFalsyValues(t *testing.T) {
	got := mapOptions(decodeTest(t, `[{"options":[],"estimated_delivery_time":{}},`+
		`{"options":null,"estimated_delivery_time":{}},`+
		`{"estimated_delivery_time":{"date":"","shipping":0,"handling":0}}]`))
	if len(got) != 3 {
		t.Fatalf("want 3 options, got %d", len(got))
	}
	wantJSON(t, got[2], `{"id":null,"shipping_method_type":null,"shipping_option_type":null,`+
		`"cost":null,"base_cost":null,"list_cost":null,"currency":null,"display":null,`+
		`"estimated_delivery_date":"","shipping_hours":0,"handling_hours":0}`)
}

func TestShippingErrors(t *testing.T) {
	t.Run("http status truncates body to 300 characters", func(t *testing.T) {
		body := strings.Repeat("é", 305)
		serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, body)
		})
		got, err := handleShippingOptions(context.Background(), map[string]any{
			"item_id": "MLB1", "cep": "01001000",
		})
		if err != nil {
			t.Fatal(err)
		}
		want := `{"error":` + jsonString(t, "Mercado Livre API returned HTTP 500: "+strings.Repeat("é", 300)) + `}`
		wantJSON(t, got, want)
	})

	t.Run("non json", func(t *testing.T) {
		serve(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `<html>not json</html>`)
		})
		got, err := handleShippingOptions(context.Background(), map[string]any{
			"item_id": "MLB1", "cep": "01001000",
		})
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"error":"non-JSON response from Mercado Livre API"}`)
	})

	t.Run("request failed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		old := apiBaseURL
		apiBaseURL = srv.URL
		t.Cleanup(func() { apiBaseURL = old })
		srv.Close()
		got, err := handleShippingOptions(context.Background(), map[string]any{
			"item_id": "MLB1", "cep": "01001000",
		})
		if err != nil {
			t.Fatal(err)
		}
		s := toJSON(t, got)
		if !strings.Contains(s, `"error":"request failed: `) {
			t.Fatalf("unexpected error: %s", s)
		}
	})
}

func TestListOrdersMissingProfile(t *testing.T) {
	old := profileDirOverride
	profileDirOverride = filepath.Join(t.TempDir(), "missing-profile")
	t.Cleanup(func() { profileDirOverride = old })

	if got := browserProfileDir(); got != profileDirOverride {
		t.Fatalf("profile dir = %q, want %q", got, profileDirOverride)
	}
	got, err := handleListOrders(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, got, `{"error":`+jsonString(t, notLoggedIn)+`}`)
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("abcdef", 3); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := truncateRunes("abc", 10); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := truncateRunes("áéíóú", 3); got != "áéí" {
		t.Fatalf("got %q", got)
	}
}

func TestCookieHeader(t *testing.T) {
	got := cookieHeader([]playwright.Cookie{
		{Name: "a", Value: "1"},
		{Name: "", Value: "skip"},
		{Name: "b", Value: "2"},
	})
	if got != "a=1; b=2" {
		t.Fatalf("got %q", got)
	}
}
