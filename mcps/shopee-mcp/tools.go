package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
	"github.com/gtihq/ai-mcps/internal/omap"
)

// DEFAULT_COUNTRY is mcp_server.DEFAULT_COUNTRY.
const DEFAULT_COUNTRY = "br"

// Docstrings, verbatim, from the original Python server. The em dash in
// descSearchProducts is written as its Unicode escape so the source keeps the
// "no em dash" rule while the wire description stays byte-for-byte equal.
const (
	descSearchAutocomplete = "Shopee search autocomplete: keyword and shop suggestions for a partial\n" +
		"query. Returns suggested keywords and matching shops."

	descSearchTrending = "Shopee's current trending searches for a market."

	descSearchProducts = "Full keyword product search on Shopee with live prices, discounts,\n" +
		"ratings and sold counts.\n\n" +
		"REQUIRES a logged-in session: run `python login.py` once (it opens a\n" +
		"browser for you to log in and saves the cookies), then this works.\n" +
		"Shopee blocks headless and raw HTTP requests (anti-bot error 90309999),\n" +
		"so the search runs in a real Chrome window and reads the results from\n" +
		"the page itself. Calls are serialized and spaced by at least\n" +
		"SHOPEE_MIN_INTERVAL_SECONDS (default 10s) between searches \u2014 one\n" +
		"window, one page load, no retries.\n\n" +
		"sort: relevancy, sales, newest, price_asc, price_desc.\n" +
		"min_price / max_price are in the market currency (BRL for country=br).\n"

	descProductsTrending = "~100 trending products with live prices and ratings for a market."

	descProductDetails = "Full product detail (title, description, images, variations, SKUs,\n" +
		"attributes, categories, shipping, shop profile) from a product ID\n" +
		"('<shop_id>.<item_id>') or any Shopee product URL.\n\n" +
		"`shipping` reports `is_free_shipping`, `estimated_delivery_days` and\n" +
		"`ships_from`. Shopee does not expose a precise freight value via the\n" +
		"public API; for an exact cost the buyer must enter their CEP on the\n" +
		"product page. Ask the user for their CEP and, when needed, point them\n" +
		"to the product link to calculate frete.\n\n" +
		"NOTE: Shopee only serves price, stock, sold count, rating and review\n" +
		"count to logged-in sessions, so those fields are null here. Live prices\n" +
		"come from `deals_flash_sale` and `products_trending` instead."

	descDealsFlashSale = "Live flash-sale deals (50 per page) with price, discount, stock and\n" +
		"units sold. `session`/`category` are optional IDs from the response's\n" +
		"`sessions` list; omit both for the ongoing session, all categories."

	descShopDetails = "Shop profile: rating breakdown, followers, response rate, vouchers.\n" +
		"`shop` is a numeric shop id, a username, or any Shopee shop URL."

	descShopReviews = "Buyer reviews for a shop, filtered by star rating (5, 4, 3, 2 or 1;\n" +
		"default 5). 20 per page."

	descShopCategories = "A shop's own product categories with item counts (50 per page)."

	descShopsOfficial = "Every official brand store in a market (20 per page)."

	descCategoriesList = "The full Shopee category tree (IDs and links) for a market."

	descListOrders = "List the logged-in user's Shopee purchases with shipping status.\n\n" +
		"Requires a logged-in session: run `python login.py` first. Uses the saved\n" +
		"browser profile. Returns the purchases page text (order ids and statuses).\n"
)

// nullDefault is a typed nil. mcpserver emits "default": null for it, matching
// the `= None` defaults of the original signatures.
var nullDefault any = (*string)(nil)

// _SEARCH_SORT from mcp_server.py. Order is the order of the error message.
var searchSorts = []string{"relevancy", "sales", "newest", "price_asc", "price_desc"}

var searchSortMap = map[string][2]string{
	"relevancy":  {"relevancy", "desc"},
	"sales":      {"sales", "desc"},
	"newest":     {"ctime", "desc"},
	"price_asc":  {"price", "asc"},
	"price_desc": {"price", "desc"},
}

// tools returns the 12 tools in the same order and shape as the Python server.
func tools() []*mcpserver.Tool {
	return []*mcpserver.Tool{
		{
			Name:        "search_autocomplete",
			Description: descSearchAutocomplete,
			Properties: []mcpserver.NamedProperty{
				{Name: "query", Prop: mcpserver.Property{Type: "string", Title: "Query"}},
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
			},
			Required: []string{"query"},
			Handler:  handleSearchAutocomplete,
		},
		{
			Name:        "search_trending",
			Description: descSearchTrending,
			Properties: []mcpserver.NamedProperty{
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
			},
			Handler: handleSearchTrending,
		},
		{
			Name:        "search_products",
			Description: descSearchProducts,
			Properties: []mcpserver.NamedProperty{
				{Name: "keyword", Prop: mcpserver.Property{Type: "string", Title: "Keyword"}},
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
				{Name: "sort", Prop: mcpserver.Property{Type: "string", Title: "Sort", Default: "relevancy"}},
				{Name: "page", Prop: mcpserver.Property{Type: "integer", Title: "Page", Default: 1}},
				{Name: "limit", Prop: mcpserver.Property{Type: "integer", Title: "Limit", Default: 20}},
				{Name: "min_price", Prop: mcpserver.Property{Type: "number", Title: "Min Price", Default: nullDefault, Nullable: true, HasDefault: true}},
				{Name: "max_price", Prop: mcpserver.Property{Type: "number", Title: "Max Price", Default: nullDefault, Nullable: true, HasDefault: true}},
			},
			Required: []string{"keyword"},
			Handler:  handleSearchProducts,
		},
		{
			Name:        "products_trending",
			Description: descProductsTrending,
			Properties: []mcpserver.NamedProperty{
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
			},
			Handler: handleProductsTrending,
		},
		{
			Name:        "product_details",
			Description: descProductDetails,
			Properties: []mcpserver.NamedProperty{
				{Name: "product", Prop: mcpserver.Property{Type: "string", Title: "Product"}},
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
			},
			Required: []string{"product"},
			Handler:  handleProductDetails,
		},
		{
			Name:        "deals_flash_sale",
			Description: descDealsFlashSale,
			Properties: []mcpserver.NamedProperty{
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
				{Name: "session", Prop: mcpserver.Property{Type: "integer", Title: "Session", Default: nullDefault, Nullable: true, HasDefault: true}},
				{Name: "category", Prop: mcpserver.Property{Type: "integer", Title: "Category", Default: nullDefault, Nullable: true, HasDefault: true}},
				{Name: "page", Prop: mcpserver.Property{Type: "integer", Title: "Page", Default: 1}},
			},
			Handler: handleDealsFlashSale,
		},
		{
			Name:        "shop_details",
			Description: descShopDetails,
			Properties: []mcpserver.NamedProperty{
				{Name: "shop", Prop: mcpserver.Property{Type: "string", Title: "Shop"}},
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
			},
			Required: []string{"shop"},
			Handler:  handleShopDetails,
		},
		{
			Name:        "shop_reviews",
			Description: descShopReviews,
			Properties: []mcpserver.NamedProperty{
				{Name: "shop", Prop: mcpserver.Property{Type: "string", Title: "Shop"}},
				{Name: "rating", Prop: mcpserver.Property{Type: "integer", Title: "Rating", Default: 5}},
				{Name: "page", Prop: mcpserver.Property{Type: "integer", Title: "Page", Default: 1}},
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
			},
			Required: []string{"shop"},
			Handler:  handleShopReviews,
		},
		{
			Name:        "shop_categories",
			Description: descShopCategories,
			Properties: []mcpserver.NamedProperty{
				{Name: "shop", Prop: mcpserver.Property{Type: "string", Title: "Shop"}},
				{Name: "page", Prop: mcpserver.Property{Type: "integer", Title: "Page", Default: 1}},
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
			},
			Required: []string{"shop"},
			Handler:  handleShopCategories,
		},
		{
			Name:        "shops_official",
			Description: descShopsOfficial,
			Properties: []mcpserver.NamedProperty{
				{Name: "page", Prop: mcpserver.Property{Type: "integer", Title: "Page", Default: 1}},
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
			},
			Handler: handleShopsOfficial,
		},
		{
			Name:        "categories_list",
			Description: descCategoriesList,
			Properties: []mcpserver.NamedProperty{
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
			},
			Handler: handleCategoriesList,
		},
		{
			Name:        "list_orders",
			Description: descListOrders,
			Properties: []mcpserver.NamedProperty{
				{Name: "country", Prop: mcpserver.Property{Type: "string", Title: "Country", Default: DEFAULT_COUNTRY}},
			},
			Handler: handleListOrders,
		},
	}
}

// ---- handlers ---------------------------------------------------------------

func errMap(message string) *omap.M { return omap.From("error", message) }

func safe(fn func() (any, error)) any {
	v, err := fn()
	if err != nil {
		return errMap(err.Error())
	}
	return v
}

// countryArg is mcp_server._country: DEFAULT_COUNTRY br, case-insensitive.
func countryArg(args map[string]any) (string, error) {
	return countryCode(argString(args, "country"))
}

func countryCode(cc string) (string, error) {
	cc = strings.ToLower(cc)
	if cc == "" {
		cc = DEFAULT_COUNTRY
	}
	for _, market := range MARKETS {
		if cc == market {
			return cc, nil
		}
	}
	return "", fmt.Errorf("country must be one of %s (got '%s')", marketsJoined(), cc)
}

// resolveProductArg is mcp_server._resolve_product.
func resolveProductArg(value, country string) (string, string, error) {
	ref, err := resolveProduct(value)
	if err != nil {
		return "", "", err
	}
	cc := country
	if ref.Country != nil {
		cc = *ref.Country
	}
	return cc, fmt.Sprintf("%d.%d", ref.ShopID, ref.ItemID), nil
}

// resolveShopArg is mcp_server._resolve_shop.
func resolveShopArg(value, country string) (string, string, error) {
	ref, err := resolveShop(value)
	if err != nil {
		return "", "", err
	}
	cc := country
	if ref.Country != nil {
		cc = *ref.Country
	}
	canonical := ""
	if ref.ShopID != nil {
		canonical = strconv.FormatInt(*ref.ShopID, 10)
	} else if ref.Username != nil {
		canonical = *ref.Username
	}
	return cc, canonical, nil
}

func handleSearchAutocomplete(_ context.Context, args map[string]any) (any, error) {
	cc, err := countryArg(args)
	if err != nil {
		return errMap(err.Error()), nil
	}
	query := argString(args, "query")
	return safe(func() (any, error) { return apiSearchAutocomplete(cc, query) }), nil
}

func handleSearchTrending(_ context.Context, args map[string]any) (any, error) {
	cc, err := countryArg(args)
	if err != nil {
		return errMap(err.Error()), nil
	}
	return safe(func() (any, error) { return apiSearchTrending(cc) }), nil
}

func handleSearchProducts(_ context.Context, args map[string]any) (any, error) {
	cc, err := countryArg(args)
	if err != nil {
		return errMap(err.Error()), nil
	}
	return searchBrowser(
		cc,
		argString(args, "keyword"),
		argStringDefault(args, "sort", "relevancy"),
		argInt(args, "page", 1),
		argInt(args, "limit", 20),
		argFloatOrNil(args, "min_price"),
		argFloatOrNil(args, "max_price"),
	), nil
}

func handleProductsTrending(_ context.Context, args map[string]any) (any, error) {
	cc, err := countryArg(args)
	if err != nil {
		return errMap(err.Error()), nil
	}
	return safe(func() (any, error) { return apiProductsTrending(cc) }), nil
}

func handleProductDetails(_ context.Context, args map[string]any) (any, error) {
	cc, err := countryArg(args)
	if err != nil {
		return errMap(err.Error()), nil
	}
	cc, canonical, err := resolveProductArg(argString(args, "product"), cc)
	if err != nil {
		return errMap(err.Error()), nil
	}
	return safe(func() (any, error) { return apiProductDetails(cc, canonical) }), nil
}

func handleDealsFlashSale(_ context.Context, args map[string]any) (any, error) {
	cc, err := countryArg(args)
	if err != nil {
		return errMap(err.Error()), nil
	}
	return safe(func() (any, error) {
		return apiDealsFlashSale(cc, argIntOrNil(args, "session"), argIntOrNil(args, "category"), argInt(args, "page", 1))
	}), nil
}

func handleShopDetails(_ context.Context, args map[string]any) (any, error) {
	cc, err := countryArg(args)
	if err != nil {
		return errMap(err.Error()), nil
	}
	cc, canonical, err := resolveShopArg(argString(args, "shop"), cc)
	if err != nil {
		return errMap(err.Error()), nil
	}
	return safe(func() (any, error) { return apiShopDetails(cc, canonical) }), nil
}

func handleShopReviews(_ context.Context, args map[string]any) (any, error) {
	cc, err := countryArg(args)
	if err != nil {
		return errMap(err.Error()), nil
	}
	cc, canonical, err := resolveShopArg(argString(args, "shop"), cc)
	if err != nil {
		return errMap(err.Error()), nil
	}
	rating := argInt(args, "rating", 5)
	page := argInt(args, "page", 1)
	return safe(func() (any, error) { return apiShopReviews(cc, canonical, rating, page) }), nil
}

func handleShopCategories(_ context.Context, args map[string]any) (any, error) {
	cc, err := countryArg(args)
	if err != nil {
		return errMap(err.Error()), nil
	}
	cc, canonical, err := resolveShopArg(argString(args, "shop"), cc)
	if err != nil {
		return errMap(err.Error()), nil
	}
	page := argInt(args, "page", 1)
	return safe(func() (any, error) { return apiShopCategories(cc, canonical, page) }), nil
}

func handleShopsOfficial(_ context.Context, args map[string]any) (any, error) {
	cc, err := countryArg(args)
	if err != nil {
		return errMap(err.Error()), nil
	}
	page := argInt(args, "page", 1)
	return safe(func() (any, error) { return apiShopsOfficial(cc, page) }), nil
}

func handleCategoriesList(_ context.Context, args map[string]any) (any, error) {
	cc, err := countryArg(args)
	if err != nil {
		return errMap(err.Error()), nil
	}
	return safe(func() (any, error) { return apiCategoriesList(cc) }), nil
}

func handleListOrders(_ context.Context, args map[string]any) (any, error) {
	// Unlike the other tools, list_orders does not call _country: the Python
	// tool passes `country` straight to refs.site().
	cc := argString(args, "country")
	if cc == "" {
		cc = DEFAULT_COUNTRY
	}
	return listOrders(cc), nil
}

// ---- argument helpers -------------------------------------------------------

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

func argStringDefault(args map[string]any, key, def string) string {
	if v := argString(args, key); v != "" {
		return v
	}
	return def
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
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return def
		}
		return n
	}
	return def
}

func argIntOrNil(args map[string]any, key string) any {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return nil
		}
		return n
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return nil
		}
		return n
	}
	return nil
}

func argFloatOrNil(args map[string]any, key string) any {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	f, ok := toFloat(v)
	if !ok {
		return nil
	}
	return f
}

// pyExcName stands in for Python's type(e).__name__.
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

// ---- _parse_search_items ----------------------------------------------------

var soldNumberRE = regexp.MustCompile(`[\d.]+`)

// soldNumber is the local sold_number helper of mcp_server._parse_search_items.
func soldNumber(v any) any {
	if m := asM(v); m != nil {
		v = first(mget(m, "historical_sold_count"), mget(m, "text"), mget(m, "value"), mget(m, "count"))
	}
	if v == nil {
		return nil
	}
	if _, ok := v.(bool); ok {
		return nil
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return x
	case float64:
		return x
	}
	textValue := strings.ToLower(fmt.Sprint(v))
	textValue = strings.ReplaceAll(textValue, ".", "")
	textValue = strings.ReplaceAll(textValue, ",", ".")
	match := soldNumberRE.FindString(textValue)
	if match == "" {
		return nil
	}
	amount, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return nil
	}
	if strings.Contains(textValue, "mil") {
		amount *= 1000
	} else if strings.Contains(textValue, "k") {
		amount *= 1000
	}
	if amount == math.Trunc(amount) {
		return int64(amount)
	}
	return amount
}

func searchStockValue(b, d *omap.M) any {
	if v, ok := b.Get("stock"); ok && v != nil {
		return number(v)
	}
	if isTruthy(mget(d, "is_sold_out")) {
		return int64(0)
	}
	return nil
}

// parseSearchItems is mcp_server._parse_search_items, ported exactly.
func parseSearchItems(payload any, country string) ([]any, any) {
	root := asM(payload)
	if root == nil {
		root = omap.New()
	}
	data := asM(mget(root, "data"))
	if data == nil {
		data = omap.New()
	}
	items := asSlice(mget(data, "items"))
	if len(items) == 0 {
		items = asSlice(mget(root, "items"))
	}
	rows := make([]any, 0)
	for _, raw := range items {
		it := asM(raw)
		if it == nil {
			continue
		}
		b := asM(mget(it, "item_basic"))
		if b == nil {
			b = omap.New()
		}
		d := asM(mget(it, "item_data"))
		if d == nil {
			d = omap.New()
		}
		a := asM(mget(it, "item_card_displayed_asset"))
		if a == nil {
			a = omap.New()
		}
		priceInfo := asM(mget(d, "item_card_display_price"))
		if priceInfo == nil {
			priceInfo = omap.New()
		}
		shopID := first(mget(b, "shopid"), mget(d, "shopid"), mget(it, "shopid"))
		itemID := first(mget(b, "itemid"), mget(d, "itemid"), mget(it, "itemid"))
		if itemID == nil {
			continue
		}
		shopData := asM(mget(d, "shop_data"))
		if shopData == nil {
			shopData = omap.New()
		}
		rating := first(mget(b, "item_rating"), mget(d, "item_rating"))
		rows = append(rows, omap.From(
			"id", itemID,
			"shop_id", shopID,
			"name", text(first(mget(b, "name"), mget(a, "name"))),
			"link", productLink(country, shopID, itemID),
			"image", image(country, first(mget(b, "image"), mget(a, "image"))),
			"images", images(country, first(mget(b, "images"), mget(a, "images"))),
			"currency", CURRENCY_OF[country],
			"price", money(first(mget(b, "price"), mget(priceInfo, "price"))),
			"price_min", money(mget(b, "price_min")),
			"price_max", money(mget(b, "price_max")),
			"price_before_discount", money(first(mget(b, "price_before_discount"), mget(priceInfo, "strikethrough_price"))),
			"discount_percent", orNil(number(first(mget(b, "show_discount"), mget(priceInfo, "discount")))),
			"sold_count", soldNumber(first(
				mget(b, "historical_sold"), mget(d, "item_card_display_sold_count"), mget(a, "sold_count"))),
			"stock", searchStockValue(b, d),
			"rating", ratingBlock(rating),
			"likes_count", number(first(mget(b, "liked_count"), mget(d, "liked_count"))),
			"shop_location", text(first(mget(b, "shop_location"), mget(a, "shop_location"))),
			"is_shopee_verified", flagValue(first(mget(b, "shopee_verified"), mget(d, "shopee_verified"), mget(shopData, "is_shopee_verified"))),
			"is_official_shop", flagValue(first(mget(b, "is_official_shop"), mget(shopData, "is_official_shop"))),
			"is_adult", flagValue(first(mget(b, "is_adult"), mget(d, "is_adult"))),
			"is_ad", pyBool(mget(it, "adsid")),
			"listed_at", ts(first(mget(b, "ctime"), mget(d, "ctime"))),
		))
	}
	total := first(mget(data, "total_count"), mget(data, "total"), mget(root, "total_count"))
	return rows, total
}
