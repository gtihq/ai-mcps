// Command twitter-mcp is the Go replacement for the Python Twitter/X MCP
// server. It drives one shared headless Chromium on a persistent profile.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
	"github.com/gtihq/ai-mcps/internal/omap"
	playwright "github.com/mxschmitt/playwright-go"
)

func main() {
	login := flag.Bool("login", false, "open a visible browser on the X profile for a one-time login")
	keepalive := flag.Bool("keepalive", false, "ping X once to refresh the session cookies")
	flag.Parse()
	if *login && *keepalive {
		fmt.Fprintln(os.Stderr, "Use only one of --login and --keepalive.")
		os.Exit(2)
	}
	if *login {
		if err := runLogin(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *keepalive {
		os.Exit(runKeepalive())
	}

	defer session.Shutdown()
	server := mcpserver.New("twitter-mcp", tools()...)
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}

// runLogin is the Go equivalent of login.py.
func runLogin() error {
	pw, ctx, err := getPersistentContext(false)
	if err != nil {
		return err
	}
	defer closeSession(pw, ctx)

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
	if _, err := page.Goto("https://x.com/login"); err != nil {
		return err
	}
	fmt.Printf("Browser opened on profile %s. Log in to X in that window.\n", PROFILE_DIR)
	fmt.Println("After logging in, press Enter here to save the session and close the browser.")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Println("Session saved.")
	return nil
}

// binaryDir is the Go counterpart of Python's Path(__file__).parent.
func binaryDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

type keepaliveResult struct {
	success bool
	fields  *omap.M
}

// pingTwitter is the Go equivalent of twitter_keepalive.ping_twitter.
func pingTwitter() keepaliveResult {
	pw, ctx, err := getPersistentContext(HEADLESS)
	if err != nil {
		return keepaliveResult{fields: omap.From("success", false, "error", fmt.Sprintf("Browser launch failed: %s", err))}
	}

	ping := func() keepaliveResult {
		pages := ctx.Pages()
		var page playwright.Page
		if len(pages) > 0 {
			page = pages[0]
		} else {
			page, err = ctx.NewPage()
			if err != nil {
				return keepaliveResult{fields: omap.From("success", false, "error", err.Error())}
			}
		}
		resp, err := page.Goto("https://x.com/home", playwright.PageGotoOptions{
			Timeout:   playwright.Float(20000),
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		})
		if err != nil {
			return keepaliveResult{fields: omap.From("success", false, "error", err.Error())}
		}
		url := page.URL()
		status := 0
		if resp != nil {
			status = resp.Status()
		}
		if strings.Contains(url, "login") || strings.Contains(url, "accounts.google") {
			return keepaliveResult{fields: omap.From("success", false, "error", "Session expired", "url", url)}
		}
		if _, err := page.Evaluate("window.scrollTo(0, 300)"); err != nil {
			return keepaliveResult{fields: omap.From("success", false, "error", err.Error())}
		}
		page.WaitForTimeout(2000)
		return keepaliveResult{
			success: true,
			fields: omap.From(
				"success", true,
				"status", status,
				"message", "Session alive, cookies refreshed",
			),
		}
	}

	result := ping()
	closeSession(pw, ctx)
	return result
}

// runKeepalive is the Go equivalent of twitter_keepalive.main. It returns the
// process exit code: 0 when the session is alive, 1 otherwise.
func runKeepalive() int {
	dataDir := filepath.Join(binaryDir(), "data")
	_ = os.MkdirAll(dataDir, 0o755)
	ts := time.Now().UTC().Format("2006-01-02 15:04:05") + " UTC"
	result := pingTwitter()
	status := "FAIL"
	if result.success {
		status = "OK"
	}
	message := ""
	if v, ok := result.fields.Get("message"); ok {
		message, _ = v.(string)
	}
	if message == "" {
		if v, ok := result.fields.Get("error"); ok {
			message, _ = v.(string)
		}
	}
	fmt.Printf("[%s] Twitter keepalive: %s: %s\n", ts, status, message)

	entry := omap.From("timestamp", ts)
	for _, key := range result.fields.Keys() {
		value, _ := result.fields.Get(key)
		entry.Set(key, value)
	}
	logFile := filepath.Join(dataDir, "twitter_keepalive.log")
	if f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		_, _ = fmt.Fprintln(f, entry)
		_ = f.Close()
	}
	if result.success {
		return 0
	}
	return 1
}
