package main

import (
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/internal/omap"
)

// ---- shops ------------------------------------------------------------------

func TestParseShop(t *testing.T) {
	shop := parseShop(fixture(t, "shop_detail_loreal.json"), "sg")
	eq(t, val(t, shop, "id"), 15235546)
	eq(t, val(t, shop, "name"), "L'Oreal Paris Official Store")
	eq(t, val(t, shop, "username"), "lorealparissg")
	eq(t, val(t, shop, "link"), "https://shopee.sg/shop/15235546")
	eq(t, val(t, shop, "username_link"), "https://shopee.sg/lorealparissg")
	hasPrefix(t, val(t, shop, "description"), "Buy shampoos")
	eq(t, val(t, shop, "country"), "SG")
	eq(t, val(t, shop, "location"), nil)
	eq(t, val(t, shop, "is_official_shop"), true)
	eq(t, val(t, shop, "is_shopee_verified"), false)
	eq(t, val(t, shop, "is_on_vacation"), false)
	eq(t, val(t, shop, "is_chat_enabled"), true)
	eq(t, val(t, shop, "items_count"), 202)
	eq(t, val(t, shop, "followers_count"), 71515)
	eq(t, val(t, shop, "following_count"), 3)
	rating := asM(val(t, shop, "rating"))
	eq(t, val(t, rating, "average"), 4.89)
	eq(t, val(t, rating, "count"), 80734)
	eq(t, val(t, rating, "good"), 78757)
	eq(t, val(t, rating, "normal"), 1506)
	eq(t, val(t, rating, "bad"), 471)
	eq(t, val(t, shop, "response_rate_percent"), 99)
	eq(t, val(t, shop, "response_time_seconds"), 10125)
	eq(t, val(t, shop, "joined_at"), "2017-01-16T07:07:56Z")
	eq(t, val(t, shop, "last_active_at"), "2026-10-24T09:10:46Z")
	hasSubstr(t, val(t, shop, "avatar"), "/f9794753aeed60aadfa103a938795d7e")
	eq(t, val(t, shop, "vouchers"), []any{})
}

func TestParseShopOtherMarkets(t *testing.T) {
	th := parseShop(fixture(t, "shop_detail_th.json"), "th")
	hasPrefix(t, val(t, th, "link"), "https://shopee.co.th/shop/")
	eq(t, val(t, th, "country"), "TH")
	if val(t, th, "location") == nil {
		t.Fatal("thai location should be present")
	}
	br := parseShop(fixture(t, "shop_detail_br.json"), "br")
	eq(t, val(t, br, "country"), "BR")
	if val(t, br, "name") == nil {
		t.Fatal("br shop name")
	}
	byName := parseShop(fixture(t, "shop_detail_by_username.json"), "sg")
	eq(t, val(t, byName, "id"), 1618725281)
	eq(t, val(t, byName, "username"), "lin0012kf.sg")
}

func TestParseShopNotFoundPayloads(t *testing.T) {
	for _, name := range []string{"shop_detail_unknown.json", "shop_detail_bad_username.json", "shop_base_unknown.json"} {
		eq(t, val(t, parseShop(fixture(t, name), "sg"), "id"), nil)
	}
	eq(t, val(t, parseShop(omap.New(), "sg"), "id"), nil)
}

func TestParseShopFromAppBase(t *testing.T) {
	base := parseShop(fixture(t, "shop_base_loreal.json"), "sg")
	detail := parseShop(fixture(t, "shop_detail_loreal.json"), "sg")
	if strings.Join(base.Keys(), ",") != strings.Join(detail.Keys(), ",") {
		t.Fatalf("key order differs:\n%v\n%v", base.Keys(), detail.Keys())
	}
	eq(t, val(t, base, "id"), 15235546)
	eq(t, val(t, base, "name"), "L'Oreal Paris Official Store")
	eq(t, val(t, base, "username"), "lorealparissg")
	eq(t, val(t, base, "link"), "https://shopee.sg/shop/15235546")
	eq(t, val(t, base, "is_official_shop"), true)
	eq(t, val(t, base, "is_chat_enabled"), true)
	rating := asM(val(t, base, "rating"))
	eq(t, val(t, rating, "average"), 4.89)
	eq(t, val(t, rating, "count"), 80932)
	eq(t, val(t, rating, "good"), 78955)
	eq(t, val(t, rating, "normal"), 1506)
	eq(t, val(t, rating, "bad"), 471)
	eq(t, val(t, base, "items_count"), 202)
	eq(t, val(t, base, "followers_count"), 71543)
	eq(t, val(t, base, "following_count"), 3)
	eq(t, val(t, base, "response_rate_percent"), 100)
	eq(t, val(t, base, "response_time_seconds"), 10466)
	eq(t, val(t, base, "has_live_stream"), true)
	eq(t, val(t, base, "has_membership"), true)
	eq(t, val(t, base, "latest_item_listed_at"), "2026-10-21T07:52:51Z")
	eq(t, val(t, base, "joined_at"), "2017-01-16T07:07:56Z")
	hasPrefix(t, val(t, base, "description"), "Buy shampoos")
	for _, key := range []string{"location", "preparation_time_seconds", "cancellation_rate_percent",
		"has_flash_sale", "has_brand_sale", "updated_at"} {
		eq(t, val(t, base, key), nil)
	}
	th := parseShop(fixture(t, "shop_base_th.json"), "th")
	eq(t, val(t, th, "username"), "rayban.th")
	eq(t, val(t, th, "link"), "https://shopee.co.th/shop/55953258")
	eq(t, val(t, th, "is_official_shop"), true)
	eq(t, val(t, th, "country"), "TH")
}

func TestParseShopCategories(t *testing.T) {
	rows, total, more := parseShopCategories(fixture(t, "shop_categories_loreal.json"), "sg", 15235546)
	eq(t, total, 25)
	eq(t, more, false)
	if len(rows) != 25 {
		t.Fatalf("25 rows, got %d", len(rows))
	}
	first := asM(rows[0])
	wantKeys := "id,name,link,image,items_count,is_auto_generated,is_adult"
	if strings.Join(first.Keys(), ",") != wantKeys {
		t.Fatalf("keys = %q", strings.Join(first.Keys(), ","))
	}
	eq(t, val(t, first, "id"), 263918827)
	eq(t, val(t, first, "name"), "\U0001F52C Glycolic Gloss Range")
	eq(t, val(t, first, "link"), "https://shopee.sg/shop/15235546?shopCollection=263918827")
	eq(t, val(t, first, "image"), "https://down-sg.img.susercontent.com/file/sg-11134207-8257u-munqrainq60z59")
	eq(t, val(t, first, "items_count"), 4)
	eq(t, val(t, first, "is_auto_generated"), false)
	eq(t, val(t, first, "is_adult"), false)
	r, tot, m := parseShopCategories(omap.New(), "sg", 1)
	if len(r) != 0 || tot != nil || m != nil {
		t.Fatalf("empty payload = %v %v %v", r, tot, m)
	}
}

func TestParseShopReviews(t *testing.T) {
	rows := parseShopReviews(fixture(t, "shop_ratings_loreal_1.json"), "sg")
	if len(rows) != 20 {
		t.Fatalf("20 rows, got %d", len(rows))
	}
	for _, raw := range rows {
		eq(t, val(t, asM(raw), "rating"), 1)
	}
	first := asM(rows[0])
	eq(t, val(t, first, "id"), 114085284892)
	eq(t, val(t, first, "comment"), nil)
	detailed := asM(val(t, first, "detailed_rating"))
	eq(t, val(t, detailed, "product_quality"), 1)
	eq(t, val(t, detailed, "seller_service"), 5)
	eq(t, val(t, detailed, "delivery_service"), 5)
	author := asM(val(t, first, "author"))
	eq(t, val(t, author, "username"), "iwantmiso")
	eq(t, val(t, author, "avatar"), nil)
	eq(t, val(t, author, "is_anonymous"), false)
	product := asM(val(t, first, "product"))
	eq(t, val(t, product, "id"), 42822231542)
	eq(t, val(t, product, "variation"), "375")
	eq(t, val(t, product, "link"), "https://shopee.sg/product/15235546/42822231542")
	hasPrefix(t, val(t, first, "created_at"), "2026-")
	commented := false
	for _, raw := range rows {
		if val(t, asM(raw), "comment") != nil {
			commented = true
		}
	}
	if !commented {
		t.Fatal("fixture carries commented reviews")
	}
	five := parseShopReviews(fixture(t, "shop_ratings_loreal_5.json"), "sg")
	if len(five) != 20 {
		t.Fatalf("20 rows, got %d", len(five))
	}
	for _, raw := range five {
		eq(t, val(t, asM(raw), "rating"), 5)
	}
	small := parseShopReviews(fixture(t, "shop_ratings_small_shop.json"), "sg")
	if len(small) == 0 {
		t.Fatal("small shop reviews")
	}
	eq(t, val(t, asM(val(t, asM(small[0]), "product")), "shop_id"), 1618725281)
	if got := parseShopReviews(omap.From("data", omap.From("items", nil)), "sg"); len(got) != 0 {
		t.Fatalf("nil items = %v", got)
	}
}

func TestParseVouchers(t *testing.T) {
	rows := parseVouchers(fixture(t, "shop_vouchers_loreal.json"))
	if len(rows) != 9 {
		t.Fatalf("9 vouchers, got %d", len(rows))
	}
	first := asM(rows[0])
	eq(t, val(t, first, "id"), 1537379311820800)
	eq(t, val(t, first, "code"), "SVC-1537379311820800")
	eq(t, val(t, first, "reward_type"), "percentage")
	eq(t, val(t, first, "discount_percent"), 18)
	eq(t, val(t, first, "discount_cap"), 3.0)
	eq(t, val(t, first, "min_spend"), 10.0)
	eq(t, val(t, first, "discount_amount"), nil)
	eq(t, val(t, first, "starts_at"), "2026-10-23T16:00:00Z")
	eq(t, val(t, first, "ends_at"), "2026-10-27T15:59:59Z")
	eq(t, val(t, first, "title"), nil)
	eq(t, val(t, first, "percentage_used"), 23)
	if got := parseVouchers(fixture(t, "shop_vouchers_small.json")); len(got) != 0 {
		t.Fatalf("small vouchers = %v", got)
	}
	if got := parseVouchers(omap.New()); len(got) != 0 {
		t.Fatalf("empty vouchers = %v", got)
	}
}

func TestParseOfficialShops(t *testing.T) {
	rows, total := parseOfficialShops(fixture(t, "official_shops_sg.json"), "sg")
	eq(t, total, 540)
	if len(rows) != 20 {
		t.Fatalf("20 shops, got %d", len(rows))
	}
	first := asM(rows[0])
	wantKeys := "id,name,username,link,brand_name,logo,joined_at"
	if strings.Join(first.Keys(), ",") != wantKeys {
		t.Fatalf("keys = %q", strings.Join(first.Keys(), ","))
	}
	eq(t, val(t, first, "id"), 43714378)
	eq(t, val(t, first, "name"), "TAKA Jewellery Official Store")
	eq(t, val(t, first, "username"), "takajewellery.sg")
	eq(t, val(t, first, "link"), "https://shopee.sg/shop/43714378")
	eq(t, val(t, first, "brand_name"), "Taka Jewellery")
	eq(t, val(t, first, "logo"), "https://down-sg.img.susercontent.com/file/ef36c4ddf56dfe663e7df0f248c6d1be")
	eq(t, val(t, first, "joined_at"), "2021-05-14T08:42:01Z")
}

// ---- catalog ----------------------------------------------------------------

func TestParseCategoryTree(t *testing.T) {
	rows := parseCategoryTree(fixture(t, "category_tree_sg.json"), "sg")
	if len(rows) != 27 {
		t.Fatalf("27 categories, got %d", len(rows))
	}
	women := asM(rows[0])
	eq(t, val(t, women, "id"), 11012819)
	eq(t, val(t, women, "name"), "Women's Apparel")
	eq(t, val(t, women, "level"), 1)
	eq(t, val(t, women, "parent_id"), nil)
	eq(t, val(t, women, "link"), "https://shopee.sg/Women-s-Apparel-cat.11012819")
	children := asSlice(val(t, women, "children"))
	if len(children) != 17 {
		t.Fatalf("17 children, got %d", len(children))
	}
	child := asM(children[0])
	eq(t, val(t, child, "name"), "Tops")
	eq(t, val(t, child, "parent_id"), 11012819)
	if len(asSlice(val(t, child, "children"))) != 0 {
		t.Fatal("leaf children must be empty")
	}
	if got := parseCategoryTree(omap.New(), "sg"); len(got) != 0 {
		t.Fatalf("empty tree = %v", got)
	}
}

func TestParseSearchHint(t *testing.T) {
	keywords, shops := parseSearchHint(fixture(t, "search_hint_iphone.json"), "sg")
	if len(keywords) != 12 || len(shops) != 0 {
		t.Fatalf("keywords=%d shops=%d", len(keywords), len(shops))
	}
	first := asM(keywords[0])
	eq(t, val(t, first, "keyword"), "iphone 16 pro max case")
	eq(t, val(t, first, "link"), "https://shopee.sg/search?keyword=iphone%2016%20pro%20max%20case")
	eq(t, val(t, first, "category"), nil)
	k, s := parseSearchHint(fixture(t, "search_hint_nonsense.json"), "sg")
	if len(k) != 0 || len(s) != 0 {
		t.Fatalf("nonsense = %v %v", k, s)
	}
	k, s = parseSearchHint(omap.New(), "sg")
	if len(k) != 0 || len(s) != 0 {
		t.Fatalf("empty = %v %v", k, s)
	}
}

func TestParseSearchSuggestion(t *testing.T) {
	rows := parseSearchSuggestion(fixture(t, "search_suggestion.json"), "sg")
	if len(rows) != 35 {
		t.Fatalf("35 rows, got %d", len(rows))
	}
	first := asM(rows[0])
	eq(t, val(t, first, "keyword"), "iPhone 18 Pro")
	eq(t, val(t, first, "item_ids"), []any{42218283362})
	eq(t, val(t, first, "shop_ids"), []any{442800909})
	hasPrefix(t, val(t, first, "image"), "https://down-sg.")
	if got := parseSearchSuggestion(omap.New(), "sg"); len(got) != 0 {
		t.Fatalf("empty = %v", got)
	}
}

func TestParseFlashSale(t *testing.T) {
	sessions := parseFlashSessions(fixture(t, "flash_sessions_sg.json"), "sg")
	if len(sessions) != 4 {
		t.Fatalf("4 sessions, got %d", len(sessions))
	}
	first := asM(sessions[0])
	eq(t, val(t, first, "id"), 513667598795255)
	eq(t, val(t, first, "is_ongoing"), true)
	eq(t, val(t, first, "starts_at"), "2026-10-24T06:00:00Z")
	eq(t, val(t, first, "name"), "24 Oct 14:00 - 19:59")
	cat := asM(listAt(t, val(t, first, "categories"), 1))
	eq(t, val(t, cat, "id"), 17)
	eq(t, val(t, cat, "name"), "$1.99 Shop")
	eq(t, val(t, cat, "image"), "https://down-sg.img.susercontent.com/file/sg-11134004-82589-mu1j7o7nqgau8d")

	rows, served := parseFlashItems(fixture(t, "flash_items_sg.json"), "sg")
	sm := asM(served)
	eq(t, val(t, sm, "id"), 513667598795255)
	eq(t, val(t, sm, "starts_at"), "2026-10-24T06:00:00Z")
	eq(t, val(t, sm, "ends_at"), "2026-10-24T12:00:00Z")
	if len(rows) != 16 {
		t.Fatalf("16 items, got %d", len(rows))
	}
	chair := asM(rows[0])
	eq(t, val(t, chair, "id"), 16931040911)
	eq(t, val(t, chair, "shop_id"), 728757183)
	eq(t, val(t, chair, "price"), 123.77)
	eq(t, val(t, chair, "price_before_discount"), 239.0)
	eq(t, val(t, chair, "discount_percent"), 48)
	eq(t, val(t, chair, "currency"), "SGD")
	eq(t, val(t, chair, "stock"), 123)
	eq(t, val(t, chair, "flash_sale_stock"), 125)
	eq(t, val(t, chair, "sold_count"), 12174)
	rating := asM(val(t, chair, "rating"))
	eq(t, val(t, rating, "average"), 4.82)
	eq(t, val(t, rating, "count"), 4398)
	eq(t, val(t, chair, "link"), "https://shopee.sg/product/728757183/16931040911")
	r, s := parseFlashItems(omap.New(), "sg")
	if len(r) != 0 || s != nil {
		t.Fatalf("empty = %v %v", r, s)
	}
}

func TestParseFeed(t *testing.T) {
	rows, total := parseFeed(fixture(t, "daily_discover_sg.json"), "sg")
	eq(t, total, 500)
	if len(rows) != 30 {
		t.Fatalf("30 rows, got %d", len(rows))
	}
	first := asM(rows[0])
	eq(t, val(t, first, "id"), 44482244164)
	eq(t, val(t, first, "shop_id"), 1618725281)
	hasPrefix(t, val(t, first, "name"), "Universal Non-Drilling Wall Mount")
	eq(t, val(t, first, "price"), 1.64)
	eq(t, val(t, first, "price_before_discount"), nil)
	eq(t, val(t, first, "discount_percent"), nil)
	rating := asM(val(t, first, "rating"))
	eq(t, val(t, rating, "average"), 4.76)
	eq(t, val(t, rating, "count"), 196)
	eq(t, val(t, first, "likes_count"), 307)
	shop := asM(val(t, first, "shop"))
	eq(t, val(t, shop, "name"), "lin0012kf.sg")
	eq(t, val(t, shop, "location"), "Mainland China")
	if len(asSlice(val(t, first, "images"))) != 9 {
		t.Fatal("9 images")
	}
	eq(t, val(t, first, "listed_at"), "2026-07-03T02:45:37Z")
	discounted := false
	for _, raw := range rows {
		if v := val(t, asM(raw), "discount_percent"); isTruthy(v) {
			discounted = true
			if !numEq(val(t, asM(raw), "price_before_discount"), val(t, asM(raw), "price")) {
				if toFloatOrZero(val(t, asM(raw), "price_before_discount")) <= toFloatOrZero(val(t, asM(raw), "price")) {
					t.Fatal("discounted price_before_discount must exceed price")
				}
			}
			break
		}
	}
	if !discounted {
		t.Fatal("fixture has discounted items")
	}
	r, tot := parseFeed(omap.From("data", omap.From("feeds", []any{omap.From("type", "banner_card")})), "sg")
	if len(r) != 0 || tot != nil {
		t.Fatalf("banner-only feed = %v %v", r, tot)
	}
}

func toFloatOrZero(v any) float64 {
	f, ok := toFloat(v)
	if !ok {
		return 0
	}
	return f
}

// ---- request schemas (test_schemas.py) --------------------------------------

func TestProductSchemaPairAndLinks(t *testing.T) {
	data, errs := loadProductDetails(map[string]any{"product": "15235546.23326616342"})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	eq(t, data["product"], "15235546.23326616342")
	eq(t, data["country"], "sg")

	data, errs = loadProductDetails(map[string]any{"product": "15235546/23326616342", "country": "TH"})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	eq(t, data["product"], "15235546.23326616342")
	eq(t, data["country"], "th")

	data, errs = loadProductDetails(map[string]any{
		"product": "https://shopee.com.br/Novo-iPhone-i.486926153.22192886389", "country": "sg"})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	eq(t, data["product"], "486926153.22192886389")
	eq(t, data["country"], "br")
}

func TestProductSchemaRejects(t *testing.T) {
	for _, value := range []string{"", "abc", "https://example.com/product/1/2", "https://shopee.sg/search?keyword=x"} {
		_, errs := loadProductDetails(map[string]any{"product": value})
		if len(errs["product"]) == 0 {
			t.Errorf("product %q should be rejected", value)
		}
	}
	_, errs := loadProductDetails(map[string]any{"product": "1.2", "url": "https://shopee.sg/product/1/2"})
	if len(errs["url"]) == 0 {
		t.Fatal("unknown url should be rejected")
	}
	_, errs = loadProductDetails(map[string]any{"product": "1.2", "country": "cl"})
	if len(errs["country"]) == 0 {
		t.Fatal("country cl should be rejected")
	}
	_, errs = loadProductDetails(map[string]any{})
	if len(errs["product"]) == 0 {
		t.Fatal("missing product should be rejected")
	}
}

func TestShopSchema(t *testing.T) {
	cases := []struct {
		raw     map[string]any
		shop    string
		country string
	}{
		{map[string]any{"shop": "15235546"}, "15235546", "sg"},
		{map[string]any{"shop": "LorealParisSG", "country": "my"}, "lorealparissg", "my"},
		{map[string]any{"shop": "https://shopee.co.th/shop/146916334"}, "146916334", "th"},
		{map[string]any{"shop": "https://shopee.sg/lorealparissg"}, "lorealparissg", "sg"},
	}
	for _, c := range cases {
		data, errs := loadShop(c.raw)
		if len(errs) != 0 {
			t.Fatalf("%v: %v", c.raw, errs)
		}
		eq(t, data["shop"], c.shop)
		eq(t, data["country"], c.country)
	}
	_, errs := loadShop(map[string]any{"shop": "https://shopee.sg/cart/"})
	if len(errs["shop"]) == 0 {
		t.Fatal("cart link should be rejected")
	}
	_, errs = loadShop(map[string]any{})
	if len(errs["shop"]) == 0 {
		t.Fatal("missing shop should be rejected")
	}
}

func TestShopCategoriesAndReviewsSchemas(t *testing.T) {
	data, errs := loadShopCategories(map[string]any{"shop": "1", "page": "2"})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	eq(t, data["shop"], "1")
	eq(t, data["country"], "sg")
	eq(t, data["page"], 2)

	data, errs = loadShopReviews(map[string]any{"shop": "1"})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	eq(t, data["shop"], "1")
	eq(t, data["country"], "sg")
	eq(t, data["rating"], 5)
	eq(t, data["page"], 1)

	data, errs = loadShopReviews(map[string]any{"shop": "1", "rating": "3", "page": "4"})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	eq(t, data["rating"], 3)
	eq(t, data["page"], 4)

	_, errs = loadShopReviews(map[string]any{"shop": "1", "rating": "6"})
	if len(errs["rating"]) == 0 {
		t.Fatal("rating 6 should be rejected")
	}
	_, errs = loadShopReviews(map[string]any{"shop": "1", "page": "0"})
	if len(errs["page"]) == 0 {
		t.Fatal("page 0 should be rejected")
	}
}

func TestSimpleMarketSchemas(t *testing.T) {
	loaders := []struct {
		name string
		fn   func(map[string]any) (map[string]any, validationErrors)
	}{
		{"categories", loadCategoriesList}, {"trending searches", loadTrendingSearches},
		{"trending products", loadTrendingProducts},
	}
	for _, l := range loaders {
		data, errs := l.fn(map[string]any{})
		if len(errs) != 0 {
			t.Fatalf("%s: %v", l.name, errs)
		}
		eq(t, data["country"], "sg")
	}
	data, errs := loadCategoriesList(map[string]any{"country": "id"})
	if len(errs) == 0 {
		eq(t, data["country"], "id")
	}
	data, errs = loadTrendingSearches(map[string]any{"country": "Vn"})
	if len(errs) == 0 {
		eq(t, data["country"], "vn")
	}
	data, errs = loadOfficialShops(map[string]any{"page": "3"})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	eq(t, data["country"], "sg")
	eq(t, data["page"], 3)
	_, errs = loadCategoriesList(map[string]any{"country": "us"})
	if len(errs["country"]) == 0 {
		t.Fatal("country us should be rejected")
	}
}

func TestAutocompleteSchema(t *testing.T) {
	data, errs := loadAutocomplete(map[string]any{"query": " iphone "})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	eq(t, data["query"], "iphone")
	eq(t, data["country"], "sg")
	_, errs = loadAutocomplete(map[string]any{})
	if len(errs["query"]) == 0 {
		t.Fatal("missing query should be rejected")
	}
	_, errs = loadAutocomplete(map[string]any{"query": strings.Repeat("x", 101)})
	if len(errs["query"]) == 0 {
		t.Fatal("long query should be rejected")
	}
}

func TestFlashSaleSchema(t *testing.T) {
	data, errs := loadFlashSale(map[string]any{})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	eq(t, data["country"], "sg")
	eq(t, data["session"], nil)
	eq(t, data["category"], nil)
	eq(t, data["page"], 1)

	data, errs = loadFlashSale(map[string]any{"session": "513667598795255", "category": "17", "page": "2", "country": "my"})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	eq(t, data["country"], "my")
	eq(t, data["session"], 513667598795255)
	eq(t, data["category"], 17)
	eq(t, data["page"], 2)

	_, errs = loadFlashSale(map[string]any{"session": "abc"})
	if len(errs["session"]) == 0 {
		t.Fatal("non-numeric session should be rejected")
	}
}
