package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/internal/httpclient"
	"github.com/gtihq/ai-mcps/internal/omap"
	"github.com/imroc/req/v3"
)

const (
	jsonTimeout  = 20 * time.Second
	pageTimeout  = 40 * time.Second
	antiFraudErr = 90309999

	apiSourcePC  = "pc"
	apiSourceAPP = "rn"
	shopeeLang   = "en"
)

// The four failure families from scraper_errors.py / shopee/fetch.py.
type apiError struct {
	kind string // upstream, blocked, badrequest, notfound
	msg  string
}

func (e *apiError) Error() string { return e.msg }

func upstream(msg string) error   { return &apiError{kind: "upstream", msg: msg} }
func blocked(msg string) error    { return &apiError{kind: "blocked", msg: msg} }
func badRequest(msg string) error { return &apiError{kind: "badrequest", msg: msg} }
func notFound(msg string) error   { return &apiError{kind: "notfound", msg: msg} }

// Headers from shopee/fetch.py, byte for byte.
var (
	apiHeaders = map[string]string{
		"accept": "application/json", "x-api-source": apiSourcePC,
		"x-requested-with": "XMLHttpRequest", "x-shopee-language": shopeeLang,
	}
	appHeaders = map[string]string{
		"accept":       "application/json",
		"user-agent":   "Android app Shopee appver=33330 app_type=1 platform=native_android os_ver=34 Cronet/102.0.5005.125",
		"x-api-source": apiSourceAPP, "x-shopee-language": shopeeLang,
	}
	pageHeaders = map[string]string{
		"accept":       "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		"x-api-source": apiSourcePC, "x-requested-with": "XMLHttpRequest",
		"x-shopee-language": shopeeLang,
	}
)

var (
	// baseURLs lets tests point an HTTP call at an httptest server without
	// changing the public link builders in refs.go.
	baseURLs = map[string]string{}

	// Retry policy from config.py. Tests can lower these.
	maxRetries          = 3
	retryBackoffSeconds = 2

	apiClient  = httpclient.New(apiHeaders, jsonTimeout)
	appClient  = httpclient.New(appHeaders, jsonTimeout)
	pageClient = httpclient.New(pageHeaders, pageTimeout)

	shopNotFoundErrors     = []int64{1000000, 2003013}
	ratingsNotFoundErrors  = []int64{10000}
	vouchersNotFoundErrors = []int64{5300000}
)

func endpoint(country, path string) string {
	if base := baseURLs[country]; base != "" {
		return base + path
	}
	return site(country) + path
}

func countryHeaders(client *req.Client, country string) *req.Request {
	return client.R().
		SetHeader("accept-language", ACCEPT_LANGUAGE_OF[country]).
		SetHeader("referer", site(country)+"/")
}

func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// retrying mirrors fetch._retrying: only upstream / blocked errors retry.
func retrying(country string, fn func() (any, error)) (any, error) {
	var last error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		value, err := fn()
		if err == nil {
			return value, nil
		}
		if e, ok := err.(*apiError); ok && e.kind != "blocked" && e.kind != "upstream" {
			return nil, err
		}
		last = err
		if attempt < maxRetries {
			time.Sleep(time.Duration(retryBackoffSeconds*attempt) * time.Second)
		}
	}
	return nil, last
}

func statusCheck(status int, text, label string) error {
	switch {
	case status == 404:
		return notFound(label + " not found")
	case status == 400:
		return badRequest(cutRunes(text, 200))
	case status == 401 || status == 403 || status == 418 || status == 429:
		return blocked(fmt.Sprintf("HTTP %d for %s", status, label))
	case status != 200:
		return upstream(fmt.Sprintf("HTTP %d for %s", status, label))
	}
	return nil
}

func containsInt(values []int64, v int64) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

// jsonOnce mirrors fetch._json_once.
func jsonOnce(country, path, label string, notFoundErrors []int64, app bool, params ...string) (any, error) {
	url := endpoint(country, path)
	if len(params) > 0 {
		url += "?" + httpclient.Encode(params...)
	}
	client := apiClient
	if app {
		client = appClient
	}
	resp, err := countryHeaders(client, country).Get(url)
	if err != nil {
		return nil, upstream(fmt.Sprintf("request failed: %s: %s", pyExcName(err), err.Error()))
	}
	if err := statusCheck(resp.StatusCode, resp.String(), label); err != nil {
		return nil, err
	}
	body := resp.Bytes()
	payload, err := decodeJSON(body)
	if err != nil {
		lower := strings.ToLower(cutRunes(resp.String(), 2000))
		if strings.Contains(lower, "verify") || strings.Contains(strings.ToLower(cutRunes(resp.String(), 300)), "<html") {
			return nil, blocked("challenge page where JSON was expected for " + label)
		}
		return nil, upstream(fmt.Sprintf("non-JSON response for %s: %s", label, cutRunes(resp.String(), 120)))
	}
	m := asM(payload)
	if m == nil {
		return nil, upstream("unexpected payload shape for " + label)
	}
	errValue := mget(m, "error")
	if numEq(errValue, antiFraudErr) || isTruthy(mget(m, "redirect_to_error_page")) {
		return nil, blocked(fmt.Sprintf("anti-fraud reply (%d) for %s", antiFraudErr, label))
	}
	if n, ok := toInt64(errValue); ok && containsInt(notFoundErrors, n) {
		return nil, notFound(label + " not found")
	}
	if errValue != nil && !numEq(errValue, 0) && errValue != "" {
		errorMsg := mget(m, "error_msg")
		if errorMsg == nil {
			errorMsg = ""
		}
		msg := fmt.Sprintf("upstream error %v %v for %s", errValue, errorMsg, label)
		return nil, upstream(strings.TrimSpace(msg))
	}
	return m, nil
}

func fetchJSON(country, path, label string, notFoundErrors []int64, app bool, params ...string) (*omap.M, error) {
	v, err := retrying(country, func() (any, error) {
		return jsonOnce(country, path, label, notFoundErrors, app, params...)
	})
	if err != nil {
		return nil, err
	}
	return asM(v), nil
}

// fetchProductPage mirrors fetch.fetch_product_page / _product_page_once.
func fetchProductPage(country string, shopID, itemID int64) (string, error) {
	label := fmt.Sprintf("product %d/%d", shopID, itemID)
	url := endpoint(country, fmt.Sprintf("/product/%d/%d", shopID, itemID))
	v, err := retrying(country, func() (any, error) {
		resp, err := countryHeaders(pageClient, country).Get(url)
		if err != nil {
			return nil, upstream(fmt.Sprintf("request failed: %s: %s", pyExcName(err), err.Error()))
		}
		if err := statusCheck(resp.StatusCode, resp.String(), label); err != nil {
			return nil, err
		}
		text := resp.String()
		final := ""
		if resp.Response != nil && resp.Response.Request != nil && resp.Response.Request.URL != nil {
			final = resp.Response.Request.URL.String()
		}
		if strings.Contains(final, "/verify/") || strings.Contains(final, "/buyer/login") {
			return nil, blocked(fmt.Sprintf("redirected to %s for %s", strings.SplitN(final, "?", 2)[0], label))
		}
		if !blobRE.MatchString(text) {
			return nil, blocked("product page served without its data blob for " + label)
		}
		return text, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// ---- transport wrappers (params keep the order of fetch.py's dicts) -------

func fetchSearchHint(country, keyword string) (*omap.M, error) {
	return fetchJSON(country, "/api/v4/search/search_hint", fmt.Sprintf("autocomplete '%s'", keyword),
		nil, false, "keyword", keyword, "search_type", "0", "version", "1")
}

func fetchSearchSuggestion(country string) (*omap.M, error) {
	return fetchJSON(country, "/api/v4/search/search_suggestion", "trending searches", nil, false)
}

func fetchCategoryTree(country string) (*omap.M, error) {
	return fetchJSON(country, "/api/v4/pages/get_category_tree", "category tree", nil, false)
}

func fetchFlashSessions(country string) (*omap.M, error) {
	return fetchJSON(country, "/api/v4/flash_sale/get_all_sessions", "flash sale sessions", nil, false)
}

func fetchFlashItems(country, promotionID string, offset, limit int, categoryID any) (*omap.M, error) {
	params := []string{
		"promotionid", promotionID, "offset", strconv.Itoa(offset), "limit", strconv.Itoa(limit),
		"need_personalize", "true", "sort_soldout", "true", "with_dp_items", "true",
	}
	if categoryID != nil {
		params = append(params, "categoryid", fmt.Sprint(categoryID))
	}
	return fetchJSON(country, "/api/v4/flash_sale/flash_sale_get_items",
		"flash sale "+promotionID, nil, false, params...)
}

func fetchDailyDiscover(country string, limit int) (*omap.M, error) {
	return fetchJSON(country, "/api/v4/homepage/get_daily_discover", "home feed", nil, false,
		"bundle", "daily_discover_main", "limit", strconv.Itoa(limit), "offset", "0")
}

func fetchOfficialShops(country string, offset, limit int) (*omap.M, error) {
	return fetchJSON(country, "/api/v4/official_shop/get_shops", "official shops", nil, false,
		"category", "-1", "offset", strconv.Itoa(offset), "limit", strconv.Itoa(limit))
}

func fetchShopBase(country, key, value string) (*omap.M, error) {
	return fetchJSON(country, "/api/v4/shop/get_shop_base", "shop "+value,
		shopNotFoundErrors, true, key, value)
}

func fetchShopCategories(country, shopID string, offset, limit int) (*omap.M, error) {
	return fetchJSON(country, "/api/v4/shop/get_categories", "shop "+shopID+" categories",
		shopNotFoundErrors, false, "shopid", shopID, "offset", strconv.Itoa(offset), "limit", strconv.Itoa(limit))
}

func fetchShopRatings(country, shopID string, stars, offset, limit int) (*omap.M, error) {
	return fetchJSON(country, "/api/v4/seller_operation/get_shop_ratings_new", "shop "+shopID+" reviews",
		ratingsNotFoundErrors, false, "shopid", shopID, "type", strconv.Itoa(stars),
		"offset", strconv.Itoa(offset), "limit", strconv.Itoa(limit))
}

func fetchShopVouchers(country, shopID string) (*omap.M, error) {
	return fetchJSON(country, "/api/v4/voucher_wallet/get_shop_vouchers_by_shopid",
		"shop "+shopID+" vouchers", vouchersNotFoundErrors, false, "shopid", shopID)
}

// ---- pagination -------------------------------------------------------------

// pagination is api._pagination.
func pagination(page, perPage int, totalCount, hasNext any) *omap.M {
	block := omap.From("page", page, "items_per_page", perPage)
	if totalCount != nil {
		block.Set("total_count", totalCount)
		total, _ := toInt64(totalCount)
		pages := 0
		if total != 0 {
			pages = int((total + int64(perPage) - 1) / int64(perPage))
			if pages < 1 {
				pages = 1
			}
		}
		block.Set("total_pages", pages)
	} else {
		block.Set("has_next", pyBool(hasNext))
		block.Set("total_pages", nil)
		block.Set("total_count", nil)
	}
	return block
}

// ---- endpoint implementations ----------------------------------------------

func shopRecord(country, shop string) (*omap.M, error) {
	var payload *omap.M
	var err error
	if isDigits(shop) {
		payload, err = fetchShopBase(country, "shopid", shop)
	} else {
		payload, err = fetchShopBase(country, "username", shop)
	}
	if err != nil {
		return nil, err
	}
	record := parseShop(payload, country)
	if !isTruthy(mget(record, "id")) {
		return nil, notFound(fmt.Sprintf("shop %s not found", shop))
	}
	shopID := fmt.Sprint(mget(record, "id"))
	vouchers, err := fetchShopVouchers(country, shopID)
	if err != nil {
		return nil, err
	}
	record.Set("vouchers", parseVouchers(vouchers))
	return record, nil
}

func apiShopDetails(country, shop string) (any, error) {
	return shopRecord(country, shop)
}

func apiShopCategories(country, shop string, page int) (any, error) {
	record, err := shopRecord(country, shop)
	if err != nil {
		return nil, err
	}
	shopID := mget(record, "id")
	shopIDStr := fmt.Sprint(shopID)
	payload, err := fetchShopCategories(country, shopIDStr, (page-1)*SHOP_CATEGORIES_PER_PAGE, SHOP_CATEGORIES_PER_PAGE)
	if err != nil {
		return nil, err
	}
	rows, total, hasMore := parseShopCategories(payload, country, shopIDStr)
	var pageBlock *omap.M
	if total != nil {
		pageBlock = pagination(page, SHOP_CATEGORIES_PER_PAGE, total, nil)
	} else {
		pageBlock = pagination(page, SHOP_CATEGORIES_PER_PAGE, nil, hasMore)
	}
	return omap.From(
		"shop", omap.From("id", shopID, "link", shopLink(country, shopID)),
		"country", country,
		"results", rows,
		"pagination", pageBlock,
	), nil
}

func apiShopReviews(country, shop string, rating, page int) (any, error) {
	record, err := shopRecord(country, shop)
	if err != nil {
		return nil, err
	}
	shopID := mget(record, "id")
	shopIDStr := fmt.Sprint(shopID)
	payload, err := fetchShopRatings(country, shopIDStr, rating,
		(page-1)*REVIEWS_PER_PAGE, REVIEWS_PER_PAGE)
	if err != nil {
		return nil, err
	}
	rows := parseShopReviews(payload, country)
	return omap.From(
		"shop", omap.From("id", shopID, "link", shopLink(country, shopID)),
		"country", country,
		"rating", rating,
		"results", rows,
		"pagination", pagination(page, REVIEWS_PER_PAGE, nil, len(rows) >= REVIEWS_PER_PAGE),
	), nil
}

func apiShopsOfficial(country string, page int) (any, error) {
	payload, err := fetchOfficialShops(country, (page-1)*OFFICIAL_SHOPS_PER_PAGE, OFFICIAL_SHOPS_PER_PAGE)
	if err != nil {
		return nil, err
	}
	rows, total := parseOfficialShops(payload, country)
	return omap.From(
		"country", country,
		"results", rows,
		"pagination", pagination(page, OFFICIAL_SHOPS_PER_PAGE, total, len(rows) >= OFFICIAL_SHOPS_PER_PAGE),
	), nil
}

func apiProductDetails(country, product string) (any, error) {
	parts := strings.Split(product, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid product reference: %s", product)
	}
	shopID, err1 := strconv.ParseInt(parts[0], 10, 64)
	itemID, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("invalid product reference: %s", product)
	}
	html, err := fetchProductPage(country, shopID, itemID)
	if err != nil {
		return nil, err
	}
	record := asM(parseProduct(html, productBlob(html), country))
	if record == nil {
		return nil, notFound(fmt.Sprintf("product %d/%d not found on %s", shopID, itemID, hostOf(country)))
	}
	shop, err := shopRecord(country, fmt.Sprint(mget(record, "shop_id")))
	if err != nil {
		return nil, err
	}
	shopBlock := omap.New()
	for _, key := range []string{
		"id", "name", "username", "link", "location", "is_official_shop",
		"is_shopee_verified", "is_preferred_plus_seller", "rating", "followers_count",
		"items_count", "response_rate_percent", "last_active_at", "joined_at",
	} {
		shopBlock.Set(key, mget(shop, key))
	}
	record.Set("shop", shopBlock)
	return record, nil
}

func apiProductsTrending(country string) (any, error) {
	payload, err := fetchDailyDiscover(country, 100)
	if err != nil {
		return nil, err
	}
	rows, _ := parseFeed(payload, country)
	return omap.From("country", country, "count", len(rows), "results", rows), nil
}

func apiDealsFlashSale(country string, session, category any, page int) (any, error) {
	payload, err := fetchFlashSessions(country)
	if err != nil {
		return nil, err
	}
	sessions := parseFlashSessions(payload, country)

	sessionID := session
	if sessionID == nil {
		var current any
		for _, s := range sessions {
			if flagValue(mget(asM(s), "is_ongoing")) == true {
				current = s
				break
			}
		}
		if current == nil && len(sessions) > 0 {
			current = sessions[0]
		}
		if current == nil {
			return omap.From(
				"country", country, "session", nil, "sessions", []any{}, "results", []any{},
				"pagination", pagination(page, FLASH_SALE_PER_PAGE, nil, false),
			), nil
		}
		sessionID = mget(asM(current), "id")
	}

	itemsPayload, err := fetchFlashItems(country, fmt.Sprint(sessionID),
		(page-1)*FLASH_SALE_PER_PAGE, FLASH_SALE_PER_PAGE, category)
	if err != nil {
		return nil, err
	}
	rows, served := parseFlashItems(itemsPayload, country)
	if served != nil && !numEq(mget(asM(served), "id"), sessionID) {
		return nil, notFound(fmt.Sprintf("flash sale session %v not found", sessionID))
	}
	var current any
	for _, s := range sessions {
		if numEq(mget(asM(s), "id"), sessionID) {
			current = s
			break
		}
	}
	chosen := current
	if chosen == nil {
		chosen = served
	}
	brief := make([]any, 0, len(sessions))
	for _, s := range sessions {
		sm := asM(s)
		brief = append(brief, omap.From(
			"id", mget(sm, "id"), "name", mget(sm, "name"),
			"starts_at", mget(sm, "starts_at"), "ends_at", mget(sm, "ends_at"),
			"is_ongoing", mget(sm, "is_ongoing"),
		))
	}
	return omap.From(
		"country", country,
		"session", chosen,
		"sessions", brief,
		"results", rows,
		"pagination", pagination(page, FLASH_SALE_PER_PAGE, nil, len(rows) >= FLASH_SALE_PER_PAGE),
	), nil
}

func apiCategoriesList(country string) (any, error) {
	payload, err := fetchCategoryTree(country)
	if err != nil {
		return nil, err
	}
	rows := parseCategoryTree(payload, country)
	return omap.From("country", country, "count", len(rows), "results", rows), nil
}

func apiSearchAutocomplete(country, query string) (any, error) {
	payload, err := fetchSearchHint(country, query)
	if err != nil {
		return nil, err
	}
	keywords, shops := parseSearchHint(payload, country)
	return omap.From(
		"country", country, "query", query, "count", len(keywords),
		"results", keywords, "shops", shops,
	), nil
}

func apiSearchTrending(country string) (any, error) {
	payload, err := fetchSearchSuggestion(country)
	if err != nil {
		return nil, err
	}
	rows := parseSearchSuggestion(payload, country)
	return omap.From("country", country, "count", len(rows), "results", rows), nil
}
