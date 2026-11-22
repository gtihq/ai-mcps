package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
)

// serve points the package-level baseURL at a test server for one test.
func serve(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	old := baseURL
	baseURL = srv.URL
	t.Cleanup(func() {
		baseURL = old
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

func TestToolSchemas(t *testing.T) {
	s := mcpserver.New("kabum", tools()...)
	line := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	want := `{"jsonrpc":"2.0","id":1,"result":{"tools":[` +
		`{"name":"search_products","description":` + strconv.Quote(descSearchProducts) + `,"inputSchema":{"properties":{` +
		`"query":{"title":"Query","type":"string"},` +
		`"page":{"default":1,"title":"Page","type":"integer"},` +
		`"page_size":{"default":20,"title":"Page Size","type":"integer"},` +
		`"sort":{"default":"most_searched","title":"Sort","type":"string"}},` +
		`"required":["query"],"title":"search_productsArguments","type":"object"}},` +
		`{"name":"product_details","description":` + strings.ReplaceAll(strings.ReplaceAll(strconv.Quote(descProductDetails), "<", `\u003c`), ">", `\u003e`) + `,"inputSchema":{"properties":{` +
		`"product_id":{"title":"Product Id","type":"string"}},` +
		`"required":["product_id"],"title":"product_detailsArguments","type":"object"}},` +
		`{"name":"zip_lookup","description":` + strconv.Quote(descZipLookup) + `,"inputSchema":{"properties":{` +
		`"cep":{"title":"Cep","type":"string"}},` +
		`"required":["cep"],"title":"zip_lookupArguments","type":"object"}},` +
		`{"name":"shipping_quote","description":` + strconv.Quote(descShippingQuote) + `,"inputSchema":{"properties":{` +
		`"product_id":{"title":"Product Id","type":"string"},` +
		`"cep":{"title":"Cep","type":"string"},` +
		`"quantity":{"default":1,"title":"Quantity","type":"integer"}},` +
		`"required":["product_id","cep"],"title":"shipping_quoteArguments","type":"object"}}` +
		`]}}` + "\n"
	if line != want {
		t.Fatalf("tools/list mismatch\n got: %s\nwant: %s", line, want)
	}
}

func TestSearchProductsClamping(t *testing.T) {
	const response = `{"data":[],"meta":{"page":{"number":2,"size":60},"total_items_count":123,"total_pages_count":3}}`
	var queries []string
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/catalog/v2/products" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		queries = append(queries, r.URL.RawQuery)
		_, _ = io.WriteString(w, response)
	})

	cases := []struct {
		args      map[string]any
		wantQuery string
	}{
		{
			args:      map[string]any{"query": "rtx 4060", "page": float64(-3), "page_size": float64(0)},
			wantQuery: "query=rtx+4060&page_number=1&page_size=1&facet_filters=&sort=most_searched&variant=catalog",
		},
		{
			args:      map[string]any{"query": "gpu", "page": float64(2), "page_size": float64(100), "sort": "lowest_price"},
			wantQuery: "query=gpu&page_number=2&page_size=60&facet_filters=&sort=lowest_price&variant=catalog",
		},
		{
			args:      map[string]any{"query": "gpu", "page": float64(0), "page_size": float64(20), "sort": "unknown"},
			wantQuery: "query=gpu&page_number=1&page_size=20&facet_filters=&sort=most_searched&variant=catalog",
		},
	}

	for _, tc := range cases {
		got, err := handleSearchProducts(context.Background(), tc.args)
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"query":`+strconv.Quote(tc.args["query"].(string))+`,"sort":`+strconv.Quote(sortArg(tc.args))+`,"page":2,"page_size":60,"total":123,"total_pages":3,"count":0,"results":[]}`)
	}
	if len(queries) != len(cases) {
		t.Fatalf("want %d requests, got %d", len(cases), len(queries))
	}
	for i, tc := range cases {
		if queries[i] != tc.wantQuery {
			t.Errorf("request %d query mismatch\n got: %s\nwant: %s", i, queries[i], tc.wantQuery)
		}
	}
}

func sortArg(args map[string]any) string {
	if v, ok := args["sort"].(string); ok {
		return v
	}
	return "most_searched"
}

func TestProductMapping(t *testing.T) {
	const resource = `{"id":12345,"attributes":{"title":"Placa RTX","price":2000.5,` +
		`"price_with_discount":1899.9,"old_price":2100,"discount_percentage":10,` +
		`"available":true,"stock":5,"average_of_ratings":4.5,"number_of_ratings":12,` +
		`"manufacturer":{"name":"NVIDIA"},"images":["http://img/1.jpg","http://img/2.jpg"],` +
		`"menu":"Hardware","max_installment":12,"is_marketplace":false,` +
		`"has_free_shipping":true,"warranty":"12 months","product_link":"placa-rtx-12345"}}`

	var item any
	if err := json.Unmarshal([]byte(resource), &item); err != nil {
		t.Fatal(err)
	}
	mapped := product(item)
	keys := "id,name,price,price_with_discount,old_price,discount_percentage,available,stock,rating,rating_count,manufacturer,image,images,category,max_installment,is_marketplace,has_free_shipping,warranty,url"
	if got := strings.Join(mapped.Keys(), ","); got != keys {
		t.Fatalf("key order mismatch\n got: %s\nwant: %s", got, keys)
	}
	wantJSON(t, mapped, `{"id":12345,"name":"Placa RTX","price":2000.5,"price_with_discount":1899.9,`+
		`"old_price":2100,"discount_percentage":10,"available":true,"stock":5,"rating":4.5,`+
		`"rating_count":12,"manufacturer":"NVIDIA","image":"http://img/1.jpg",`+
		`"images":["http://img/1.jpg","http://img/2.jpg"],"category":"Hardware",`+
		`"max_installment":12,"is_marketplace":false,"has_free_shipping":true,`+
		`"warranty":"12 months","url":"https://www.kabum.com.br/produto/12345/placa-rtx-12345"}`)
}

func TestProductMappingEmptyAttributes(t *testing.T) {
	var item any
	if err := json.Unmarshal([]byte(`{"id":99,"attributes":{}}`), &item); err != nil {
		t.Fatal(err)
	}
	wantJSON(t, product(item), `{"id":99,"name":null,"price":null,"price_with_discount":null,`+
		`"old_price":null,"discount_percentage":null,"available":null,"stock":null,`+
		`"rating":null,"rating_count":null,"manufacturer":null,"image":null,"images":[],`+
		`"category":null,"max_installment":null,"is_marketplace":null,`+
		`"has_free_shipping":null,"warranty":null,"url":"https://www.kabum.com.br/produto/99"}`)
}

func TestProductDetailsFormats(t *testing.T) {
	const resource = `{"id":7,"attributes":{"title":"Mouse","product_link":"mouse-7","images":["i.png"]}}`
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "resource directly",
			body: resource,
			want: `{"result":{"id":7,"name":"Mouse","price":null,"price_with_discount":null,"old_price":null,"discount_percentage":null,"available":null,"stock":null,"rating":null,"rating_count":null,"manufacturer":null,"image":"i.png","images":["i.png"],"category":null,"max_installment":null,"is_marketplace":null,"has_free_shipping":null,"warranty":null,"url":"https://www.kabum.com.br/produto/7/mouse-7"}}`,
		},
		{
			name: "data list",
			body: `{"data":[` + resource + `]}`,
			want: `{"result":{"id":7,"name":"Mouse","price":null,"price_with_discount":null,"old_price":null,"discount_percentage":null,"available":null,"stock":null,"rating":null,"rating_count":null,"manufacturer":null,"image":"i.png","images":["i.png"],"category":null,"max_installment":null,"is_marketplace":null,"has_free_shipping":null,"warranty":null,"url":"https://www.kabum.com.br/produto/7/mouse-7"}}`,
		},
		{
			name: "data object",
			body: `{"data":` + resource + `}`,
			want: `{"result":{"id":7,"name":"Mouse","price":null,"price_with_discount":null,"old_price":null,"discount_percentage":null,"available":null,"stock":null,"rating":null,"rating_count":null,"manufacturer":null,"image":"i.png","images":["i.png"],"category":null,"max_installment":null,"is_marketplace":null,"has_free_shipping":null,"warranty":null,"url":"https://www.kabum.com.br/produto/7/mouse-7"}}`,
		},
		{
			name: "empty data list",
			body: `{"data":[]}`,
			want: `{"error":"could not parse product 7"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/catalog/v2/products/7" {
					t.Errorf("unexpected path %q", r.URL.Path)
				}
				_, _ = io.WriteString(w, tc.body)
			})
			got, err := handleProductDetails(context.Background(), map[string]any{"product_id": "7"})
			if err != nil {
				t.Fatal(err)
			}
			wantJSON(t, got, tc.want)
		})
	}
}

func TestCEPErrors(t *testing.T) {
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no HTTP request expected, got %s %s", r.Method, r.URL)
	})
	for _, cep := range []string{"123", "abcdefgh", "010010000"} {
		got, err := handleZipLookup(context.Background(), map[string]any{"cep": cep})
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"error":"CEP must have 8 digits"}`)

		got, err = handleShippingQuote(context.Background(), map[string]any{
			"product_id": "1", "cep": cep,
		})
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"error":"CEP must have 8 digits"}`)
	}
}

func TestZipLookup(t *testing.T) {
	const body = `{"zip_code":"01001000","street":"Praça da Sé","city":"São Paulo","state":"SP"}`
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "zip_code=01001000" {
			t.Errorf("unexpected query %q", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, body)
	})
	got, err := handleZipLookup(context.Background(), map[string]any{"cep": "01001-000"})
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, got, body)

	got, err = handleZipLookup(context.Background(), map[string]any{"cep": "abc01001000xyz"})
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, got, body)
}

func TestShippingQuote(t *testing.T) {
	const tokenBody = `{"session":"tok123"}`
	const quoteBody = `{"quotes":[{"seller_name":"KaBuM!","deliveries":[` +
		`{"name":"Normal","type":"normal","price":15.5,"original_price":20,"max_days":5,"arrival_date":"2026-11-30"},` +
		`{"name":"Expresso","type":"express","price":30,"original_price":null,"max_days":2,"arrival_date":"","expected_arrival_date":"2026-11-25"}` +
		`]}]}`

	var requestBody string
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lg/v1/session/new-token":
			_, _ = io.WriteString(w, tokenBody)
		case "/shipping/v4/quotation":
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			requestBody = string(b)
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("unexpected Content-Type %q", r.Header.Get("Content-Type"))
			}
			_, _ = io.WriteString(w, quoteBody)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	got, err := handleShippingQuote(context.Background(), map[string]any{
		"product_id": "123", "cep": "01001-000", "quantity": float64(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, got, `{"cep":"01001000","product_id":123,"quantity":2,"quotes":[{"seller":"KaBuM!",`+
		`"deliveries":[{"name":"Normal","type":"normal","price":15.5,"original_price":20,`+
		`"max_days":5,"arrival_date":"2026-11-30"},{"name":"Expresso","type":"express",`+
		`"price":30,"original_price":null,"max_days":2,"arrival_date":"2026-11-25"}]}]}`)

	wantBody := `{"zip_code":"01001000","sellers":[{"seller_name":"KaBuM!","seller_id":"0",` +
		`"products":[{"code":123,"quantity":2,"offer_id":0}]}],"client":{"id":"",` +
		`"session":"tok123","type":"F","is_prime":false},"store":5,"origin":"product"}`
	if requestBody != wantBody {
		t.Fatalf("request body mismatch\n got: %s\nwant: %s", requestBody, wantBody)
	}
}

func TestShippingQuoteErrors(t *testing.T) {
	t.Run("non numeric product", func(t *testing.T) {
		serve(t, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("no HTTP request expected, got %s", r.URL)
		})
		got, err := handleShippingQuote(context.Background(), map[string]any{
			"product_id": "abc", "cep": "01001000",
		})
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"error":"product_id must be numeric"}`)
	})

	t.Run("no session token", func(t *testing.T) {
		serve(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{}`)
		})
		got, err := handleShippingQuote(context.Background(), map[string]any{
			"product_id": "1", "cep": "01001000",
		})
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"error":"could not obtain KaBuM! session token"}`)
	})
}

func TestGetErrors(t *testing.T) {
	t.Run("http status", func(t *testing.T) {
		serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		got, err := handleSearchProducts(context.Background(), map[string]any{"query": "x"})
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"error":"KaBuM! API returned HTTP 500"}`)
	})

	t.Run("non json", func(t *testing.T) {
		serve(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `<html>not json</html>`)
		})
		got, err := handleSearchProducts(context.Background(), map[string]any{"query": "x"})
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"error":"non-JSON response from KaBuM! API"}`)
	})

	t.Run("shipping http status", func(t *testing.T) {
		serve(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/lg/v1/session/new-token" {
				_, _ = io.WriteString(w, `{"session":"tok"}`)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
		})
		got, err := handleShippingQuote(context.Background(), map[string]any{
			"product_id": "1", "cep": "01001000",
		})
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"error":"KaBuM! shipping API returned HTTP 400"}`)
	})
}

func TestHeaders(t *testing.T) {
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		want := map[string]string{
			"accept":          "application/json, text/plain, */*",
			"accept-language": "pt-BR,pt;q=0.9,en-US;q=0.8,en;q=0.7",
			"user-agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
			"origin":          "https://www.kabum.com.br",
			"referer":         "https://www.kabum.com.br/",
		}
		for k, v := range want {
			if got := r.Header.Get(k); got != v {
				t.Errorf("header %s: got %q, want %q", k, got, v)
			}
		}
		_, _ = io.WriteString(w, `{"session":"ok"}`)
	})
	if _, err := handleZipLookup(context.Background(), map[string]any{"cep": "01001000"}); err != nil {
		t.Fatal(err)
	}
}
