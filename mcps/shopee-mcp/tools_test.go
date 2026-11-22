package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
	"github.com/gtihq/ai-mcps/internal/omap"
)

// ---- test helpers -----------------------------------------------------------

func fixture(t *testing.T, name string) any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("shopee", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(name, ".json") {
		v, err := decodeJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	return string(data)
}

func productFixture(t *testing.T, name, country string) *omap.M {
	t.Helper()
	html := fixture(t, name).(string)
	return asM(parseProduct(html, productBlob(html), country))
}

func val(t *testing.T, m *omap.M, key string) any {
	t.Helper()
	if m == nil {
		t.Fatalf("nil map looking for %q", key)
	}
	v, _ := m.Get(key)
	return v
}

func eqNum(got, want any) bool {
	f1, ok1 := toFloat(got)
	f2, ok2 := toFloat(want)
	return ok1 && ok2 && f1 == f2
}

func eq(t *testing.T, got, want any) {
	t.Helper()
	if gotSlice, ok := got.([]any); ok {
		wantSlice, ok := want.([]any)
		if !ok || len(gotSlice) != len(wantSlice) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range gotSlice {
			eq(t, gotSlice[i], wantSlice[i])
		}
		return
	}
	if got == nil || want == nil {
		if got != want {
			t.Fatalf("got %v, want %v", got, want)
		}
		return
	}
	if s, ok := want.(string); ok {
		if g, ok := got.(string); ok && g == s {
			return
		}
	}
	if b, ok := want.(bool); ok {
		if g, ok := got.(bool); ok && g == b {
			return
		}
	}
	if f1, ok1 := toFloat(got); ok1 {
		if f2, ok2 := toFloat(want); ok2 && f1 == f2 {
			return
		}
	}
	if reflect.DeepEqual(got, want) {
		return
	}
	t.Fatalf("got %v (%T), want %v (%T)", got, got, want, want)
}

func hasPrefix(t *testing.T, got any, prefix string) {
	t.Helper()
	s, ok := got.(string)
	if !ok || !strings.HasPrefix(s, prefix) {
		t.Fatalf("got %v, want prefix %q", got, prefix)
	}
}

func hasSubstr(t *testing.T, got any, substr string) {
	t.Helper()
	s, ok := got.(string)
	if !ok || !strings.Contains(s, substr) {
		t.Fatalf("got %v, want substring %q", got, substr)
	}
}

func listAt(t *testing.T, v any, i int) any {
	t.Helper()
	items := asSlice(v)
	if i >= len(items) {
		t.Fatalf("index %d out of range (%d items)", i, len(items))
	}
	return items[i]
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- refs -------------------------------------------------------------------

func TestResolveProduct(t *testing.T) {
	cases := []struct {
		value   string
		shop    int64
		item    int64
		country *string
		rejects bool
	}{
		{"15235546.23326616342", 15235546, 23326616342, nil, false},
		{"15235546/23326616342", 15235546, 23326616342, nil, false},
		{"https://shopee.sg/product/15235546/23326616342", 15235546, 23326616342, ptrString("sg"), false},
		{"https://shopee.co.th/product/146916334/29523950820?sp_atk=x", 146916334, 29523950820, ptrString("th"), false},
		{"https://shopee.com.br/Novo-iPhone-17-i.486926153.22192886389", 486926153, 22192886389, ptrString("br"), false},
		{"shopee.sg/x-i.1.2", 0, 0, nil, true},
		{"https://mall.shopee.sg/universal-link/product/15235546/23326616342", 15235546, 23326616342, ptrString("sg"), false},
	}
	for _, c := range cases {
		ref, err := resolveProduct(c.value)
		if c.rejects {
			if err == nil {
				t.Errorf("resolveProduct(%q) should reject", c.value)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolveProduct(%q): %v", c.value, err)
			continue
		}
		eq(t, ref.ShopID, c.shop)
		eq(t, ref.ItemID, c.item)
		if c.country == nil {
			if ref.Country != nil {
				t.Errorf("%q: country = %q, want nil", c.value, *ref.Country)
			}
		} else {
			if ref.Country == nil || *ref.Country != *c.country {
				t.Errorf("%q: country = %v, want %q", c.value, ref.Country, *c.country)
			}
		}
	}
	for _, value := range []string{"", "abc.def", "https://example.com/product/1/2",
		"https://shopee.cl/product/1/2", "https://shopee.sg/search?keyword=x"} {
		if _, err := resolveProduct(value); err == nil {
			t.Errorf("resolveProduct(%q) should reject", value)
		}
	}
}

func TestResolveShop(t *testing.T) {
	cases := []struct {
		value   string
		id      *int64
		user    *string
		country *string
	}{
		{"15235546", ptrInt64(15235546), nil, nil},
		{"lorealparissg", nil, ptrString("lorealparissg"), nil},
		{"LorealParisSG", nil, ptrString("lorealparissg"), nil},
		{"https://shopee.sg/shop/15235546", ptrInt64(15235546), nil, ptrString("sg")},
		{"https://shopee.sg/shop/15235546?page=2", ptrInt64(15235546), nil, ptrString("sg")},
		{"https://shopee.com.my/lorealparissg", nil, ptrString("lorealparissg"), ptrString("my")},
	}
	for _, c := range cases {
		ref, err := resolveShop(c.value)
		if err != nil {
			t.Errorf("resolveShop(%q): %v", c.value, err)
			continue
		}
		if c.id == nil {
			if ref.ShopID != nil {
				t.Errorf("%q: id = %d, want nil", c.value, *ref.ShopID)
			}
		} else if ref.ShopID == nil || *ref.ShopID != *c.id {
			t.Errorf("%q: id = %v, want %d", c.value, ref.ShopID, *c.id)
		}
		if c.user == nil {
			if ref.Username != nil {
				t.Errorf("%q: username = %q, want nil", c.value, *ref.Username)
			}
		} else if ref.Username == nil || *ref.Username != *c.user {
			t.Errorf("%q: username = %v, want %q", c.value, ref.Username, *c.user)
		}
		if c.country == nil {
			if ref.Country != nil {
				t.Errorf("%q: country = %q, want nil", c.value, *ref.Country)
			}
		} else if ref.Country == nil || *ref.Country != *c.country {
			t.Errorf("%q: country = %v, want %q", c.value, ref.Country, *c.country)
		}
	}
	for _, value := range []string{"", "a b", "https://shopee.sg/search?keyword=x", "https://shopee.sg/cart/",
		"https://example.com/shop/1", "https://shopee.sg/product/1/2"} {
		if _, err := resolveShop(value); err == nil {
			t.Errorf("resolveShop(%q) should reject", value)
		}
	}
}

func TestLinks(t *testing.T) {
	eq(t, productLink("th", 1, 2), "https://shopee.co.th/product/1/2")
	eq(t, shopLink("br", 7), "https://shopee.com.br/shop/7")
	eq(t, categoryLink("sg", "Women's Apparel", 11012819), "https://shopee.sg/Women-s-Apparel-cat.11012819")
	if !strings.HasSuffix(categoryLink("th", "อุปกรณ์ DIY", 11045185), "-cat.11045185") {
		t.Fatal("thai category link")
	}
	eq(t, imageLink("id", "abc"), "https://down-id.img.susercontent.com/file/abc")
	eq(t, imageLink("id", "https://x/y.png"), "https://x/y.png")
	eq(t, imageLink("sg", nil), nil)
	eq(t, site("br"), "https://shopee.com.br")
	eq(t, searchLink("sg", "iphone 16 pro max case"), "https://shopee.sg/search?keyword=iphone%2016%20pro%20max%20case")
}

// ---- value helpers ----------------------------------------------------------

func TestMoneyAndTs(t *testing.T) {
	eq(t, money(1205000), 12.05)
	for _, v := range []any{0, -1, nil, "x", true} {
		eq(t, money(v), nil)
	}
	eq(t, ts(1792821600), "2026-10-24T06:00:00Z")
	eq(t, ts(0), nil)
	eq(t, ts(nil), nil)
	eq(t, text("  "), nil)
	eq(t, text("-"), nil)
	eq(t, text("&amp;x"), "&x")
	eq(t, number("12"), 12)
	eq(t, number("1.5"), 1.5)
	eq(t, number(true), nil)
	eq(t, flagValue(1), true)
	eq(t, flagValue(0), false)
	eq(t, flagValue("x"), nil)
	block := asM(ratingBlock(omap.From("rating_star", 4.8912, "rating_count", []any{10, 1, 0, 1, 2, 6})))
	eq(t, val(t, block, "average"), 4.89)
	eq(t, val(t, block, "count"), 10)
	byStar := asM(val(t, block, "counts_by_star"))
	eq(t, val(t, byStar, "1"), 1)
	eq(t, val(t, byStar, "2"), 0)
	eq(t, val(t, byStar, "5"), 6)
	eq(t, ratingBlock(omap.From("rating_star", nil, "rating_count", []any{})), nil)
}

// ---- product ----------------------------------------------------------------

func TestProductIdentityAndLinks(t *testing.T) {
	p := productFixture(t, "product_loreal_sg_trimmed.html", "sg")
	eq(t, val(t, p, "id"), 23326616342)
	eq(t, val(t, p, "shop_id"), 15235546)
	hasPrefix(t, val(t, p, "name"), "L'Oreal Paris Elseve Fall Resist")
	eq(t, val(t, p, "link"), "https://shopee.sg/product/15235546/23326616342")
	hasSubstr(t, val(t, p, "canonical_link"), "-i.15235546.23326616342")
	eq(t, val(t, p, "status"), "normal")
	eq(t, val(t, p, "condition"), "new")
	brand := asM(val(t, p, "brand"))
	eq(t, val(t, brand, "id"), 2387597)
	eq(t, val(t, brand, "name"), "L'Oreal Paris")
	eq(t, val(t, p, "currency"), "SGD")
	eq(t, val(t, p, "listed_at"), "2023-02-22T17:44:44Z")
	shop := asM(val(t, p, "shop"))
	eq(t, val(t, shop, "id"), 15235546)
	eq(t, val(t, shop, "link"), "https://shopee.sg/shop/15235546")
}

func TestProductPaywalledFieldsAreNull(t *testing.T) {
	for _, tc := range []struct{ name, country string }{
		{"product_loreal_sg_trimmed.html", "sg"},
		{"product_th_trimmed.html", "th"},
		{"product_br_trimmed.html", "br"},
	} {
		p := productFixture(t, tc.name, tc.country)
		for _, key := range []string{"price", "price_min", "price_max", "price_before_discount",
			"stock", "sold_count", "rating", "reviews_count", "likes_count"} {
			if v := val(t, p, key); v != nil {
				t.Errorf("%s: %s = %v, want nil", tc.name, key, v)
			}
		}
		for _, raw := range asSlice(val(t, p, "models")) {
			m := asM(raw)
			eq(t, val(t, m, "stock"), nil)
			eq(t, val(t, m, "price"), nil)
		}
	}
}

func TestProductContent(t *testing.T) {
	p := productFixture(t, "product_loreal_sg_trimmed.html", "sg")
	hasPrefix(t, val(t, p, "description"), "\u2022 What is it ?")
	hasSubstr(t, val(t, p, "description"), "Up to 97% Less Hair Fall")
	imgs := asSlice(val(t, p, "images"))
	if len(imgs) != 8 {
		t.Fatalf("8 images, got %d", len(imgs))
	}
	for _, img := range imgs {
		hasPrefix(t, img, "https://down-sg.img.susercontent.com/file/")
	}
	if len(asSlice(val(t, p, "variations"))) != 0 {
		t.Fatal("one-SKU listing should drop the placeholder tier")
	}
	models := asSlice(val(t, p, "models"))
	if len(models) != 1 || val(t, asM(models[0]), "is_available") != true {
		t.Fatalf("models = %v", models)
	}
	found := false
	for _, raw := range asSlice(val(t, p, "attributes")) {
		a := asM(raw)
		if eqNum(val(t, a, "id"), 100016) && val(t, a, "name") == "Pack Type" && val(t, a, "value") == "Value Size" {
			found = true
		}
	}
	if !found {
		t.Fatal("attribute 100016 not found")
	}
	cats := asSlice(val(t, p, "categories"))
	want := []string{"Beauty & Personal Care", "Women's Hair Care", "Shampoo"}
	for i, name := range want {
		eq(t, val(t, asM(cats[i]), "name"), name)
	}
	eq(t, val(t, asM(cats[0]), "link"), "https://shopee.sg/Beauty-Personal-Care-cat.11012301")
	services := asSlice(val(t, p, "services"))
	eq(t, val(t, asM(services[0]), "title"), "100% Authentic")
	eq(t, val(t, asM(services[1]), "title"), "15-Day Free Returns")
	shipping := asM(val(t, p, "shipping"))
	eq(t, val(t, shipping, "is_free_shipping"), false)
	eq(t, val(t, shipping, "estimated_delivery_days"), 1)
	eq(t, val(t, shipping, "ships_from"), "SG")
	limits := asM(val(t, p, "purchase_limits"))
	eq(t, val(t, limits, "min_quantity"), 1)
	eq(t, val(t, limits, "max_quantity"), nil)
	eq(t, val(t, p, "is_official_shop"), true)
	eq(t, val(t, p, "is_adult"), false)
	eq(t, val(t, p, "has_video"), false)
}

func TestProductVariationsAndModels(t *testing.T) {
	thai := productFixture(t, "product_th_trimmed.html", "th")
	eq(t, val(t, thai, "currency"), "THB")
	eq(t, val(t, thai, "brand"), nil)
	variations := asSlice(val(t, thai, "variations"))
	if len(variations) != 1 {
		t.Fatalf("1 variation, got %d", len(variations))
	}
	v := asM(variations[0])
	eq(t, val(t, v, "name"), "\u0e2a\u0e35")
	opts := asSlice(val(t, v, "options"))
	if len(opts) != 25 {
		t.Fatalf("25 options, got %d", len(opts))
	}
	hasPrefix(t, val(t, asM(opts[0]), "image"), "https://down-th.img.susercontent.com/")
	models := asSlice(val(t, thai, "models"))
	if len(models) != 25 {
		t.Fatalf("25 models, got %d", len(models))
	}
	eq(t, val(t, asM(models[0]), "option_indexes"), []any{11})
	attrs := asSlice(val(t, thai, "attributes"))
	if len(attrs) != 1 {
		t.Fatalf("1 attribute, got %d", len(attrs))
	}
	a := asM(attrs[0])
	eq(t, val(t, a, "id"), nil)
	eq(t, val(t, a, "name"), "ส่งจาก")
	eq(t, val(t, a, "value"), "จังหวัดสุโขทัย")
	hasPrefix(t, val(t, asM(listAt(t, val(t, thai, "categories"), 0)), "link"), "https://shopee.co.th/%E0%B9%80")

	brazil := productFixture(t, "product_br_trimmed.html", "br")
	variations = asSlice(val(t, brazil, "variations"))
	if len(variations) != 2 {
		t.Fatalf("2 variations, got %d", len(variations))
	}
	eq(t, val(t, asM(variations[0]), "name"), "Cor")
	eq(t, val(t, asM(variations[1]), "name"), "Modelo / Memória")
	models = asSlice(val(t, brazil, "models"))
	if len(models) != 21 {
		t.Fatalf("21 models, got %d", len(models))
	}
	eq(t, val(t, asM(models[0]), "option_indexes"), []any{1, 0})
	eq(t, val(t, asM(models[0]), "name"), "Azul,17 PRO MAX - 1TB")
}

func TestProductUnknownIdIsNone(t *testing.T) {
	html := fixture(t, "product_unknown_sg_trimmed.html").(string)
	if productBlob(html) == nil {
		t.Fatal("blob expected")
	}
	eq(t, parseProduct(html, productBlob(html), "sg"), nil)
	eq(t, pageTitle(html), "Shopee Singapore | Cheaper, Faster On Shopee")
	eq(t, parseProduct("", nil, "sg"), nil)
	eq(t, parseProduct("<html></html>", omap.From("initialState", omap.New()), "sg"), nil)
}

// ---- tool wire schemas ------------------------------------------------------

func expectedTool(t *testing.T, name, desc, props, required string) string {
	t.Helper()
	req := ""
	if required != "" {
		req = `,"required":` + required
	}
	return `{"name":"` + name + `","description":` + jsonString(t, desc) +
		`,"inputSchema":{"properties":` + props + req +
		`,"title":"` + name + `Arguments","type":"object"}}`
}

func TestToolSchemas(t *testing.T) {
	countryProp := `"country":{"default":"br","title":"Country","type":"string"}`
	nullInt := func(title string) string {
		return `"` + title + `":{"anyOf":[{"type":"integer"},{"type":"null"}],"default":null,"title":"` + strings.ToUpper(title[:1]) + title[1:] + `"}`
	}
	pageProp := `"page":{"default":1,"title":"Page","type":"integer"}`
	shopProp := `"shop":{"title":"Shop","type":"string"}`

	toolsJSON := []string{
		expectedTool(t, "search_autocomplete", descSearchAutocomplete,
			`{"query":{"title":"Query","type":"string"},`+countryProp+`}`, `["query"]`),
		expectedTool(t, "search_trending", descSearchTrending,
			`{`+countryProp+`}`, ""),
		expectedTool(t, "search_products", descSearchProducts,
			`{"keyword":{"title":"Keyword","type":"string"},`+countryProp+
				`,"sort":{"default":"relevancy","title":"Sort","type":"string"},`+pageProp+
				`,"limit":{"default":20,"title":"Limit","type":"integer"},`+
				`"min_price":{"anyOf":[{"type":"number"},{"type":"null"}],"default":null,"title":"Min Price"},`+
				`"max_price":{"anyOf":[{"type":"number"},{"type":"null"}],"default":null,"title":"Max Price"}}`, `["keyword"]`),
		expectedTool(t, "products_trending", descProductsTrending,
			`{`+countryProp+`}`, ""),
		expectedTool(t, "product_details", descProductDetails,
			`{"product":{"title":"Product","type":"string"},`+countryProp+`}`, `["product"]`),
		expectedTool(t, "deals_flash_sale", descDealsFlashSale,
			`{`+countryProp+`,`+nullInt("session")+`,`+nullInt("category")+`,`+pageProp+`}`, ""),
		expectedTool(t, "shop_details", descShopDetails,
			`{`+shopProp+`,`+countryProp+`}`, `["shop"]`),
		expectedTool(t, "shop_reviews", descShopReviews,
			`{`+shopProp+`,"rating":{"default":5,"title":"Rating","type":"integer"},`+pageProp+`,`+countryProp+`}`,
			`["shop"]`),
		expectedTool(t, "shop_categories", descShopCategories,
			`{`+shopProp+`,`+pageProp+`,`+countryProp+`}`, `["shop"]`),
		expectedTool(t, "shops_official", descShopsOfficial,
			`{`+pageProp+`,`+countryProp+`}`, ""),
		expectedTool(t, "categories_list", descCategoriesList,
			`{`+countryProp+`}`, ""),
		expectedTool(t, "list_orders", descListOrders,
			`{`+countryProp+`}`, ""),
	}

	s := mcpserver.New("shopee", tools()...)
	line := s.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	want := `{"jsonrpc":"2.0","id":1,"result":{"tools":[` + strings.Join(toolsJSON, ",") + `]}}` + "\n"
	if line != want {
		t.Fatalf("tools/list mismatch\n got: %s\nwant: %s", line, want)
	}
}

func TestCountryValidation(t *testing.T) {
	cc, err := countryCode("TH")
	if err != nil || cc != "th" {
		t.Fatalf("countryCode(TH) = %q, %v", cc, err)
	}
	cc, err = countryCode("")
	if err != nil || cc != DEFAULT_COUNTRY {
		t.Fatalf("countryCode() = %q, %v", cc, err)
	}
	_, err = countryCode("us")
	want := "country must be one of sg, my, th, vn, ph, id, tw, br, mx (got 'us')"
	if err == nil || err.Error() != want {
		t.Fatalf("countryCode(us) error = %v, want %q", err, want)
	}

	s := mcpserver.New("shopee", tools()...)
	line := s.Handle(context.Background(), []byte(
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"categories_list","arguments":{"country":"us"}}}`))
	if !strings.Contains(line, "country must be one of sg, my, th, vn, ph, id, tw, br, mx (got 'us')") {
		t.Fatalf("tools/call did not return the country error: %s", line)
	}
}

// ---- _parse_search_items ----------------------------------------------------

func TestParseSearchItems(t *testing.T) {
	const payloadJSON = `{"data":{"items":[
		{"item_basic":{"itemid":100,"shopid":1,"name":"A &amp; B","image":"h1","images":["i1","i2"],` +
		`"price":1234567,"price_min":100000,"price_max":200000,"price_before_discount":2000000,` +
		`"show_discount":10,"historical_sold":5,"stock":3,` +
		`"item_rating":{"rating_star":4.5,"rating_count":[10,1,0,0,4,5]},` +
		`"liked_count":7,"shop_location":"SG","shopee_verified":1,"is_official_shop":1,` +
		`"is_adult":0,"ctime":1792821600},"adsid":"ad"},
		{"item_data":{"shopid":2,"itemid":200,` +
		`"item_card_display_price":{"price":500000,"strikethrough_price":1000000,"discount":50},` +
		`"item_card_display_sold_count":"1.2k","is_sold_out":true,"liked_count":1,` +
		`"item_rating":{"rating_star":4,"rating_count":[3,0,0,0,0,3]},` +
		`"shop_data":{"is_shopee_verified":1,"is_official_shop":1},"ctime":1792821600},` +
		`"item_card_displayed_asset":{"name":"B","image":"h2","images":["j1"],"sold_count":"2mil"}},
		{"item_basic":{"name":"no id"}}
	],"total_count":42}}`
	payload, err := decodeJSON([]byte(payloadJSON))
	if err != nil {
		t.Fatal(err)
	}
	rows, total := parseSearchItems(payload, "br")
	eq(t, total, 42)
	if len(rows) != 2 {
		t.Fatalf("2 rows, got %d", len(rows))
	}
	row1 := asM(rows[0])
	wantKeys := "id,shop_id,name,link,image,images,currency,price,price_min,price_max," +
		"price_before_discount,discount_percent,sold_count,stock,rating,likes_count,shop_location," +
		"is_shopee_verified,is_official_shop,is_adult,is_ad,listed_at"
	if got := strings.Join(row1.Keys(), ","); got != wantKeys {
		t.Fatalf("key order = %q, want %q", got, wantKeys)
	}
	eq(t, val(t, row1, "name"), "A & B")
	eq(t, val(t, row1, "price"), 12.35)
	eq(t, val(t, row1, "price_before_discount"), 20.0)
	eq(t, val(t, row1, "discount_percent"), 10)
	eq(t, val(t, row1, "sold_count"), 5)
	eq(t, val(t, row1, "stock"), 3)
	eq(t, val(t, row1, "is_ad"), true)
	eq(t, val(t, row1, "is_shopee_verified"), true)
	eq(t, val(t, row1, "listed_at"), "2026-10-24T06:00:00Z")
	eq(t, val(t, asM(val(t, row1, "rating")), "average"), 4.5)

	row2 := asM(rows[1])
	eq(t, val(t, row2, "name"), "B")
	eq(t, val(t, row2, "price"), 5.0)
	eq(t, val(t, row2, "price_before_discount"), 10.0)
	eq(t, val(t, row2, "sold_count"), 12000)
	eq(t, val(t, row2, "stock"), 0)
	eq(t, val(t, row2, "is_ad"), false)
}

// ---- list_orders ------------------------------------------------------------

func TestListOrdersMissingProfile(t *testing.T) {
	old := browserProfileOverride
	browserProfileOverride = filepath.Join(t.TempDir(), "missing-profile")
	t.Cleanup(func() { browserProfileOverride = old })
	got, err := handleListOrders(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, val(t, asM(got), "error"), "not logged in: run `python login.py` first")
}
