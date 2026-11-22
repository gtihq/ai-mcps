package main

import (
	"reflect"

	"github.com/gtihq/ai-mcps/internal/omap"
)

// ---- catalog ----------------------------------------------------------------

func categoryOf(country string, raw any) any {
	c := asM(raw)
	children := make([]any, 0)
	for _, child := range asSlice(mget(c, "children")) {
		if asM(child) != nil {
			children = append(children, categoryOf(country, child))
		}
	}
	name := firstNN(mget(c, "display_name"), mget(c, "name"))
	nameStr := ""
	if s, ok := name.(string); ok {
		nameStr = s
	}
	parent := orNil(mget(c, "parent_catid"))
	return omap.From(
		"id", mget(c, "catid"),
		"name", text(name),
		"level", number(mget(c, "level")),
		"parent_id", parent,
		"link", categoryLink(country, nameStr, mget(c, "catid")),
		"image", image(country, mget(c, "image")),
		"children", children,
	)
}

// parseCategoryTree is parsers.parse_category_tree.
func parseCategoryTree(payload any, country string) []any {
	root := asM(payload)
	data := asM(mget(root, "data"))
	rows := make([]any, 0)
	for _, c := range asSlice(mget(data, "category_list")) {
		if asM(c) != nil {
			rows = append(rows, categoryOf(country, c))
		}
	}
	return rows
}

// parseSearchHint is parsers.parse_search_hint.
func parseSearchHint(payload any, country string) ([]any, []any) {
	root := asM(payload)
	keywords := make([]any, 0)
	for _, raw := range asSlice(mget(root, "keywords")) {
		kw := asM(raw)
		if kw == nil || text(mget(kw, "keyword")) == nil {
			continue
		}
		keyword := text(mget(kw, "keyword")).(string)
		catID := orNil(mget(kw, "catid"))
		var category any
		if isTruthy(catID) {
			category = omap.From("id", catID, "name", text(mget(kw, "cat_display_name")))
		}
		keywords = append(keywords, omap.From(
			"keyword", keyword,
			"link", searchLink(country, keyword),
			"category", category,
		))
	}
	shops := make([]any, 0)
	hint := asM(mget(root, "shop_hint"))
	shopValues := asSlice(mget(hint, "shops"))
	if len(shopValues) == 0 {
		if h, ok := mget(root, "shop_hint").([]any); ok {
			shopValues = h
		}
	}
	for _, raw := range shopValues {
		shop := asM(raw)
		if shop != nil && isTruthy(mget(shop, "shopid")) {
			shops = append(shops, omap.From(
				"id", mget(shop, "shopid"),
				"name", text(firstTruthy(mget(shop, "shop_name"), mget(shop, "name"))),
				"link", shopLink(country, mget(shop, "shopid")),
			))
		}
	}
	return keywords, shops
}

// parseSearchSuggestion is parsers.parse_search_suggestion.
func parseSearchSuggestion(payload any, country string) []any {
	root := asM(payload)
	data := asM(mget(root, "data"))
	rows := make([]any, 0)
	for _, raw := range asSlice(mget(data, "queries")) {
		q := asM(raw)
		if q == nil || text(mget(q, "text")) == nil {
			continue
		}
		keyword := text(mget(q, "text")).(string)
		var imageValue any
		imageValue = mget(q, "image")
		if !isTruthy(imageValue) {
			if imagesList := asSlice(mget(q, "images")); len(imagesList) > 0 {
				imageValue = imagesList[0]
			} else {
				imageValue = nil
			}
		}
		itemIDs := make([]any, 0)
		for _, i := range asSlice(mget(q, "item_ids")) {
			if isTruthy(i) {
				itemIDs = append(itemIDs, i)
			}
		}
		shopIDs := make([]any, 0)
		for _, i := range asSlice(mget(q, "shop_ids")) {
			if isTruthy(i) {
				shopIDs = append(shopIDs, i)
			}
		}
		rows = append(rows, omap.From(
			"keyword", keyword,
			"link", searchLink(country, keyword),
			"image", image(country, imageValue),
			"item_ids", itemIDs,
			"shop_ids", shopIDs,
		))
	}
	return rows
}

// parseFlashSessions is parsers.parse_flash_sessions.
func parseFlashSessions(payload any, country string) []any {
	root := asM(payload)
	data := asM(mget(root, "data"))
	rows := make([]any, 0)
	for _, raw := range asSlice(mget(data, "sessions")) {
		s := asM(raw)
		if s == nil {
			continue
		}
		categories := make([]any, 0)
		for _, cr := range asSlice(mget(s, "categories")) {
			c := asM(cr)
			if c != nil {
				categories = append(categories, omap.From(
					"id", mget(c, "catid"),
					"name", text(mget(c, "catname")),
					"image", image(country, mget(c, "image")),
				))
			}
		}
		rows = append(rows, omap.From(
			"id", mget(s, "promotionid"),
			"name", text(mget(s, "name")),
			"description", text(mget(s, "description")),
			"starts_at", ts(mget(s, "start_time")),
			"ends_at", ts(mget(s, "end_time")),
			"is_ongoing", flagValue(mget(s, "is_ongoing")),
			"categories", categories,
		))
	}
	return rows
}

// parseFlashItems is parsers.parse_flash_items.
func parseFlashItems(payload any, country string) ([]any, any) {
	root := asM(payload)
	data := asM(mget(root, "data"))
	rows := make([]any, 0)
	for _, raw := range asSlice(mget(data, "items")) {
		it := asM(raw)
		if it == nil {
			continue
		}
		shopID := mget(it, "shopid")
		itemID := mget(it, "itemid")
		rating := asM(mget(it, "item_rating"))
		categoryIDs := make([]any, 0)
		for _, c := range asSlice(mget(it, "cats")) {
			if isTruthy(c) {
				categoryIDs = append(categoryIDs, c)
			}
		}
		rows = append(rows, omap.From(
			"id", itemID,
			"shop_id", shopID,
			"name", text(mget(it, "name")),
			"link", productLink(country, shopID, itemID),
			"image", image(country, mget(it, "image")),
			"images", images(country, mget(it, "promo_images")),
			"currency", CURRENCY_OF[country],
			"price", money(mget(it, "price")),
			"price_before_discount", money(mget(it, "price_before_discount")),
			"discount_percent", orNil(number(mget(it, "raw_discount"))),
			"stock", number(mget(it, "stock")),
			"flash_sale_stock", number(mget(it, "flash_sale_stock")),
			"sold_count", number(mget(it, "historical_sold")),
			"rating", ratingBlock(omap.From(
				"rating_star", mget(rating, "rating_star"),
				"rating_count", mget(rating, "rating_counts"),
			)),
			"category_ids", categoryIDs,
			"flash_sale_category_id", mget(it, "flash_catid"),
			"promotion_id", mget(it, "promotionid"),
			"starts_at", ts(mget(it, "start_time")),
			"ends_at", ts(mget(it, "end_time")),
			"is_shop_official", flagValue(mget(it, "is_shop_official")),
			"is_shop_preferred", flagValue(mget(it, "is_shop_preferred")),
			"is_shopee_choice", flagValue(mget(it, "is_shopee_choice")),
			"is_mart", flagValue(mget(it, "is_mart")),
			"is_adult", flagValue(mget(it, "is_adult")),
		))
	}
	session := asM(mget(data, "session"))
	var served any
	if isTruthy(mget(session, "promotionid")) {
		served = omap.From(
			"id", mget(session, "promotionid"),
			"starts_at", ts(mget(session, "start_time")),
			"ends_at", ts(mget(session, "end_time")),
		)
	}
	return rows, served
}

// orNil emulates `value or None` for numbers: zero becomes nil.
func orNil(v any) any {
	if v == nil || numEq(v, 0) {
		return nil
	}
	return v
}

// parseFeed is parsers.parse_feed.
func parseFeed(payload any, country string) ([]any, any) {
	root := asM(payload)
	data := asM(mget(root, "data"))
	rows := make([]any, 0)
	for _, raw := range asSlice(mget(data, "feeds")) {
		feed := asM(raw)
		if feed == nil {
			continue
		}
		card := asM(mget(feed, "centralised_item_card"))
		item := asM(mget(card, "item_data"))
		if item == nil || !isTruthy(mget(item, "itemid")) {
			continue
		}
		asset := asM(mget(card, "item_card_displayed_asset"))
		price := asM(mget(item, "item_card_display_price"))
		shop := asM(mget(item, "shop_data"))
		shopID := mget(item, "shopid")
		itemID := mget(item, "itemid")
		sold := mget(item, "item_card_display_sold_count")
		var soldCount any
		if sm := asM(sold); sm != nil {
			soldCount = number(mget(sm, "historical_sold_count"))
		} else {
			soldCount = number(sold)
		}
		before := money(mget(price, "strikethrough_price"))
		if !isTruthy(before) && !reflect.DeepEqual(mget(price, "original_price"), mget(price, "price")) {
			before = money(mget(price, "original_price"))
		}
		rows = append(rows, omap.From(
			"id", itemID,
			"shop_id", shopID,
			"name", text(mget(asset, "name")),
			"link", productLink(country, shopID, itemID),
			"image", image(country, mget(asset, "image")),
			"images", images(country, mget(asset, "images")),
			"currency", CURRENCY_OF[country],
			"price", money(mget(price, "price")),
			"price_before_discount", before,
			"discount_percent", orNil(number(mget(price, "discount"))),
			"sold_count", soldCount,
			"rating", ratingBlock(mget(item, "item_rating")),
			"likes_count", number(mget(item, "liked_count")),
			"category_id", mget(item, "catid"),
			"shop", omap.From(
				"id", shopID,
				"name", text(mget(shop, "shop_name")),
				"link", shopLink(country, shopID),
				"location", text(mget(asset, "shop_location")),
			),
			"is_sold_out", flagValue(mget(item, "is_sold_out")),
			"is_shopee_verified", flagValue(mget(item, "shopee_verified")),
			"is_adult", flagValue(mget(item, "is_adult")),
			"listed_at", ts(mget(item, "ctime")),
		))
	}
	return rows, number(mget(data, "feed_total"))
}
