package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/internal/omap"
)

// serveCountry points one market at an httptest server. site() is unchanged,
// so link output and language/referer headers still use the real Shopee host.
func serveCountry(t *testing.T, country string, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	oldBase, hadBase := baseURLs[country]
	oldRetries := maxRetries
	baseURLs[country] = srv.URL
	maxRetries = 1
	t.Cleanup(func() {
		if hadBase {
			baseURLs[country] = oldBase
		} else {
			delete(baseURLs, country)
		}
		maxRetries = oldRetries
		srv.Close()
	})
}

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("shopee", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSearchAutocompleteHTTP(t *testing.T) {
	serveCountry(t, "br", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/search/search_hint" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		eq(t, q.Get("keyword"), "iphone")
		eq(t, q.Get("search_type"), "0")
		eq(t, q.Get("version"), "1")
		eq(t, r.Header.Get("accept-language"), ACCEPT_LANGUAGE_OF["br"])
		eq(t, r.Header.Get("referer"), "https://shopee.com.br/")
		eq(t, r.Header.Get("x-api-source"), "pc")
		eq(t, r.Header.Get("x-requested-with"), "XMLHttpRequest")
		_, _ = w.Write(fixtureBytes(t, "search_hint_iphone.json"))
	})
	got, err := apiSearchAutocomplete("br", "iphone")
	if err != nil {
		t.Fatal(err)
	}
	m := asM(got)
	eq(t, val(t, m, "country"), "br")
	eq(t, val(t, m, "query"), "iphone")
	eq(t, val(t, m, "count"), 12)
	if len(asSlice(val(t, m, "results"))) != 12 {
		t.Fatal("12 results")
	}
}

func TestCategoriesListHTTP(t *testing.T) {
	serveCountry(t, "sg", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/pages/get_category_tree" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write(fixtureBytes(t, "category_tree_sg.json"))
	})
	got, err := apiCategoriesList("sg")
	if err != nil {
		t.Fatal(err)
	}
	m := asM(got)
	eq(t, val(t, m, "count"), 27)
	if len(asSlice(val(t, m, "results"))) != 27 {
		t.Fatal("27 categories")
	}
}

func TestShopDetailsHTTP(t *testing.T) {
	serveCountry(t, "sg", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/shop/get_shop_base":
			eq(t, r.URL.Query().Get("username"), "lorealparissg")
			eq(t, r.Header.Get("x-api-source"), "rn")
			_, _ = w.Write(fixtureBytes(t, "shop_base_loreal.json"))
		case "/api/v4/voucher_wallet/get_shop_vouchers_by_shopid":
			eq(t, r.URL.Query().Get("shopid"), "15235546")
			_, _ = w.Write(fixtureBytes(t, "shop_vouchers_loreal.json"))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})
	got, err := apiShopDetails("sg", "lorealparissg")
	if err != nil {
		t.Fatal(err)
	}
	m := asM(got)
	eq(t, val(t, m, "id"), 15235546)
	if len(asSlice(val(t, m, "vouchers"))) != 9 {
		t.Fatal("9 vouchers")
	}
}

func TestProductDetailsHTTP(t *testing.T) {
	serveCountry(t, "sg", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/product/"):
			eq(t, r.URL.Path, "/product/15235546/23326616342")
			eq(t, r.Header.Get("x-requested-with"), "XMLHttpRequest")
			_, _ = w.Write(fixtureBytes(t, "product_loreal_sg_trimmed.html"))
		case r.URL.Path == "/api/v4/shop/get_shop_base":
			_, _ = w.Write(fixtureBytes(t, "shop_base_loreal.json"))
		case r.URL.Path == "/api/v4/voucher_wallet/get_shop_vouchers_by_shopid":
			_, _ = w.Write(fixtureBytes(t, "shop_vouchers_loreal.json"))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})
	got, err := apiProductDetails("sg", "15235546.23326616342")
	if err != nil {
		t.Fatal(err)
	}
	m := asM(got)
	eq(t, val(t, m, "id"), 23326616342)
	hasPrefix(t, val(t, m, "name"), "L'Oreal Paris Elseve Fall Resist")
	shop := asM(val(t, m, "shop"))
	eq(t, val(t, shop, "id"), 15235546)
	eq(t, val(t, shop, "name"), "L'Oreal Paris Official Store")
	if _, ok := shop.Get("vouchers"); ok {
		t.Fatal("product shop block must not contain vouchers")
	}
}

func TestProductDetailsNotFoundHTTP(t *testing.T) {
	serveCountry(t, "sg", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixtureBytes(t, "product_unknown_sg_trimmed.html"))
	})
	_, err := apiProductDetails("sg", "15235546.23326616342")
	want := "product 15235546/23326616342 not found on shopee.sg"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestDealsFlashSaleHTTP(t *testing.T) {
	serveCountry(t, "sg", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/flash_sale/get_all_sessions":
			_, _ = w.Write(fixtureBytes(t, "flash_sessions_sg.json"))
		case "/api/v4/flash_sale/flash_sale_get_items":
			q := r.URL.Query()
			eq(t, q.Get("promotionid"), "513667598795255")
			eq(t, q.Get("offset"), "0")
			eq(t, q.Get("limit"), "50")
			eq(t, q.Get("need_personalize"), "true")
			eq(t, q.Get("sort_soldout"), "true")
			eq(t, q.Get("with_dp_items"), "true")
			if q.Get("categoryid") != "" {
				t.Errorf("unexpected categoryid %q", q.Get("categoryid"))
			}
			_, _ = w.Write(fixtureBytes(t, "flash_items_sg.json"))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})
	got, err := apiDealsFlashSale("sg", nil, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	m := asM(got)
	if len(asSlice(val(t, m, "results"))) != 16 {
		t.Fatal("16 deals")
	}
	session := asM(val(t, m, "session"))
	eq(t, val(t, session, "id"), 513667598795255)
	if len(asSlice(val(t, m, "sessions"))) != 4 {
		t.Fatal("4 sessions")
	}
}

func TestHTTPErrorMapping(t *testing.T) {
	t.Run("not found errors", func(t *testing.T) {
		serveCountry(t, "sg", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"error":1000000,"error_msg":"service_err"}`))
		})
		_, err := apiShopDetails("sg", "doesnotexist")
		want := "shop doesnotexist not found"
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	})
	t.Run("anti fraud", func(t *testing.T) {
		serveCountry(t, "sg", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"error":90309999}`))
		})
		_, err := apiSearchTrending("sg")
		want := "anti-fraud reply (90309999) for trending searches"
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	})
	t.Run("status", func(t *testing.T) {
		serveCountry(t, "sg", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
		_, err := apiCategoriesList("sg")
		want := "HTTP 403 for category tree"
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	})
}

func TestPaginationBlock(t *testing.T) {
	withTotal := pagination(2, 20, 55, nil)
	eq(t, val(t, withTotal, "page"), 2)
	eq(t, val(t, withTotal, "total_count"), 55)
	eq(t, val(t, withTotal, "total_pages"), 3)
	keys := strings.Join(withTotal.Keys(), ",")
	eq(t, keys, "page,items_per_page,total_count,total_pages")

	without := pagination(1, 20, nil, true)
	keys = strings.Join(without.Keys(), ",")
	eq(t, keys, "page,items_per_page,has_next,total_pages,total_count")
	eq(t, val(t, without, "has_next"), true)
	eq(t, val(t, without, "total_pages"), nil)

	zero := pagination(1, 20, 0, nil)
	eq(t, val(t, zero, "total_pages"), 0)
	_ = omap.New()
}
