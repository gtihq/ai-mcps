package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// DEFAULT_MARKET is refs.DEFAULT_MARKET. The MCP tools override it with the
// country argument (default "br"); the request schemas use this value.
const DEFAULT_MARKET = "sg"

// SITE_OF is refs.SITE_OF: country code -> site host. Order matters: it is
// refs.MARKETS and the order of every "must be one of" error message.
var SITE_OF = []struct{ Country, Host string }{
	{"sg", "shopee.sg"},
	{"my", "shopee.com.my"},
	{"th", "shopee.co.th"},
	{"vn", "shopee.vn"},
	{"ph", "shopee.ph"},
	{"id", "shopee.co.id"},
	{"tw", "shopee.tw"},
	{"br", "shopee.com.br"},
	{"mx", "shopee.com.mx"},
}

// MARKETS is refs.MARKETS, in the same order as SITE_OF.
var MARKETS = marketCodes()

// CURRENCY_OF is refs.CURRENCY_OF.
var CURRENCY_OF = map[string]string{
	"sg": "SGD", "my": "MYR", "th": "THB", "vn": "VND", "ph": "PHP",
	"id": "IDR", "tw": "TWD", "br": "BRL", "mx": "MXN",
}

// ACCEPT_LANGUAGE_OF is refs.ACCEPT_LANGUAGE_OF.
var ACCEPT_LANGUAGE_OF = map[string]string{
	"sg": "en-SG,en-GB;q=0.9,en-US;q=0.8,en;q=0.7",
	"my": "ms-MY,ms;q=0.9,en-MY;q=0.8,en;q=0.7",
	"th": "th-TH,th;q=0.9,en-US;q=0.8,en;q=0.7",
	"vn": "vi-VN,vi;q=0.9,en-US;q=0.8,en;q=0.7",
	"ph": "en-PH,en;q=0.9,fil;q=0.8",
	"id": "id-ID,id;q=0.9,en-US;q=0.8,en;q=0.7",
	"tw": "zh-TW,zh;q=0.9,en-US;q=0.8,en;q=0.7",
	"br": "pt-BR,pt;q=0.9,en-US;q=0.8,en;q=0.7",
	"mx": "es-MX,es;q=0.9,en-US;q=0.8,en;q=0.7",
}

// IMAGE_HOST_OF is refs.IMAGE_HOST_OF.
var IMAGE_HOST_OF = imageHosts()

// ProductRef is refs.ProductRef. Country is nil when the input was a bare id.
type ProductRef struct {
	ShopID  int64
	ItemID  int64
	Country *string
}

// ShopRef is refs.ShopRef. One of ShopID / Username is set; Country is nil for
// a bare id or username.
type ShopRef struct {
	ShopID   *int64
	Username *string
	Country  *string
}

var (
	pairRE         = regexp.MustCompile(`^(\d{1,12})[./](\d{1,15})$`)
	linkProductRes = []*regexp.Regexp{
		regexp.MustCompile(`/product/(\d{1,12})/(\d{1,15})(?:[/?#]|$)`),
		regexp.MustCompile(`-i\.(\d{1,12})\.(\d{1,15})(?:[/?#]|$)`),
	}
	linkShopIDRE  = regexp.MustCompile(`^/shop/(\d{1,12})(?:[/?#]|$)`)
	usernameRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._]{1,48}$`)
	reservedPaths = map[string]bool{
		"product": true, "shop": true, "search": true, "cart": true,
		"checkout": true, "user": true, "buyer": true, "mall": true, "m": true,
		"api": true, "verify": true, "universal-link": true, "flash_sale": true,
		"daily_discover": true, "top_products": true, "find_similar_products": true,
		"blog": true, "seller": true, "events3": true, "events": true,
		"digital-product": true, "food": true, "video": true, "live": true,
		"official-shop": true,
	}
)

func marketCodes() []string {
	out := make([]string, 0, len(SITE_OF))
	for _, s := range SITE_OF {
		out = append(out, s.Country)
	}
	return out
}

func imageHosts() map[string]string {
	out := make(map[string]string, len(SITE_OF))
	for _, s := range SITE_OF {
		out[s.Country] = "https://down-" + s.Country + ".img.susercontent.com/file/"
	}
	return out
}

func hostOf(country string) string {
	for _, s := range SITE_OF {
		if s.Country == country {
			return s.Host
		}
	}
	return ""
}

func countryOfHost(host string) (string, bool) {
	for _, s := range SITE_OF {
		if s.Host == host {
			return s.Country, true
		}
	}
	return "", false
}

func marketsJoined() string { return strings.Join(MARKETS, ", ") }

func hostsJoined() string {
	hosts := make([]string, 0, len(SITE_OF))
	for _, s := range SITE_OF {
		hosts = append(hosts, s.Host)
	}
	return strings.Join(hosts, ", ")
}

// site is refs.site.
func site(country string) string { return "https://" + hostOf(country) }

// parseLink is refs._parse_link: (host, path, url) for an http(s) or
// protocol-relative link, else nil.
func parseLink(value string) (host, path, full string, ok bool) {
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	}
	low := strings.ToLower(value)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return "", "", "", false
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", "", "", false
	}
	p := u.Path
	if p == "" {
		p = "/"
	}
	return strings.ToLower(u.Hostname()), p, value, true
}

// marketOfHost is refs._market_of_host.
func marketOfHost(host string) (*string, error) {
	labels := strings.Split(host, ".")
	for i := range labels {
		candidate := strings.Join(labels[i:], ".")
		if cc, found := countryOfHost(candidate); found {
			return &cc, nil
		}
	}
	hasShopee := false
	for _, label := range labels {
		if label == "shopee" {
			hasShopee = true
			break
		}
	}
	if hasShopee {
		return nil, fmt.Errorf("Shopee market %s is not supported; use one of: %s.", host, hostsJoined())
	}
	return nil, fmt.Errorf("Must be a Shopee link.")
}

func ptrString(s string) *string { return &s }
func ptrInt64(i int64) *int64    { return &i }

// resolveProduct is refs.resolve_product.
func resolveProduct(value string) (ProductRef, error) {
	value = strings.TrimSpace(value)
	if m := pairRE.FindStringSubmatch(value); m != nil {
		return ProductRef{ShopID: mustParseInt(m[1]), ItemID: mustParseInt(m[2])}, nil
	}
	if host, _, full, ok := parseLink(value); ok {
		country, err := marketOfHost(host)
		if err != nil {
			return ProductRef{}, err
		}
		for _, re := range linkProductRes {
			if m := re.FindStringSubmatch(full); m != nil {
				return ProductRef{ShopID: mustParseInt(m[1]), ItemID: mustParseInt(m[2]), Country: country}, nil
			}
		}
		return ProductRef{}, fmt.Errorf("Must be a Shopee product link " +
			"(https://shopee.sg/product/<shop_id>/<item_id> or " +
			"https://shopee.sg/<name>-i.<shop_id>.<item_id>).")
	}
	return ProductRef{}, fmt.Errorf("Must be '<shop_id>.<item_id>' (e.g. 15235546.23326616342) " +
		"or a Shopee product link.")
}

// resolveShop is refs.resolve_shop.
func resolveShop(value string) (ShopRef, error) {
	value = strings.TrimSpace(value)
	if isDigits(value) {
		return ShopRef{ShopID: ptrInt64(mustParseInt(value))}, nil
	}
	if host, path, _, ok := parseLink(value); ok {
		country, err := marketOfHost(host)
		if err != nil {
			return ShopRef{}, err
		}
		if m := linkShopIDRE.FindStringSubmatch(path); m != nil {
			return ShopRef{ShopID: ptrInt64(mustParseInt(m[1])), Country: country}, nil
		}
		segments := make([]string, 0)
		for _, s := range strings.Split(path, "/") {
			if s != "" {
				segments = append(segments, s)
			}
		}
		if len(segments) == 1 && !reservedPaths[strings.ToLower(segments[0])] && usernameRE.MatchString(segments[0]) {
			return ShopRef{Username: ptrString(strings.ToLower(segments[0])), Country: country}, nil
		}
		return ShopRef{}, fmt.Errorf("Must be a Shopee shop link " +
			"(https://shopee.sg/shop/<shop_id> or https://shopee.sg/<username>).")
	}
	if usernameRE.MatchString(value) && !strings.Contains(value, "/") {
		return ShopRef{Username: ptrString(strings.ToLower(value))}, nil
	}
	return ShopRef{}, fmt.Errorf("Must be a Shopee shop id (15235546), a username (lorealparissg) " +
		"or a Shopee shop link.")
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func mustParseInt(s string) int64 {
	var n int64
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

// productLink is refs.product_link.
func productLink(country string, shopID, itemID any) string {
	return fmt.Sprintf("%s/product/%v/%v", site(country), shopID, itemID)
}

// shopLink is refs.shop_link.
func shopLink(country string, shopID any) string {
	return fmt.Sprintf("%s/shop/%v", site(country), shopID)
}

// shopUsernameLink is refs.shop_username_link.
func shopUsernameLink(country, username string) string {
	return site(country) + "/" + username
}

var categorySlugRE = regexp.MustCompile(`[\s/&,.'"()\[\]+:;!?]+`)

// urlQuote is urllib.parse.quote with the given safe characters. Unreserved
// characters are always kept; everything else is percent encoded per UTF-8
// byte with upper-case hex.
func urlQuote(value, safe string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '_' || c == '.' || c == '-' || c == '~' || strings.ContainsRune(safe, rune(c)) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// searchLink is refs.search_link.
func searchLink(country, keyword string) string {
	return site(country) + "/search?keyword=" + urlQuote(keyword, "/")
}

// categoryLink is refs.category_link.
func categoryLink(country, name string, categoryID any) string {
	slug := strings.Trim(categorySlugRE.ReplaceAllString(name, "-"), "-")
	if slug == "" {
		slug = "cat"
	}
	return fmt.Sprintf("%s/%s-cat.%v", site(country), urlQuote(slug, "-"), categoryID)
}

// imageLink is refs.image_link.
func imageLink(country string, value any) any {
	if value == nil {
		return nil
	}
	s := fmt.Sprint(value)
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s
	}
	host, ok := IMAGE_HOST_OF[country]
	if !ok {
		host = IMAGE_HOST_OF[DEFAULT_MARKET]
	}
	return host + s
}
