package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gtihq/ai-mcps/internal/omap"
	playwright "github.com/mxschmitt/playwright-go"
)

const (
	searchSelector    = "li.ui-search-layout__item"
	navTimeoutMS      = 60000.0
	selectorTimeoutMS = 25000.0
	loginPollMS       = 3000.0
	loginSettleMS     = 5000.0
	ordersSettleMS    = 10000.0
	loginTimeout      = 10 * time.Minute
	profileDirName    = ".browser-profile"
	cookieFileName    = "cookies.json"
	loginWaitMessage  = "aguardando login... (%ds)   \r"
)

// extractJS is the original Python _EXTRACT_JS, copied verbatim. A raw string
// keeps the regular expressions (\d, \s, \.) exactly as they were written.
const extractJS = `(items) => items.map(el => {
  const a = el.querySelector('h3.poly-component__title-wrapper a, a.poly-component__title');
  const img = el.querySelector('img.poly-component__picture');
  const title = ((a && a.textContent) || (img && img.alt) || '').trim();
  const href = a ? a.href : '';
  let url = href;
  let item_id = null;
  const m = href.match(/item_id(?:%3A|:)(ML[AB]\d+)/i);
  if (m) { item_id = m[1]; url = 'https://www.mercadolivre.com.br/p/' + m[1]; }
  const frac = el.querySelector('.andes-money-amount__fraction');
  const cents = el.querySelector('.andes-money-amount__cents');
  let price = null;
  if (frac) {
    const f = frac.textContent.replace(/\./g, '').replace(/\D/g, '');
    const c = cents ? cents.textContent.trim().replace(/\D/g, '') : '';
    price = c ? parseFloat(f + '.' + c) : parseInt(f || '0', 10);
  }
  const prev = el.querySelector('.andes-money-amount--previous .andes-money-amount__fraction');
  let oldPrice = null;
  if (prev) {
    const f = prev.textContent.replace(/\./g, '').replace(/\D/g, '');
    const pc = el.querySelector('.andes-money-amount--previous .andes-money-amount__cents');
    const cc = pc ? pc.textContent.trim().replace(/\D/g, '') : '';
    oldPrice = cc ? parseFloat(f + '.' + cc) : parseInt(f || '0', 10);
  }
  const rating = (el.querySelector('.poly-component__review-compacted .polylabel-label') || {}).textContent || null;
  const seller = (el.querySelector('.poly-component__seller') || {}).textContent || null;
  const shipEl = el.querySelector('.poly-component__shipping-v2, .poly-component__shipping');
  const shipping = shipEl ? shipEl.textContent.trim().replace(/\s+/g, ' ') : null;
  return { title, item_id, url, price, old_price: oldPrice, rating: rating ? rating.trim() : null, seller: seller ? seller.trim() : null, shipping };
})
`

// resultKeys is the property order of the object returned by extractJS. Go
// maps do not keep order, so results are rebuilt as ordered objects.
var resultKeys = []string{"title", "item_id", "url", "price", "old_price", "rating", "seller", "shipping"}

var (
	// profileDirOverride lets tests point list_orders at a missing directory
	// without touching the real profile next to the executable.
	profileDirOverride string

	pageMu      sync.Mutex
	pageInst    playwright.Page
	browserInst playwright.Browser
	pwInst      *playwright.Playwright
)

// isHeadless mirrors ML_HEADLESS: "0" (default) is a visible browser.
func isHeadless() bool {
	return os.Getenv("ML_HEADLESS") == "1"
}

// browserBaseDir is the directory beside the executable, where the persistent
// profile and cookies.json live. os.Executable is used instead of Python's
// __file__ so a compiled binary finds its own profile.
func browserBaseDir() string {
	if profileDirOverride != "" {
		return filepath.Dir(profileDirOverride)
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// browserProfileDir is browserBaseDir/.browser-profile.
func browserProfileDir() string {
	if profileDirOverride != "" {
		return profileDirOverride
	}
	return filepath.Join(browserBaseDir(), profileDirName)
}

func contextOptions() playwright.BrowserTypeLaunchPersistentContextOptions {
	return playwright.BrowserTypeLaunchPersistentContextOptions{
		Locale:     playwright.String("pt-BR"),
		TimezoneId: playwright.String("America/Sao_Paulo"),
		Viewport:   &playwright.Size{Width: 1366, Height: 900},
	}
}

// getPage mirrors _get_page: one Chromium instance, one context and one page,
// created lazily and reused for every search.
func getPage() (playwright.Page, error) {
	pageMu.Lock()
	defer pageMu.Unlock()
	if pageInst != nil {
		return pageInst, nil
	}

	pw, err := playwright.Run()
	if err != nil {
		return nil, err
	}
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Channel:  playwright.String("chrome"),
		Headless: playwright.Bool(isHeadless()),
	})
	if err != nil {
		_ = pw.Stop()
		return nil, err
	}
	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Locale:     playwright.String("pt-BR"),
		TimezoneId: playwright.String("America/Sao_Paulo"),
		Viewport:   &playwright.Size{Width: 1366, Height: 900},
	})
	if err != nil {
		_ = browser.Close()
		_ = pw.Stop()
		return nil, err
	}
	page, err := ctx.NewPage()
	if err != nil {
		_ = browser.Close()
		_ = pw.Stop()
		return nil, err
	}

	pwInst = pw
	browserInst = browser
	pageInst = page
	return pageInst, nil
}

func navigationError(err error) *omap.M {
	return errMap(fmt.Sprintf("navigation failed: %s: %s", pyExcName(err), err.Error()))
}

// searchProducts mirrors search_products, including its tolerant waits and
// the logged-out empty state. Browser errors are returned as the same dict
// shape as the Python server instead of failing the JSON-RPC call.
func searchProducts(query string, limit int) any {
	page, err := getPage()
	if err != nil {
		return navigationError(err)
	}

	url := searchURL(slug(query))
	if _, err := page.Goto(url, playwright.PageGotoOptions{
		Timeout:   playwright.Float(navTimeoutMS),
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return navigationError(err)
	}

	// Wait for results, with tolerance for the login wall / empty state.
	_, _ = page.WaitForSelector(searchSelector, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(selectorTimeoutMS),
	})

	items, err := page.QuerySelectorAll(searchSelector)
	if err != nil {
		return navigationError(err)
	}
	if len(items) == 0 {
		text, err := page.InnerText("body")
		if err != nil {
			return navigationError(err)
		}
		note := strings.ReplaceAll(truncateRunes(text, 300), "\n", " ")
		return omap.From("query", query, "count", 0, "results", []any{}, "note", note)
	}

	value, err := page.Evaluate(extractJS, items)
	if err != nil {
		return navigationError(err)
	}
	results := asSlice(value)
	for i, r := range results {
		results[i] = orderResult(r)
	}
	n := len(results)
	if limit < n {
		n = limit
	}
	if n < 1 {
		n = 1
	}
	if n > len(results) {
		n = len(results)
	}
	results = results[:n]

	return omap.From(
		"query", query,
		"url", url,
		"count", len(results),
		"results", results,
	)
}

// orderResult restores the JS object property order lost in Go's map decode.
func orderResult(v any) any {
	m := asM(v)
	if m == nil {
		return v
	}
	out := omap.New()
	for _, key := range resultKeys {
		if value, ok := m.Get(key); ok {
			out.Set(key, value)
		}
	}
	return out
}

// listOrders mirrors list_orders with a persistent profile. The caller has
// already verified that the profile directory exists.
func listOrders(profile string) any {
	pw, err := playwright.Run()
	if err != nil {
		return navigationError(err)
	}
	defer pw.Stop()

	opts := contextOptions()
	opts.Channel = playwright.String("chrome")
	opts.Headless = playwright.Bool(isHeadless())
	ctx, err := pw.Chromium.LaunchPersistentContext(profile, opts)
	if err != nil {
		return navigationError(err)
	}
	defer ctx.Close()

	pages := ctx.Pages()
	var page playwright.Page
	if len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = ctx.NewPage()
		if err != nil {
			return navigationError(err)
		}
	}

	if _, err := page.Goto(ordersURL, playwright.PageGotoOptions{
		Timeout:   playwright.Float(navTimeoutMS),
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return navigationError(err)
	}
	page.WaitForTimeout(ordersSettleMS)
	url := page.URL()
	text, err := page.InnerText("body")
	if err != nil {
		return navigationError(err)
	}

	lower := strings.ToLower(url)
	if strings.Contains(lower, "login") || strings.Contains(lower, "identification") {
		return omap.From("error", notLoggedIn, "url", url)
	}
	return omap.From("url", url, "text", truncateRunes(text, 4000))
}

// cookieHeader mirrors _cookies_to_header.
func cookieHeader(cookies []playwright.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c.Name != "" {
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

// saveCookies mirrors _save, including the UTC timestamp and indent=2 output.
func saveCookies(cookies []playwright.Cookie) error {
	objects := make([]any, 0, len(cookies))
	for _, c := range cookies {
		objects = append(objects, omap.From(
			"name", c.Name,
			"value", c.Value,
			"domain", c.Domain,
			"path", c.Path,
			"expires", c.Expires,
			"httpOnly", c.HttpOnly,
			"secure", c.Secure,
			"sameSite", sameSiteValue(c),
		))
	}
	data := omap.From(
		"captured_at", time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"cookie_header", cookieHeader(cookies),
		"cookies", objects,
	)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return err
	}
	path := filepath.Join(browserBaseDir(), cookieFileName)
	if err := os.WriteFile(path, bytes.TrimSuffix(buf.Bytes(), []byte("\n")), 0o644); err != nil {
		return err
	}
	fmt.Printf("\nOK: %d cookies salvos em %s\n", len(cookies), path)
	return nil
}

func sameSiteValue(c playwright.Cookie) any {
	if c.SameSite == nil {
		return nil
	}
	return string(*c.SameSite)
}

// profileReady mirrors _profile_ready.
func profileReady(profile string) bool {
	for _, rel := range []string{
		filepath.Join("Default", "Network", "Cookies"),
		filepath.Join("Default", "Cookies"),
	} {
		if _, err := os.Stat(filepath.Join(profile, rel)); err == nil {
			return true
		}
	}
	return false
}

// chromeUserData mirrors the Python CHROME_USER_DATA path.
func chromeUserData() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = home
		}
	}
	return filepath.Join(base, "Google", "Chrome", "User Data")
}

var profileFiles = []string{
	"Local State",
	filepath.Join("Default", "Network", "Cookies"),
	filepath.Join("Default", "Network", "Cookies-journal"),
	filepath.Join("Default", "Cookies"),
	filepath.Join("Default", "Cookies-journal"),
	filepath.Join("Default", "Preferences"),
	filepath.Join("Default", "Secure Preferences"),
}

// seedProfile mirrors _seed_profile.
func seedProfile(profile string) int {
	copied := 0
	source := chromeUserData()
	for _, rel := range profileFiles {
		src := filepath.Join(source, rel)
		info, err := os.Stat(src)
		if err != nil || info.IsDir() {
			continue
		}
		dst := filepath.Join(profile, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			continue
		}
		if copyFile(src, dst, info.Mode()) == nil {
			copied++
		}
	}
	return copied
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// isLoggedIn mirrors _is_logged_in: no login/challenge URL and one of the
// strong logged-in markers in the page text.
func isLoggedIn(page playwright.Page) bool {
	url := page.URL()
	text, err := page.InnerText("body")
	if err != nil {
		return false
	}
	if strings.Contains(url, "login") || strings.Contains(url, "identification") || strings.Contains(url, "challenge") {
		return false
	}
	for _, marker := range []string{"Sair", "Minha conta", "Meu perfil"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// runLogin is the Go equivalent of python login.py, invoked with --login.
func runLogin() error {
	fmt.Println("== Mercado Livre login helper ==")

	profile := browserProfileDir()
	if !profileReady(profile) {
		source := chromeUserData()
		if _, err := os.Stat(source); err == nil {
			fmt.Printf("Herdando a sessao do Chrome: %s\n", source)
			fmt.Printf("Arquivos copiados: %d\n", seedProfile(profile))
		}
		if err := os.MkdirAll(profile, 0o755); err != nil {
			return err
		}
	}

	pw, err := playwright.Run()
	if err != nil {
		return err
	}
	defer pw.Stop()

	opts := contextOptions()
	opts.Channel = playwright.String("chrome")
	opts.Headless = playwright.Bool(false)
	ctx, err := pw.Chromium.LaunchPersistentContext(profile, opts)
	if err != nil {
		return err
	}
	defer ctx.Close()

	pages := ctx.Pages()
	var page playwright.Page
	if len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = ctx.NewPage()
		if err != nil {
			return err
		}
	}

	fmt.Println("Abrindo o Mercado Livre...")
	if _, err := page.Goto("https://www.mercadolivre.com.br", playwright.PageGotoOptions{
		Timeout:   playwright.Float(navTimeoutMS),
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		fmt.Printf("Aviso ao navegar: %s: %s\n", pyExcName(err), err.Error())
	}

	fmt.Println()
	fmt.Println(">>> Faca login na janela do Chrome.")
	fmt.Println(">>> O script detecta o login automaticamente (ate 10 min).")
	fmt.Println(">>> NAO feche a janela antes de ver 'Login detectado!'.")
	fmt.Println()

	start := time.Now()
	var lastCookies []playwright.Cookie
	for time.Since(start) < loginTimeout {
		page.WaitForTimeout(loginPollMS)
		if isLoggedIn(page) {
			fmt.Println("\nLogin detectado! Salvando...")
			page.WaitForTimeout(loginSettleMS)
			lastCookies, _ = ctx.Cookies()
			break
		}
		fmt.Printf(loginWaitMessage, int(time.Since(start).Seconds()))
	}

	cookies, err := ctx.Cookies()
	if err == nil && len(cookies) > 0 {
		lastCookies = cookies
	}

	if len(lastCookies) == 0 {
		return fmt.Errorf("ERRO: nenhum cookie capturado. Rode de novo e conclua o login.")
	}
	if err := saveCookies(lastCookies); err != nil {
		return err
	}
	fmt.Println()
	fmt.Println("Perfil salvo em:", profile)
	return nil
}
