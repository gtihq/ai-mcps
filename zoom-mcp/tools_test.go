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

// serve points the package-level requestBaseURL at a test server for one test.
func serve(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	old := requestBaseURL
	requestBaseURL = srv.URL
	t.Cleanup(func() {
		requestBaseURL = old
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

func decodeTest(t *testing.T, s string) any {
	t.Helper()
	v, err := decodeOrdered([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// nextDataHTML wraps a JSON payload in the exact script tag the regex expects.
func nextDataHTML(data string) string {
	return `<html><head></head><body>` + "\n" +
		`<script id="__NEXT_DATA__" type="application/json">` + data + `</script>` + "\n" +
		`</body></html>`
}

func TestToolSchemas(t *testing.T) {
	s := mcpserver.New("zoom", tools()...)
	line := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	want := `{"jsonrpc":"2.0","id":1,"result":{"tools":[` +
		`{"name":"search_products","description":` + strconv.Quote(descSearchProducts) + `,"inputSchema":{"properties":{` +
		`"query":{"title":"Query","type":"string"},` +
		`"page":{"default":1,"title":"Page","type":"integer"}},` +
		`"required":["query"],"title":"search_productsArguments","type":"object"}}` +
		`]}}` + "\n"
	if line != want {
		t.Fatalf("tools/list mismatch\n got: %s\nwant: %s", line, want)
	}
}

func TestNextData(t *testing.T) {
	t.Run("DOTALL spans newlines inside the JSON", func(t *testing.T) {
		html := `<html>` + "\n" +
			`<script id="__NEXT_DATA__" type="application/json">{"props":{"initialReduxState":{"hits":{"hits":[{"name":"A"}]}}}}</script>` + "\n" +
			`</html>`
		got := nextData(html)
		if got == nil {
			t.Fatal("expected parsed __NEXT_DATA__")
		}
		props := asM(mget(got, "props"))
		irs := asM(mget(props, "initialReduxState"))
		hits := asM(mget(irs, "hits"))
		wantJSON(t, mget(hits, "hits"), `[{"name":"A"}]`)
	})

	t.Run("absent script", func(t *testing.T) {
		if got := nextData(`<html><body>blocked</body></html>`); got != nil {
			t.Fatalf("expected nil, got %s", got)
		}
	})

	t.Run("invalid JSON payload", func(t *testing.T) {
		html := `<script id="__NEXT_DATA__" type="application/json">{not json}</script>`
		if got := nextData(html); got != nil {
			t.Fatalf("expected nil, got %s", got)
		}
	})

	t.Run("multiple scripts keeps the first __NEXT_DATA__", func(t *testing.T) {
		html := `<script>var a = 1;</script>` + "\n" +
			`<script id="__NEXT_DATA__" type="application/json">{"a":1}</script>` + "\n" +
			`<script id="__NEXT_DATA__" type="application/json">{"a":2}</script>`
		wantJSON(t, nextData(html), `{"a":1}`)
	})
}

func TestProductMapping(t *testing.T) {
	const hit = `{"name":"Fone Bluetooth","price":199.9,"bestOffer":{"merchantName":"KaBuM!"},` +
		`"merchants":[{"name":"KaBuM!"},{"name":"Amazon"},{"id":7}],"storeCount":2,` +
		`"preciseRating":4.7,"rating":4,"ratingTotal":123,"countOfComments":50,` +
		`"loweringPercentage":15,"categoryName":"Audio","image":"http://img/fone.jpg",` +
		`"url":"/produto/fone","stamps":["OFERTA","FRETE GRATIS"]}`

	mapped := product(decodeTest(t, hit))
	keys := "name,price,currency,best_store,stores,store_count,rating,rating_count,discount_percent,category,image,url,badges"
	if got := strings.Join(mapped.Keys(), ","); got != keys {
		t.Fatalf("key order mismatch\n got: %s\nwant: %s", got, keys)
	}
	wantJSON(t, mapped, `{"name":"Fone Bluetooth","price":199.9,"currency":"BRL","best_store":"KaBuM!",`+
		`"stores":["KaBuM!","Amazon"],"store_count":2,"rating":4.7,"rating_count":123,`+
		`"discount_percent":15,"category":"Audio","image":"http://img/fone.jpg",`+
		`"url":"https://www.zoom.com.br/produto/fone","badges":["OFERTA","FRETE GRATIS"]}`)
}

func TestProductFallbacks(t *testing.T) {
	t.Run("shortName, rating, countOfComments and empty stamps", func(t *testing.T) {
		const hit = `{"shortName":"Curto","rating":3.5,"countOfComments":9,` +
			`"bestOffer":{},"merchants":[{"name":""},{"name":null},{}],"url":""}`
		wantJSON(t, product(decodeTest(t, hit)), `{"name":"Curto","price":null,"currency":"BRL",`+
			`"best_store":null,"stores":[],"store_count":null,"rating":3.5,"rating_count":9,`+
			`"discount_percent":null,"category":null,"image":null,`+
			`"url":"https://www.zoom.com.br","badges":[]}`)
	})

	t.Run("primary keys win over fallbacks", func(t *testing.T) {
		const hit = `{"name":"Principal","shortName":"Curto","preciseRating":4.8,"rating":1,` +
			`"ratingTotal":500,"countOfComments":2,"stamps":["NOVO"]}`
		wantJSON(t, product(decodeTest(t, hit)), `{"name":"Principal","price":null,"currency":"BRL",`+
			`"best_store":null,"stores":[],"store_count":null,"rating":4.8,"rating_count":500,`+
			`"discount_percent":null,"category":null,"image":null,`+
			`"url":"https://www.zoom.com.br","badges":["NOVO"]}`)
	})

	t.Run("empty hit", func(t *testing.T) {
		wantJSON(t, product(decodeTest(t, `{}`)), `{"name":null,"price":null,"currency":"BRL",`+
			`"best_store":null,"stores":[],"store_count":null,"rating":null,"rating_count":null,`+
			`"discount_percent":null,"category":null,"image":null,`+
			`"url":"https://www.zoom.com.br","badges":[]}`)
	})
}

func TestSearchProductsParams(t *testing.T) {
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.RawQuery != "q=notebook+gamer&page=3" {
			t.Errorf("unexpected query %q", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, nextDataHTML(`{"props":{}}`))
	})

	got, err := handleSearchProducts(context.Background(), map[string]any{
		"query": "notebook gamer", "page": float64(3),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, got, `{"query":"notebook gamer","page":3,"total":null,"count":0,"results":[]}`)
}

func TestSearchProductsPagination(t *testing.T) {
	var queries []string
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		_, _ = io.WriteString(w, nextDataHTML(`{"props":{}}`))
	})

	cases := []struct {
		args     map[string]any
		wantPage string
	}{
		{map[string]any{"query": "x", "page": float64(0)}, "page=1"},
		{map[string]any{"query": "x", "page": float64(-3)}, "page=1"},
		{map[string]any{"query": "x", "page": float64(2)}, "page=2"},
		{map[string]any{"query": "x", "page": "5"}, "page=5"},
		{map[string]any{"query": "x"}, "page=1"},
	}

	for _, tc := range cases {
		got, err := handleSearchProducts(context.Background(), tc.args)
		if err != nil {
			t.Fatal(err)
		}
		page, err := strconv.Atoi(strings.TrimPrefix(tc.wantPage, "page="))
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"query":"x","page":`+strconv.Itoa(page)+`,"total":null,"count":0,"results":[]}`)
	}

	if len(queries) != len(cases) {
		t.Fatalf("want %d requests, got %d", len(cases), len(queries))
	}
	for i, tc := range cases {
		if queries[i] != "q=x&"+tc.wantPage {
			t.Errorf("request %d query mismatch\n got: %s\nwant: %s", i, queries[i], "q=x&"+tc.wantPage)
		}
	}
}

func TestSearchProductsFullResult(t *testing.T) {
	const data = `{"props":{"initialReduxState":{"hits":{"hits":[` +
		`{"name":"Fone","price":99.9,"stamps":["OFERTA"],"url":"/fone"},` +
		`{"shortName":"Mouse","url":"/mouse"},` +
		`123` +
		`],"pagination":{"nbHits":42}}}}}`
	body := `<script>console.log(1)</script>` + "\n" +
		nextDataHTML(data) + "\n" +
		`<script id="__NEXT_DATA__" type="application/json">{"props":{}}</script>`

	serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})

	got, err := handleSearchProducts(context.Background(), map[string]any{"query": "x"})
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, got, `{"query":"x","page":1,"total":42,"count":3,"results":[`+
		`{"name":"Fone","price":99.9,"currency":"BRL","best_store":null,"stores":[],`+
		`"store_count":null,"rating":null,"rating_count":null,"discount_percent":null,`+
		`"category":null,"image":null,"url":"https://www.zoom.com.br/fone","badges":["OFERTA"]},`+
		`{"name":"Mouse","price":null,"currency":"BRL","best_store":null,"stores":[],`+
		`"store_count":null,"rating":null,"rating_count":null,"discount_percent":null,`+
		`"category":null,"image":null,"url":"https://www.zoom.com.br/mouse","badges":[]}]}`)
}

func TestSearchProductsErrors(t *testing.T) {
	t.Run("http status", func(t *testing.T) {
		serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		got, err := handleSearchProducts(context.Background(), map[string]any{"query": "x"})
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"error":"Zoom returned HTTP 500"}`)
	})

	t.Run("missing __NEXT_DATA__", func(t *testing.T) {
		serve(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `<html><body>anti-bot challenge</body></html>`)
		})
		got, err := handleSearchProducts(context.Background(), map[string]any{"query": "x"})
		if err != nil {
			t.Fatal(err)
		}
		wantJSON(t, got, `{"error":"could not parse Zoom __NEXT_DATA__ (possible anti-bot challenge)"}`)
	})
}

func TestHeaders(t *testing.T) {
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		want := map[string]string{
			"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"accept-language": "pt-BR,pt;q=0.9,en-US;q=0.8,en;q=0.7",
			"user-agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		}
		for k, v := range want {
			if got := r.Header.Get(k); got != v {
				t.Errorf("header %s: got %q, want %q", k, got, v)
			}
		}
		_, _ = io.WriteString(w, nextDataHTML(`{"props":{}}`))
	})
	if _, err := handleSearchProducts(context.Background(), map[string]any{"query": "x"}); err != nil {
		t.Fatal(err)
	}
}
