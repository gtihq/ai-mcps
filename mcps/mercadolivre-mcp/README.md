<div align="center">

# Mercado Livre MCP

> Product search, shipping quotes and orders on Mercado Livre

**It drives a real Chrome through `playwright-go` for search and orders, while shipping quotes use the public API directly.**

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](#requirements)
[![Protocol](https://img.shields.io/badge/protocol-stdio%20JSON--RPC-blue.svg)](#overview)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20Windows-lightgrey.svg)](#requirements)

</div>

---

## Overview

An MCP server that searches products on Mercado Livre (Brasil) with live prices (BRL), no API key. It is a Go port of the former Python server and speaks the same JSON-RPC 2.0 stdio protocol.

Mercado Livre serves search results only to a real browser (bot detection shows a "login to continue" wall to plain HTTP clients). This server drives a real Chrome through `playwright-go` to run the search and extract product cards. `shipping_options` uses the public API directly and needs no browser.

Mercado Livre detects headless Chrome (serves an empty page), so a visible browser window is required by default. Set `ML_HEADLESS=1` to force headless anyway.

## Requirements

- Go 1.25 or later on Linux or Windows.
- Google Chrome and the playwright-go driver for `search_products` and `list_orders`.
- A one-time login for `list_orders`.
- No API key is required.

## Build and run

Build the binary from this directory:

```bash
go build -o mercadolivre-mcp .
```

On Windows, use `go build -o mercadolivre-mcp.exe .`. You can also run it without building:

```bash
go run .
```

The first run needs the Playwright driver. If it is not installed, install it once with `go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6201.1 install chromium` (the server itself never installs anything at runtime).

## Register with your agent

Register the server with your agent. The command it runs is the built binary. For example, for OpenCode:

```bash
opencode mcp add mercadolivre --global -- /absolute/path/to/ai-mcps/mcps/mercadolivre-mcp/mercadolivre-mcp
```

## Login

`list_orders` needs a logged-in session. Search and `shipping_options` work without one.

### One-time login

Run the login helper once, log in in the Chrome window that opens, and the session is detected automatically (up to 10 minutes):

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

## Environment

| Variable | Default | Purpose |
|----------|---------|---------|
| `ML_HEADLESS` | `0` | `1` forces headless (may serve an empty page) |
| `LOCALAPPDATA` | user home | On Windows, where the existing Chrome profile is looked up during `--login` |

Search URLs (`https://lista.mercadolivre.com.br/{slug}`), the shipping API URL (`https://api.mercadolibre.com/items/{iid}/shipping_options?zip_code={digits}`) and the purchases URL (`https://myaccount.mercadolivre.com.br/my_purchases/list`) are fixed. HTTP requests use a 40 second timeout; browser navigation uses 60 seconds, the results selector waits 25 seconds, and `list_orders` waits 10 seconds after loading.

## Layout

| Path | Responsibility |
|------|----------------|
| `main.go` | MCP entry point and `--login` flag handling |
| `tools.go` | Tool definitions, the shipping API call and response mapping |
| `browser.go` | Playwright browser, the extraction JS, the persistent profile and the login helper |
| `tools_test.go` | Tests with `testing` and `httptest`; no real browser is started |
| `LICENSE` | License for this server |

## Development

Run the tests and the vet check from the repository root:

```bash
go test ./mcps/mercadolivre-mcp/...
go vet ./mcps/mercadolivre-mcp/...
```

See [../../README.md](../../README.md) for the full repository.
