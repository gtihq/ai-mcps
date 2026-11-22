package main

import (
	"fmt"
	"strings"

	"github.com/gtihq/ai-mcps/internal/omap"
)

// ---- shops ------------------------------------------------------------------

// parseShop is parsers.parse_shop.
func parseShop(payload any, country string) *omap.M {
	root := asM(payload)
	data := asM(mget(root, "data"))
	account := asM(mget(data, "account"))
	shopRating := asM(mget(data, "shop_rating"))
	tabs := asM(mget(data, "show_tab"))
	newArrivals := asM(mget(tabs, "new_arrival_tab"))
	membership := asM(mget(tabs, "membership_tab"))
	shopID := mget(data, "shopid")
	username := text(mget(account, "username"))

	good := number(firstNN(mget(data, "rating_good"), mget(shopRating, "rating_good")))
	normal := number(firstNN(mget(data, "rating_normal"), mget(shopRating, "rating_normal")))
	bad := number(firstNN(mget(data, "rating_bad"), mget(shopRating, "rating_bad")))
	var total any
	hasNumeric := false
	for _, v := range []any{good, normal, bad} {
		if _, ok := toFloat(v); ok {
			hasNumeric = true
			break
		}
	}
	if hasNumeric {
		var sum float64
		allInt := true
		for _, v := range []any{good, normal, bad} {
			f, ok := toFloat(v)
			if !ok {
				continue
			}
			sum += f
			if _, isInt := v.(int64); !isInt {
				if _, isInt2 := v.(int); !isInt2 {
					allInt = false
				}
			}
		}
		if allInt {
			total = int64(sum)
		} else {
			total = sum
		}
	}
	average := number(mget(data, "rating_star"))
	avg := average
	if f, ok := average.(float64); ok {
		avg = round2(f)
	}
	var usernameLink any
	if username != nil {
		usernameLink = shopUsernameLink(country, username.(string))
	}
	var chatEnabled any
	if b, ok := mget(data, "chat_disabled").(bool); ok {
		chatEnabled = !b
	}
	countryValue := text(mget(data, "country"))
	if countryValue == nil {
		countryValue = strings.ToUpper(country)
	}
	return omap.From(
		"id", shopID,
		"name", text(mget(data, "name")),
		"username", username,
		"link", shopLink(country, shopID),
		"username_link", usernameLink,
		"description", text(mget(data, "description")),
		"country", countryValue,
		"location", text(mget(data, "shop_location")),
		"avatar", image(country, mget(account, "portrait")),
		"cover_image", image(country, mget(data, "cover")),
		"is_official_shop", flagValue(firstNN(mget(data, "is_official_shop"), mget(data, "show_official_shop_label"))),
		"is_shopee_verified", flagValue(mget(data, "is_shopee_verified")),
		"is_preferred_plus_seller", flagValue(mget(data, "is_preferred_plus_seller")),
		"is_on_vacation", flagValue(mget(data, "vacation")),
		"is_chat_enabled", chatEnabled,
		"items_count", number(mget(data, "item_count")),
		"followers_count", number(mget(data, "follower_count")),
		"following_count", number(mget(account, "following_count")),
		"rating", omap.From(
			"average", avg,
			"count", total,
			"good", good, "normal", normal, "bad", bad,
		),
		"response_rate_percent", number(mget(data, "response_rate")),
		"response_time_seconds", number(mget(data, "response_time")),
		"preparation_time_seconds", number(mget(data, "preparation_time")),
		"cancellation_rate_percent", number(mget(data, "cancellation_rate")),
		"has_flash_sale", flagValue(mget(data, "has_flash_sale")),
		"has_brand_sale", flagValue(mget(data, "has_brand_sale")),
		"has_live_stream", flagValue(firstNN(mget(data, "show_live_tab"), mget(tabs, "show_live_tab"))),
		"has_membership", flagValue(firstNN(mget(data, "show_membership_tab"), mget(membership, "show_membership_tab"))),
		"latest_item_listed_at", ts(firstNN(mget(data, "latest_item_ctime"), mget(newArrivals, "latest_item_ctime"))),
		"joined_at", ts(mget(data, "ctime")),
		"last_active_at", ts(mget(data, "last_active_time")),
		"updated_at", ts(mget(data, "mtime")),
		"vouchers", []any{},
	)
}

// parseShopCategories is parsers.parse_shop_categories.
func parseShopCategories(payload any, country string, shopID any) ([]any, any, any) {
	root := asM(payload)
	data := asM(mget(root, "data"))
	rows := make([]any, 0)
	for _, raw := range asSlice(mget(data, "shop_categories")) {
		cat := asM(raw)
		if cat == nil {
			continue
		}
		catID := mget(cat, "shop_category_id")
		rows = append(rows, omap.From(
			"id", catID,
			"name", text(mget(cat, "display_name")),
			"link", fmt.Sprintf("%s?shopCollection=%v", shopLink(country, shopID), catID),
			"image", image(country, mget(cat, "image")),
			"items_count", number(mget(cat, "total")),
			"is_auto_generated", flagValue(mget(cat, "is_generated")),
			"is_adult", flagValue(mget(cat, "is_adult")),
		))
	}
	return rows, number(mget(data, "total")), flagValue(mget(data, "has_more"))
}

func reviewProduct(country string, raw any) any {
	m := asM(raw)
	if m == nil {
		return nil
	}
	itemID := mget(m, "itemid")
	shopID := mget(m, "shopid")
	var link any
	if isTruthy(itemID) && isTruthy(shopID) {
		link = productLink(country, shopID, itemID)
	}
	return omap.From(
		"id", itemID, "shop_id", shopID, "name", text(mget(m, "name")),
		"link", link, "image", image(country, mget(m, "image")),
		"model_id", orNil(mget(m, "modelid")), "variation", text(mget(m, "model_name")),
	)
}

func replyOf(raw any) any {
	m := asM(raw)
	if m == nil {
		return nil
	}
	comment := text(mget(m, "comment"))
	if comment == nil {
		return nil
	}
	return omap.From("comment", comment, "created_at", ts(mget(m, "ctime")))
}

// parseShopReviews is parsers.parse_shop_reviews.
func parseShopReviews(payload any, country string) []any {
	root := asM(payload)
	data := asM(mget(root, "data"))
	rows := make([]any, 0)
	for _, raw := range asSlice(mget(data, "items")) {
		review := asM(raw)
		if review == nil {
			continue
		}
		products := asSlice(mget(review, "product_items"))
		tagValues := asSlice(mget(review, "template_tags"))
		if len(tagValues) == 0 {
			tagValues = asSlice(mget(review, "tags"))
		}
		tags := make([]any, 0)
		for _, t := range tagValues {
			var tagged any
			if tm := asM(t); tm != nil {
				tagged = text(firstTruthy(mget(tm, "tag_description"), mget(tm, "text"), mget(tm, "name")))
			} else {
				tagged = text(t)
			}
			if tagged != nil {
				tags = append(tags, tagged)
			}
		}
		var detailed any
		if dr := asM(mget(review, "detailed_rating")); dr != nil {
			detailed = omap.New()
			for _, key := range dr.Keys() {
				detailed.(*omap.M).Set(key, number(mget(dr, key)))
			}
		}
		var product any
		if len(products) > 0 {
			product = reviewProduct(country, products[0])
		}
		videos := make([]any, 0)
		for _, v := range asSlice(mget(review, "videos")) {
			if !isTruthy(v) {
				continue
			}
			if vm := asM(v); vm != nil {
				videos = append(videos, mget(vm, "url"))
			} else {
				videos = append(videos, v)
			}
		}
		rows = append(rows, omap.From(
			"id", mget(review, "cmtid"),
			"rating", number(mget(review, "rating_star")),
			"comment", text(mget(review, "comment")),
			"detailed_rating", detailed,
			"author", omap.From(
				"username", text(mget(review, "author_username")),
				"avatar", image(country, mget(review, "author_portrait")),
				"is_anonymous", flagValue(mget(review, "anonymous")),
			),
			"product", product,
			"images", images(country, mget(review, "images")),
			"videos", videos,
			"tags", tags,
			"likes_count", number(mget(review, "like_count")),
			"is_repeat_purchase", flagValue(mget(review, "is_repeated_purchase")),
			"seller_reply", replyOf(mget(review, "ItemRatingReply")),
			"created_at", ts(mget(review, "ctime")),
			"updated_at", ts(mget(review, "mtime")),
		))
	}
	return rows
}

var REWARD_TYPES = map[int64]string{0: "percentage", 1: "fixed_amount", 2: "coins"}

// parseVouchers is parsers.parse_vouchers.
func parseVouchers(payload any) []any {
	root := asM(payload)
	data := asM(mget(root, "data"))
	rows := make([]any, 0)
	for _, raw := range asSlice(mget(data, "voucher_list")) {
		v := asM(raw)
		if v == nil {
			continue
		}
		var percentage any
		if p := number(mget(v, "reward_percentage")); isTruthy(p) {
			percentage = p
		} else if p := number(mget(v, "discount_percentage")); isTruthy(p) {
			percentage = p
		} else if p := number(mget(v, "reward_percentage_with_decimal")); isTruthy(p) {
			if f, ok := toFloat(p); ok {
				percentage = round2(f / MONEY_UNIT)
			}
		}
		var rewardType any
		if n, ok := toInt64(mget(v, "reward_type")); ok {
			rewardType = REWARD_TYPES[n]
		} else {
			rewardType = text(mget(v, "reward_type"))
		}
		discountAmount := money(mget(v, "reward_value"))
		if !isTruthy(discountAmount) {
			discountAmount = money(mget(v, "discount_value"))
		}
		discountCap := money(mget(v, "reward_cap"))
		if !isTruthy(discountCap) {
			discountCap = money(mget(v, "discount_cap"))
		}
		coinsPercent := number(mget(v, "coin_percentage"))
		if !isTruthy(coinsPercent) {
			coinsPercent = nil
		}
		rows = append(rows, omap.From(
			"id", mget(v, "promotionid"),
			"code", text(mget(v, "voucher_code")),
			"title", text(mget(v, "title")),
			"description", text(mget(v, "description")),
			"reward_type", rewardType,
			"discount_percent", percentage,
			"discount_amount", discountAmount,
			"discount_cap", discountCap,
			"coins_percent", coinsPercent,
			"min_spend", money(mget(v, "min_spend")),
			"starts_at", ts(mget(v, "start_time")),
			"ends_at", ts(mget(v, "end_time")),
			"claim_starts_at", ts(mget(v, "claim_start_time")),
			"claim_ends_at", ts(mget(v, "claim_end_time")),
			"usage_limit", number(mget(v, "usage_limit")),
			"used_count", number(mget(v, "used_count")),
			"left_count", number(mget(v, "left_count")),
			"percentage_claimed", number(mget(v, "percentage_claimed")),
			"percentage_used", number(mget(v, "percentage_used")),
			"is_new_user_only", flagValue(mget(v, "new_user_only")),
			"is_fully_claimed", flagValue(mget(v, "fully_claimed")),
			"is_expired", flagValue(mget(v, "has_expired")),
		))
	}
	return rows
}

// parseOfficialShops is parsers.parse_official_shops.
func parseOfficialShops(payload any, country string) ([]any, any) {
	root := asM(payload)
	data := asM(mget(root, "data"))
	rows := make([]any, 0)
	for _, raw := range asSlice(mget(data, "official_shops")) {
		shop := asM(raw)
		if shop == nil {
			continue
		}
		shopID := mget(shop, "shopid")
		username := text(mget(shop, "username"))
		rows = append(rows, omap.From(
			"id", shopID,
			"name", text(mget(shop, "shop_name")),
			"username", username,
			"link", shopLink(country, shopID),
			"brand_name", text(mget(shop, "brand_name")),
			"logo", image(country, firstNN(mget(shop, "logo_pc"), mget(shop, "logo"))),
			"joined_at", ts(mget(shop, "ctime")),
		))
	}
	return rows, number(mget(data, "total"))
}
