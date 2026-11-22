package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Request-schema constants from shopee/schemas.py.
const (
	REVIEWS_PER_PAGE         = 20
	FLASH_SALE_PER_PAGE      = 50
	OFFICIAL_SHOPS_PER_PAGE  = 20
	SHOP_CATEGORIES_PER_PAGE = 50
)

// validationErrors mirrors marshmallow's normalized messages: field -> list
// of messages. The tests only need the field names and a stable Error string.
type validationErrors map[string][]string

func (e validationErrors) Error() string {
	keys := make([]string, 0, len(e))
	for k := range e {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+strings.Join(e[k], " "))
	}
	return strings.Join(parts, "; ")
}

func (e validationErrors) add(field, message string) {
	e[field] = append(e[field], message)
}

type fieldKind int

const (
	fieldCountry fieldKind = iota
	fieldProductRef
	fieldShopRef
	fieldPage
	fieldPositiveInt
	fieldQuery
	fieldRating
)

type fieldSpec struct {
	name     string
	kind     fieldKind
	required bool
	def      any
	max      int64
}

// strippedString is shared by StrippedString / QueryField / ChoiceField.
func strippedString(v any) string {
	s, _ := v.(string)
	return strings.Join(strings.Fields(s), " ")
}

func parseIntegerField(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		return int64(x), true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return i, err == nil
	}
	return 0, false
}

// loadSchema validates raw against specs, applies each field's load_default,
// rejects unknown keys and runs the MarketSchema post_load hook.
func loadSchema(raw map[string]any, specs []fieldSpec) (map[string]any, validationErrors) {
	errs := validationErrors{}
	data := make(map[string]any)
	known := make(map[string]bool, len(specs))
	for _, sp := range specs {
		known[sp.name] = true
		v, present := raw[sp.name]
		if !present || v == nil {
			if sp.required {
				errs.add(sp.name, "Missing data for required field.")
			} else {
				data[sp.name] = sp.def
			}
			continue
		}
		switch sp.kind {
		case fieldCountry:
			s := strings.ToLower(strippedString(v))
			valid := false
			for _, cc := range MARKETS {
				if s == cc {
					valid = true
					break
				}
			}
			if !valid {
				errs.add(sp.name, "Must be one of: "+marketsJoined()+".")
			} else {
				data[sp.name] = s
			}
		case fieldProductRef, fieldShopRef:
			s := strippedString(v)
			if s == "" {
				errs.add(sp.name, "Must not be empty.")
				continue
			}
			if sp.kind == fieldProductRef {
				ref, err := resolveProduct(s)
				if err != nil {
					errs.add(sp.name, err.Error())
				} else {
					data[sp.name] = ref
				}
			} else {
				ref, err := resolveShop(s)
				if err != nil {
					errs.add(sp.name, err.Error())
				} else {
					data[sp.name] = ref
				}
			}
		case fieldPage:
			i, ok := parseIntegerField(v)
			if !ok {
				errs.add(sp.name, "Not a valid integer.")
			} else if i < 1 || i > sp.max {
				errs.add(sp.name, fmt.Sprintf("Must be greater than or equal to 1 and less than or equal to %d.", sp.max))
			} else {
				data[sp.name] = i
			}
		case fieldPositiveInt:
			i, ok := parseIntegerField(v)
			if !ok {
				errs.add(sp.name, "Not a valid integer.")
			} else if i < 1 {
				errs.add(sp.name, "Must be greater than or equal to 1.")
			} else {
				data[sp.name] = i
			}
		case fieldQuery:
			s := strippedString(v)
			if s == "" {
				errs.add(sp.name, "Must not be empty.")
			} else if len([]rune(s)) > int(sp.max) {
				errs.add(sp.name, fmt.Sprintf("Longer than maximum length %d.", sp.max))
			} else {
				data[sp.name] = s
			}
		case fieldRating:
			s := strings.ToLower(strippedString(v))
			if s != "5" && s != "4" && s != "3" && s != "2" && s != "1" {
				errs.add(sp.name, "Must be one of: 5, 4, 3, 2, 1.")
			} else {
				data[sp.name] = s
			}
		}
	}
	for k := range raw {
		if !known[k] {
			errs.add(k, "Unknown field.")
		}
	}

	// MarketSchema._resolve_market
	country, _ := data["country"].(string)
	if ref, ok := data["product"].(ProductRef); ok {
		if ref.Country != nil {
			country = *ref.Country
		}
		data["product"] = fmt.Sprintf("%d.%d", ref.ShopID, ref.ItemID)
	}
	if ref, ok := data["shop"].(ShopRef); ok {
		if ref.Country != nil {
			country = *ref.Country
		}
		if ref.ShopID != nil {
			data["shop"] = strconv.FormatInt(*ref.ShopID, 10)
		} else if ref.Username != nil {
			data["shop"] = *ref.Username
		}
	}
	if country == "" {
		country = DEFAULT_MARKET
	}
	data["country"] = country

	// ShopReviewsSchema._rating_int
	if rating, ok := data["rating"].(string); ok {
		if i, err := strconv.Atoi(rating); err == nil {
			data["rating"] = i
		}
	}
	return data, errs
}

var (
	countrySpec = fieldSpec{name: "country", kind: fieldCountry, def: DEFAULT_MARKET}
	productSpec = fieldSpec{name: "product", kind: fieldProductRef, required: true}
	shopSpec    = fieldSpec{name: "shop", kind: fieldShopRef, required: true}
)

func loadProductDetails(raw map[string]any) (map[string]any, validationErrors) {
	return loadSchema(raw, []fieldSpec{countrySpec, productSpec})
}

func loadShop(raw map[string]any) (map[string]any, validationErrors) {
	return loadSchema(raw, []fieldSpec{countrySpec, shopSpec})
}

func loadShopCategories(raw map[string]any) (map[string]any, validationErrors) {
	return loadSchema(raw, []fieldSpec{countrySpec, shopSpec,
		{name: "page", kind: fieldPage, def: int64(1), max: 50},
	})
}

func loadShopReviews(raw map[string]any) (map[string]any, validationErrors) {
	return loadSchema(raw, []fieldSpec{countrySpec, shopSpec,
		{name: "rating", kind: fieldRating, def: "5"},
		{name: "page", kind: fieldPage, def: int64(1), max: 500},
	})
}

func loadCategoriesList(raw map[string]any) (map[string]any, validationErrors) {
	return loadSchema(raw, []fieldSpec{countrySpec})
}

func loadTrendingSearches(raw map[string]any) (map[string]any, validationErrors) {
	return loadSchema(raw, []fieldSpec{countrySpec})
}

func loadTrendingProducts(raw map[string]any) (map[string]any, validationErrors) {
	return loadSchema(raw, []fieldSpec{countrySpec})
}

func loadOfficialShops(raw map[string]any) (map[string]any, validationErrors) {
	return loadSchema(raw, []fieldSpec{countrySpec,
		{name: "page", kind: fieldPage, def: int64(1), max: 200},
	})
}

func loadAutocomplete(raw map[string]any) (map[string]any, validationErrors) {
	return loadSchema(raw, []fieldSpec{countrySpec,
		{name: "query", kind: fieldQuery, required: true, max: 100},
	})
}

func loadFlashSale(raw map[string]any) (map[string]any, validationErrors) {
	return loadSchema(raw, []fieldSpec{countrySpec,
		{name: "session", kind: fieldPositiveInt, def: nil},
		{name: "category", kind: fieldPositiveInt, def: nil},
		{name: "page", kind: fieldPage, def: int64(1), max: 50},
	})
}
