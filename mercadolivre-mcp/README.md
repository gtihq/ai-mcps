# Mercado Livre MCP server

An MCP server that searches products on Mercado Livre (Brasil) with live prices (BRL), no API key. It is a Go port of the former Python server and speaks the same JSON-RPC 2.0 stdio protocol.

Mercado Livre serves search results only to a real browser (bot detection shows a "login to continue" wall to plain HTTP clients). This server drives a real Chrome through `playwright-go` to run the search and extract product cards. `shipping_options` uses the public API directly and needs no browser.

Note: Mercado Livre detects headless Chrome (serves an empty page), so a visible browser window is required by default. Set `ML_HEADLESS=1` to force headless anyway.

## Run

Build the binary from this directory:

```bash
go build -o mercadolivre-mcp .
```

On Windows, use `go build -o mercadolivre-mcp.exe .`. You can also run it without building:

```bash
go run .
```

Register the server with your agent. The command it runs is the built binary. For example, for OpenCode:

```bash
opencode mcp add mercadolivre --global -- /absolute/path/to/ai-mcps/mercadolivre-mcp/mercadolivre-mcp
```

The first run needs the Playwright driver. If it is not installed, install it once with `go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6201.1 install chromium` (the server itself never installs anything at runtime).

### One-time login

`list_orders` needs a logged-in session. Run the login helper once, log in in the Chrome window that opens, and the session is detected automatically (up to 10 minutes):

```bash
./mercadolivre-mcp --login
```

On Windows, use `mercadolivre-mcp.exe --login` or `go run . --login`. The persistent profile is saved in `.browser-profile` beside the executable, and the captured cookies are written to `cookies.json` in the same directory.

## Tools

| Tool | Description |
|------|-------------|
| `search_products` | Keyword search with price, old price, rating, review count and product link. Runs a real browser. |
| `shipping_options` | Quote shipping options for an `MLB...` item to a Brazilian CEP. |
| `list_orders` | List the logged-in user's purchases with shipping status. Requires `--login` first. |

## Layout

| Path | Responsibility |
|------|----------------|
| `main.go` | MCP entry point and `--login` flag handling |
| `tools.go` | Tool definitions, the shipping API call and response mapping |
| `browser.go` | Playwright browser, the extraction JS, the persistent profile and the login helper |
| `tools_test.go` | Tests with `testing` and `httptest`; no real browser is started |
| `LICENSE` | License for this server |

## Environment

| Variable | Default | Purpose |
|----------|---------|---------|
| `ML_HEADLESS` | `0` | `1` forces headless (may serve an empty page) |
| `LOCALAPPDATA` | user home | On Windows, where the existing Chrome profile is looked up during `--login` |

Search URLs (`https://lista.mercadolivre.com.br/{slug}`), the shipping API URL (`https://api.mercadolibre.com/items/{iid}/shipping_options?zip_code={digits}`) and the purchases URL (`https://myaccount.mercadolivre.com.br/my_purchases/list`) are fixed. HTTP requests use a 40 second timeout; browser navigation uses 60 seconds, the results selector waits 25 seconds, and `list_orders` waits 10 seconds after loading.

## Development

Run the tests and the vet check from the repository root:

```bash
go test ./mercadolivre-mcp/...
go vet ./mercadolivre-mcp/...
```

See [../../../README.md](../../../README.md) for the full repository.
