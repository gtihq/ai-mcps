package main

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/internal/omap"
)

// MONEY_UNIT is parsers.MONEY_UNIT. Shopee prices are integers in 1/100000 of
// the local currency.
const MONEY_UNIT = 100000

var (
	metaRE  = regexp.MustCompile(`<meta[^>]+(?:name|property)="(description|og:url|og:image)"[^>]+content="([^"]*)"`)
	blobRE  = regexp.MustCompile(`(?s)<script type="text/mfe-initial-data"[^>]*>(.*?)</script>`)
	titleRE = regexp.MustCompile(`(?s)<title[^>]*>(.*?)</title>`)
)

// number is parsers.number.
func number(v any) any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case bool:
		return nil
	case int:
		return x
	case int64:
		return x
	case float64:
		return x
	case json.Number:
		s := string(x)
		if strings.Contains(s, ".") {
			f, err := x.Float64()
			if err != nil {
				return nil
			}
			return f
		}
		i, err := x.Int64()
		if err != nil {
			return nil
		}
		return i
	case string:
		s := strings.TrimSpace(x)
		if strings.Contains(s, ".") {
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil
			}
			return f
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil
		}
		return i
	}
	return nil
}

// money is parsers.money.
func money(v any) any {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok && s == "" {
		return nil
	}
	if _, ok := v.(bool); ok {
		return nil
	}
	f, ok := toFloat(v)
	if !ok || f == -1 {
		return nil
	}
	amount := f / MONEY_UNIT
	if amount <= 0 {
		return nil
	}
	return round2(amount)
}

// text is parsers.text.
func text(v any) any {
	if v == nil {
		return nil
	}
	switch v.(type) {
	case bool, int, int64, float64, json.Number, []any, *omap.M, map[string]any:
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	s = html.UnescapeString(strings.TrimSpace(s))
	if s == "" || s == "-" || s == "N/A" || s == "null" {
		return nil
	}
	return s
}

// flag is parsers.flag.
func flagValue(v any) any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case bool:
		return x
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case json.Number:
		f, ok := toFloat(x)
		if !ok {
			return nil
		}
		return f != 0
	}
	return nil
}

// ts is parsers.ts.
func ts(v any) any {
	if v == nil {
		return nil
	}
	if _, ok := v.(bool); ok {
		return nil
	}
	i, ok := toInt64(v)
	if !ok || i == 0 {
		return nil
	}
	// Python datetime.fromtimestamp raises for out-of-range values.
	if i < -62135596800 || i > 253402300799 {
		return nil
	}
	return time.Unix(i, 0).UTC().Format("2006-01-02T15:04:05Z")
}

// image is parsers.image.
func image(country string, v any) any { return imageLink(country, v) }

// images is parsers.images.
func images(country string, values any) []any {
	out := make([]any, 0)
	for _, v := range asSlice(values) {
		if isTruthy(v) {
			out = append(out, image(country, v))
		}
	}
	return out
}

// ratingBlock is parsers.rating_block.
func ratingBlock(raw any) any {
	m := asM(raw)
	if m == nil {
		return nil
	}
	counts := firstNN(mget(m, "rating_count"), mget(m, "rating_counts"))
	var byStar any
	var total any
	if list := asSlice(counts); len(list) == 6 {
		total = number(list[0])
		byStar = omap.New()
		for star := 1; star <= 5; star++ {
			byStar.(*omap.M).Set(strconv.Itoa(star), number(list[star]))
		}
	} else if list := asSlice(counts); len(list) > 0 {
		total = number(list[0])
	}
	average := number(mget(m, "rating_star"))
	if average == nil && total == nil {
		return nil
	}
	avg := average
	if f, ok := average.(float64); ok {
		avg = round2(f)
	}
	return omap.From("average", avg, "count", total, "counts_by_star", byStar)
}

// ---- product ----------------------------------------------------------------

var CONDITIONS = map[int64]string{1: "new", 2: "used"}

// pageMeta is parsers.page_meta.
func pageMeta(htmlText string) map[string]string {
	found := make(map[string]string)
	for _, m := range metaRE.FindAllStringSubmatch(htmlText, -1) {
		key := m[1]
		if _, exists := found[key]; !exists {
			found[key] = html.UnescapeString(m[2])
		}
	}
	return found
}

// productBlob is fetch.product_blob.
func productBlob(htmlText string) any {
	m := blobRE.FindStringSubmatch(htmlText)
	if m == nil {
		return nil
	}
	v, err := decodeJSON([]byte(m[1]))
	if err != nil {
		return nil
	}
	return v
}

// pageTitle is fetch.page_title.
func pageTitle(htmlText string) string {
	if m := titleRE.FindStringSubmatch(htmlText); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// blobRecord is parsers._blob_record.
func blobRecord(blob any) *omap.M {
	root := asM(blob)
	initial := asM(mget(root, "initialState"))
	domain := asM(mget(initial, "DOMAIN_PDP"))
	data := asM(mget(domain, "data"))
	bff := asM(mget(data, "PDP_BFF_DATA"))
	cached := asM(mget(bff, "cachedMap"))
	if cached == nil || cached.Len() == 0 {
		return nil
	}
	keys := cached.Keys()
	record := asM(mget(cached, keys[0]))
	if record == nil || asM(mget(record, "item")) == nil {
		return nil
	}
	return record
}

func descriptionOf(record, item *omap.M) any {
	pd := asM(mget(record, "product_description"))
	lines := make([]string, 0)
	for _, p := range asSlice(mget(pd, "paragraph_list")) {
		pm := asM(p)
		if pm == nil || !numEq(mget(pm, "type"), 0) {
			continue
		}
		if line := text(mget(pm, "text")); line != nil {
			lines = append(lines, line.(string))
		}
	}
	if len(lines) > 0 {
		return strings.Join(lines, "\n")
	}
	return text(mget(item, "description"))
}

func isEmptyString(v any) bool {
	s, ok := v.(string)
	return ok && s == ""
}

func variationsOf(country string, item *omap.M) []any {
	out := make([]any, 0)
	for _, raw := range asSlice(mget(item, "tier_variations")) {
		tier := asM(raw)
		if tier == nil {
			continue
		}
		name := text(mget(tier, "name"))
		options := asSlice(mget(tier, "options"))
		tierImages := asSlice(mget(tier, "images"))
		placeholder := len(options) == 0 || (len(options) == 1 && isEmptyString(options[0]))
		if name == nil && placeholder {
			continue
		}
		opts := make([]any, 0, len(options))
		for i, opt := range options {
			var img any
			if i < len(tierImages) {
				img = image(country, tierImages[i])
			}
			opts = append(opts, omap.From("name", text(opt), "image", img))
		}
		out = append(out, omap.From("name", name, "options", opts))
	}
	return out
}

func modelsOf(item *omap.M) []any {
	out := make([]any, 0)
	for _, raw := range asSlice(mget(item, "models")) {
		model := asM(raw)
		if model == nil {
			continue
		}
		ext := asM(mget(model, "extinfo"))
		var tierIndex any
		if list, ok := mget(ext, "tier_index").([]any); ok {
			tierIndex = list
		}
		out = append(out, omap.From(
			"id", first(mget(model, "model_id"), mget(model, "modelid")),
			"name", text(mget(model, "name")),
			"option_indexes", tierIndex,
			"price", money(mget(model, "price")),
			"stock", nil,
			"is_pre_order", flagValue(mget(ext, "is_pre_order")),
			"estimated_delivery_days", number(mget(ext, "estimated_days")),
			"is_available", numEq(mget(model, "status"), 1),
		))
	}
	return out
}

func attributesOf(record, item *omap.M) []any {
	attrs := asSlice(mget(item, "attributes"))
	if len(attrs) == 0 {
		pa := asM(mget(record, "product_attributes"))
		attrs = asSlice(mget(pa, "attrs"))
	}
	out := make([]any, 0)
	for _, raw := range attrs {
		attr := asM(raw)
		if attr == nil || text(mget(attr, "name")) == nil {
			continue
		}
		out = append(out, omap.From(
			"id", mget(attr, "id"),
			"name", text(mget(attr, "name")),
			"value", text(mget(attr, "value")),
		))
	}
	return out
}

func categoriesOf(country string, item *omap.M) []any {
	cats := asSlice(mget(item, "fe_categories"))
	if len(cats) == 0 {
		cats = asSlice(mget(item, "categories"))
	}
	out := make([]any, 0)
	for _, raw := range cats {
		c := asM(raw)
		if c == nil || !isTruthy(mget(c, "catid")) {
			continue
		}
		name := mget(c, "display_name")
		nameStr := ""
		if s, ok := name.(string); ok {
			nameStr = s
		}
		out = append(out, omap.From(
			"id", mget(c, "catid"),
			"name", text(name),
			"link", categoryLink(country, nameStr, mget(c, "catid")),
		))
	}
	return out
}

func videosOf(item *omap.M) []any {
	out := make([]any, 0)
	for _, raw := range asSlice(mget(item, "video_info_list")) {
		video := asM(raw)
		if video == nil {
			continue
		}
		var link any
		if df := asM(mget(video, "default_format")); df != nil {
			link = mget(df, "url")
		} else {
			link = mget(video, "url")
		}
		out = append(out, omap.From(
			"id", mget(video, "video_id"),
			"link", link,
			"thumbnail", mget(video, "thumb_url"),
			"duration_seconds", number(mget(video, "duration")),
		))
	}
	return out
}

func servicesOf(record *omap.M) []any {
	drawer := asM(mget(record, "service_drawer"))
	entrance := asM(mget(record, "service_entrance"))
	list := asSlice(mget(drawer, "services"))
	if len(list) == 0 {
		list = asSlice(mget(entrance, "services"))
	}
	out := make([]any, 0)
	for _, raw := range list {
		svc := asM(raw)
		if svc == nil {
			continue
		}
		title := text(firstTruthy(mget(svc, "title"), mget(svc, "text")))
		if title == nil {
			continue
		}
		out = append(out, omap.From("title", title, "description", text(mget(svc, "description"))))
	}
	return out
}

// parseProduct is parsers.parse_product.
func parseProduct(htmlText string, blob any, country string) any {
	record := blobRecord(blob)
	if record == nil {
		return nil
	}
	item := asM(mget(record, "item"))
	shopID := first(mget(item, "shop_id"), mget(item, "shopid"))
	itemID := first(mget(item, "item_id"), mget(item, "itemid"))
	meta := pageMeta(htmlText)
	brand := text(mget(item, "brand"))
	var condition any
	if n, ok := toInt64(mget(item, "condition")); ok {
		condition = CONDITIONS[n]
	}
	var brandBlock any
	if brand != nil {
		brandBlock = omap.From("id", orNil(mget(item, "brand_id")), "name", brand)
	}
	imagesValue := asSlice(mget(item, "images"))
	if len(imagesValue) == 0 {
		pi := asM(mget(record, "product_images"))
		imagesValue = asSlice(mget(pi, "images"))
	}
	minQty := number(mget(item, "min_purchase_limit"))
	if minQty == nil || numEq(minQty, 0) {
		minQty = nil
	}
	var canonical any = meta["og:url"]
	if canonical == "" {
		canonical = nil
	}
	return omap.From(
		"id", itemID,
		"shop_id", shopID,
		"name", text(first(mget(item, "title"), mget(item, "name"))),
		"link", productLink(country, shopID, itemID),
		"canonical_link", canonical,
		"status", text(mget(item, "item_status")),
		"condition", condition,
		"brand", brandBlock,
		"currency", CURRENCY_OF[country],
		"price", money(mget(item, "price")),
		"price_min", money(mget(item, "price_min")),
		"price_max", money(mget(item, "price_max")),
		"price_before_discount", money(mget(item, "price_before_discount")),
		"stock", nil,
		"sold_count", number(mget(item, "historical_sold")),
		"rating", ratingBlock(mget(item, "item_rating")),
		"reviews_count", number(mget(item, "cmt_count")),
		"likes_count", number(mget(item, "liked_count")),
		"description", descriptionOf(record, item),
		"images", images(country, imagesValue),
		"videos", videosOf(item),
		"variations", variationsOf(country, item),
		"models", modelsOf(item),
		"attributes", attributesOf(record, item),
		"categories", categoriesOf(country, item),
		"services", servicesOf(record),
		"shipping", omap.From(
			"is_free_shipping", flagValue(mget(item, "is_free_shipping")),
			"estimated_delivery_days", number(mget(item, "estimated_days")),
			"ships_from", text(mget(item, "shop_location")),
		),
		"purchase_limits", omap.From(
			"min_quantity", minQty,
			"max_quantity", number(mget(item, "overall_purchase_limit")),
		),
		"is_pre_order", flagValue(mget(item, "is_pre_order")),
		"is_adult", flagValue(mget(item, "is_adult")),
		"has_video", flagValue(mget(item, "item_has_video")),
		"is_official_shop", flagValue(mget(item, "is_official_shop")),
		"is_mart", flagValue(mget(item, "is_mart")),
		"listed_at", ts(mget(item, "ctime")),
		"shop", omap.From("id", shopID, "link", shopLink(country, shopID)),
	)
}
