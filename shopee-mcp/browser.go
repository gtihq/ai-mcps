package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gtihq/ai-mcps/internal/omap"
	playwright "github.com/mxschmitt/playwright-go"
)

const (
	profileDirName = ".browser-profile"
	cookieFileName = "cookies.json"
	loginTimeout   = 10 * time.Minute
)

var (
	// browserProfileOverride lets tools_test.go point list_orders at a missing
	// directory without touching the real profile beside the executable.
	browserProfileOverride string

	browserMu          sync.Mutex
	browserLastDone    time.Time
	browserMinInterval = minIntervalFromEnv()
)

// browserBaseDir is the directory beside the executable, standing in for
// Python's _SCRIPT_DIR. os.Executable is used so a compiled binary finds its
// own profile regardless of the working directory.
func browserBaseDir() string {
	if browserProfileOverride != "" {
		return filepath.Dir(browserProfileOverride)
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

func browserProfileDir() string {
	if browserProfileOverride != "" {
		return browserProfileOverride
	}
	return filepath.Join(browserBaseDir(), profileDirName)
}

func cookieFilePath() string { return filepath.Join(browserBaseDir(), cookieFileName) }

func minIntervalFromEnv() float64 {
	raw := os.Getenv("SHOPEE_MIN_INTERVAL_SECONDS")
	if raw == "" {
		raw = "10"
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || f < 0 {
		return 0
	}
	return f
}

func persistentOptions() playwright.BrowserTypeLaunchPersistentContextOptions {
	return playwright.BrowserTypeLaunchPersistentContextOptions{
		Channel:    playwright.String("chrome"),
		Headless:   playwright.Bool(false),
		Locale:     playwright.String("pt-BR"),
		TimezoneId: playwright.String("America/Sao_Paulo"),
		Viewport:   &playwright.Size{Width: 1366, Height: 900},
	}
}

var cookieKeys = map[string]bool{
	"name": true, "value": true, "domain": true, "path": true,
	"expires": true, "httpOnly": true, "secure": true, "sameSite": true,
}

// browserCookies is mcp_server._browser_cookies: cookies.json in Playwright's
// add_cookies format.
func browserCookies() []playwright.OptionalCookie {
	rawBytes, err := os.ReadFile(cookieFilePath())
	if err != nil {
		return nil
	}
	payload, err := decodeJSON(rawBytes)
	if err != nil {
		return nil
	}
	root := asM(payload)
	var list []any
	if c := mget(root, "cookies"); c != nil {
		list = asSlice(c)
	}
	out := make([]playwright.OptionalCookie, 0, len(list))
	for _, raw := range list {
		c := asM(raw)
		if c == nil {
			continue
		}
		filtered := omap.New()
		for _, key := range c.Keys() {
			if cookieKeys[key] {
				filtered.Set(key, mget(c, key))
			}
		}
		var sameSite *playwright.SameSiteAttribute
		if s, ok := mget(filtered, "sameSite").(string); ok {
			switch strings.ToLower(s) {
			case "lax", "strict", "none":
				sameSite = map[string]*playwright.SameSiteAttribute{
					"lax":    playwright.SameSiteAttributeLax,
					"strict": playwright.SameSiteAttributeStrict,
					"none":   playwright.SameSiteAttributeNone,
				}[strings.ToLower(s)]
			default:
				sameSite = playwright.SameSiteAttributeLax
			}
		}
		expires := float64(-1)
		if e, ok := toFloat(mget(filtered, "expires")); ok && e != 0 {
			expires = e
		}
		domain, _ := mget(filtered, "domain").(string)
		if domain == "" {
			continue
		}
		path, _ := mget(filtered, "path").(string)
		name, _ := mget(filtered, "name").(string)
		value, _ := mget(filtered, "value").(string)
		opt := playwright.OptionalCookie{
			Name: name, Value: value, Domain: &domain, Expires: &expires,
		}
		if path != "" {
			opt.Path = &path
		}
		if b, ok := mget(filtered, "httpOnly").(bool); ok {
			opt.HttpOnly = &b
		}
		if b, ok := mget(filtered, "secure").(bool); ok {
			opt.Secure = &b
		}
		opt.SameSite = sameSite
		out = append(out, opt)
	}
	return out
}

// searchBrowser is mcp_server._search_browser: a real headed Chrome, one
// navigation, serialized and rate limited, capturing the page's own JSON.
func searchBrowser(country, keyword, sort string, page, limit int, minPrice, maxPrice any) any {
	if _, ok := searchSortMap[sort]; !ok {
		return errMap("sort must be one of " + strings.Join(searchSorts, ", "))
	}
	by, order := searchSortMap[sort][0], searchSortMap[sort][1]
	url := fmt.Sprintf("%s/search?keyword=%s&page=%d&sortBy=%s&order=%s",
		site(country), strings.ReplaceAll(keyword, " ", "%20"), page-1, by, order)

	browserMu.Lock()
	defer browserMu.Unlock()
	if wait := browserMinInterval - time.Since(browserLastDone).Seconds(); wait > 0 {
		time.Sleep(time.Duration(wait * float64(time.Second)))
	}
	defer func() { browserLastDone = time.Now() }()

	pw, err := playwright.Run()
	if err != nil {
		return browserFailure(err)
	}
	defer pw.Stop()

	ctx, err := pw.Chromium.LaunchPersistentContext(browserProfileDir(), persistentOptions())
	if err != nil {
		return browserFailure(err)
	}
	defer ctx.Close()

	pages := ctx.Pages()
	var tab playwright.Page
	if len(pages) > 0 {
		tab = pages[0]
	} else {
		tab, err = ctx.NewPage()
		if err != nil {
			return browserFailure(err)
		}
	}

	var capturedMu sync.Mutex
	var captured []any
	tab.OnResponse(func(r playwright.Response) {
		if !strings.Contains(r.URL(), "/api/v4/search/search_items") {
			return
		}
		var payload any
		if err := r.JSON(&payload); err != nil {
			return
		}
		m := asM(payload)
		data := asM(mget(m, "data"))
		items := asSlice(mget(data, "items"))
		if len(items) == 0 {
			items = asSlice(mget(m, "items"))
		}
		errValue := mget(m, "error")
		if len(items) > 0 && (errValue == nil || numEq(errValue, 0) || errValue == "") {
			capturedMu.Lock()
			captured = append(captured, m)
			capturedMu.Unlock()
		}
	})

	// Seed the missing SPC_U cookie from cookies.json, exactly like the
	// Python server.
	shopeeCookies, _ := ctx.Cookies()
	hasSPCU := false
	for _, c := range shopeeCookies {
		if strings.Contains(c.Domain, "shopee") && c.Name == "SPC_U" {
			hasSPCU = true
			break
		}
	}
	if !hasSPCU {
		if cookies := browserCookies(); len(cookies) > 0 {
			_ = ctx.AddCookies(cookies)
		}
	}

	// One navigation only. No reloads and no retries.
	_, err = tab.Goto(url, playwright.PageGotoOptions{
		Timeout:   playwright.Float(45000),
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	if err != nil {
		return browserFailure(err)
	}
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		capturedMu.Lock()
		n := len(captured)
		capturedMu.Unlock()
		if n > 0 {
			break
		}
		tab.WaitForTimeout(1000)
	}
	if len(captured) == 0 {
		return errMap("Shopee did not return results in the browser. " +
			"Run `python login.py` again and confirm you are logged in.")
	}

	rows, total := parseSearchItems(captured[0], country)
	if minPrice != nil {
		min, _ := toFloat(minPrice)
		filtered := make([]any, 0, len(rows))
		for _, row := range rows {
			price, ok := toFloat(mget(asM(row), "price"))
			if !ok {
				price = 0
			}
			if price >= min {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	if maxPrice != nil {
		max, _ := toFloat(maxPrice)
		filtered := make([]any, 0, len(rows))
		for _, row := range rows {
			price, ok := toFloat(mget(asM(row), "price"))
			if !ok {
				price = 0
			}
			if price <= max {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	if limit < len(rows) {
		rows = rows[:limit]
	}
	return omap.From(
		"country", country,
		"currency", CURRENCY_OF[country],
		"query", keyword,
		"sort", sort,
		"page", page,
		"total", total,
		"count", len(rows),
		"results", rows,
		"source", "browser",
	)
}

func browserFailure(err error) *omap.M {
	return errMap(fmt.Sprintf("browser search failed: %s: %s", pyExcName(err), err.Error()))
}

// listOrders is mcp_server.list_orders. The caller uses the raw country (the
// Python tool does not call _country), with "br" as its signature default.
func listOrders(country string) any {
	profile := browserProfileDir()
	if _, err := os.Stat(profile); err != nil {
		return errMap("not logged in: run `python login.py` first")
	}
	pw, err := playwright.Run()
	if err != nil {
		return errMap(fmt.Sprintf("browser failed: %s: %s", pyExcName(err), err.Error()))
	}
	defer pw.Stop()

	ctx, err := pw.Chromium.LaunchPersistentContext(profile, persistentOptions())
	if err != nil {
		return errMap(fmt.Sprintf("browser failed: %s: %s", pyExcName(err), err.Error()))
	}
	defer ctx.Close()

	pages := ctx.Pages()
	var page playwright.Page
	if len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = ctx.NewPage()
		if err != nil {
			return errMap(fmt.Sprintf("browser failed: %s: %s", pyExcName(err), err.Error()))
		}
	}
	_, err = page.Goto(site(country)+"/user/purchase", playwright.PageGotoOptions{
		Timeout:   playwright.Float(60000),
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	if err != nil {
		return errMap(fmt.Sprintf("browser failed: %s: %s", pyExcName(err), err.Error()))
	}
	page.WaitForTimeout(12000)
	url := page.URL()
	text, err := page.InnerText("body")
	if err != nil {
		return errMap(fmt.Sprintf("browser failed: %s: %s", pyExcName(err), err.Error()))
	}
	lower := strings.ToLower(url)
	if strings.Contains(lower, "login") || strings.Contains(lower, "verify") {
		return omap.From("error", "not logged in: run `python login.py` first", "url", url)
	}
	return omap.From("url", url, "text", cutRunes(text, 4000))
}

// ---- one-time login helper (login.py) --------------------------------------

var loginIndicators = map[string]bool{"SPC_EC": true, "SPC_U": true, "sessionid": true}

func loggedIn(names map[string]bool) bool {
	for name := range names {
		if loginIndicators[name] {
			return true
		}
	}
	return false
}

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

func profileReady(profile string) bool {
	for _, rel := range []string{
		filepath.Join("Default", "Network", "Cookies"),
		filepath.Join("Default", "Cookies"),
	} {
		if info, err := os.Stat(filepath.Join(profile, rel)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

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
		in, err := os.Open(src)
		if err != nil {
			continue
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			_ = in.Close()
			continue
		}
		_, copyErr := io.Copy(out, in)
		_ = in.Close()
		if closeErr := out.Close(); copyErr == nil && closeErr == nil {
			copied++
		}
	}
	return copied
}

func cookieHeader(cookies []playwright.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c.Name != "" {
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func saveCookies(cookies []playwright.Cookie) error {
	names := make(map[string]bool)
	objects := make([]any, 0, len(cookies))
	for _, c := range cookies {
		names[c.Name] = true
		var sameSite any
		if c.SameSite != nil {
			sameSite = string(*c.SameSite)
		}
		objects = append(objects, omap.From(
			"name", c.Name, "value", c.Value, "domain", c.Domain, "path", c.Path,
			"expires", c.Expires, "httpOnly", c.HttpOnly, "secure", c.Secure,
			"sameSite", sameSite,
		))
	}
	data := omap.From(
		"captured_at", time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"cookie_header", cookieHeader(cookies),
		"cookies", objects,
		"logged_in", loggedIn(names),
	)
	// Encoder with SetEscapeHTML(false) is closest to
	// json.dump(ensure_ascii=False, indent=2).
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return err
	}
	path := cookieFilePath()
	if err := os.WriteFile(path, bytes.TrimSuffix(buf.Bytes(), []byte("\n")), 0o644); err != nil {
		return err
	}
	fmt.Printf("OK: %d cookies salvos em %s\n", len(cookies), path)
	fmt.Printf("Login detectado: %s\n", map[bool]string{true: "SIM", false: "NAO CONFIRMADO"}[loggedIn(names)])
	return nil
}

// runLogin is the Go equivalent of python login.py, invoked with --login.
func runLogin() error {
	fmt.Println("== Shopee login helper ==")
	profile := browserProfileDir()
	if !profileReady(profile) {
		source := chromeUserData()
		if info, err := os.Stat(source); err == nil && info.IsDir() {
			fmt.Printf("Herdando a sessao do Chrome: %s\n", source)
			fmt.Printf("Arquivos copiados: %d\n", seedProfile(profile))
			if !profileReady(profile) {
				fmt.Println()
				fmt.Println("ATENCAO: nao consegui copiar o banco de cookies do Chrome.")
				fmt.Println("O Chrome parece estar aberto e travando o arquivo.")
				fmt.Println("Feche TODAS as janelas do Chrome e rode de novo se quiser")
				fmt.Println("herdar a sessao - ou apenas faca login na janela a seguir.")
			}
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
	ctx, err := pw.Chromium.LaunchPersistentContext(profile, persistentOptions())
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

	fmt.Println("Abrindo a Shopee...")
	if _, err := page.Goto("https://shopee.com.br", playwright.PageGotoOptions{
		Timeout:   playwright.Float(60000),
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		fmt.Printf("Aviso ao navegar: %s: %s\n", pyExcName(err), err.Error())
	}
	fmt.Println("Faca login na janela (QR, e-mail/senha ou Facebook).")
	fmt.Println("Pode fechar a janela quando terminar: os cookies sao salvos de qualquer forma.")
	fmt.Println("Aguardando o login...")

	start := time.Now()
	var lastCookies []playwright.Cookie
	for time.Since(start) < loginTimeout {
		page.WaitForTimeout(2000)
		cookies, err := ctx.Cookies()
		if err != nil {
			continue
		}
		lastCookies = cookies
		names := make(map[string]bool, len(cookies))
		for _, c := range cookies {
			names[c.Name] = true
		}
		if loggedIn(names) {
			fmt.Println("Login detectado! Deixando a pagina terminar de carregar...")
			page.WaitForTimeout(6000)
			if cookies, err := ctx.Cookies(); err == nil {
				lastCookies = cookies
			}
			break
		}
	}
	if cookies, err := ctx.Cookies(); err == nil && len(cookies) > 0 {
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
	fmt.Println("(nao apague essa pasta: e o navegador confiavel que o MCP usa nas buscas)")
	return nil
}
