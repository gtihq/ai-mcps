# Shopee MCP server

An MCP server that searches products, flash deals, shop profiles, reviews, trending searches and categories on Shopee across 9 markets. It is a Go port of the former Python server and speaks the same JSON-RPC 2.0 stdio protocol, with the same 12 tools, descriptions, argument schemas and response shapes.

No API key. Keyword search requires a one-time login because Shopee only serves search results to a logged-in browser.

## Why the search opens a browser window

Shopee protects search with an anti-bot token (`af-ac-enc-dat`) that only the page itself can generate. Plain HTTP and headless browsers are rejected with error `90309999`. The server therefore runs the search in a real, visible Chrome window and reads the JSON response the page loads.

It is deliberately conservative because it runs on real customer accounts:

- **One window at a time.** Concurrent calls queue instead of opening several browsers.
- **One page load per search.** No reloads and no retry loops.
- **Minimum gap between searches.** `SHOPEE_MIN_INTERVAL_SECONDS` (default `10`) is enforced after the previous search finishes.
- Slow is fine; hammering is not.

## Run

Build the binary from this directory:

```bash
go build -o shopee-mcp .
```

On Windows, use `go build -o shopee-mcp.exe .`. You can also run it without building:

```bash
go run .
```

Log in once (opens a visible Chrome window). The helper saves a dedicated profile in `.browser-profile/` and a backup in `cookies.json`:

```bash
go run . --login
```

Do not delete the profile: Shopee trusts a browser that has completed a login, and `search_products` and `list_orders` reuse the same profile. Both files contain real session material and are ignored by git.

Register the server with your agent. The command it runs is the built binary. For example, for OpenCode:

```bash
opencode mcp add shopee --global -- /absolute/path/to/ai-mcps/shopee-mcp/shopee-mcp
```

## Tools

| Tool | Description |
|------|-------------|
| `search_autocomplete` | Keyword and shop suggestions for a partial query. |
| `search_trending` | Current trending searches for a market. |
| `search_products` | Full keyword product search with live prices. Requires a logged-in browser. |
| `products_trending` | About 100 trending products with live prices and ratings. |
| `product_details` | Full product detail by id or URL. |
| `deals_flash_sale` | Live flash-sale deals with price, discount, stock and units sold. |
| `shop_details` | Shop profile, rating breakdown, followers, response rate and vouchers. |
| `shop_reviews` | Buyer reviews filtered by star rating. |
| `shop_categories` | A shop's own product categories with item counts. |
| `shops_official` | The official brand store directory. |
| `categories_list` | The full Shopee category tree. |
| `list_orders` | The logged-in user's purchases with shipping status. |

`sort` accepts `relevancy`, `sales`, `newest`, `price_asc`, `price_desc`. Prices are in the market currency (BRL for `country=br`). The default country is `br`. Markets are `sg`, `my`, `th`, `vn`, `ph`, `id`, `tw`, `br`, `mx`. A product or shop link carries its own market and overrides the `country` argument.

## Layout

| Path | Responsibility |
|------|----------------|
| `main.go` | MCP entry point and the `--login` flag |
| `refs.go` | Markets, currencies, link builders and product/shop reference resolution |
| `parsers.go` | Value helpers and product page parsing |
| `parsers_shops.go` | Shop, review, voucher and official shop parsing |
| `parsers_catalog.go` | Category tree, autocomplete, trending, flash sale and feed parsing |
| `jsonutil.go` | Ordered JSON decoding and Python-compatible value coercion |
| `api.go` | Shopee HTTP transport, pagination and the 10 non-browser endpoints |
| `schemas.go` | Request validation used by the schema tests |
| `tools.go` | The 12 MCP tool definitions, handlers and search-item parser |
| `browser.go` | Real-browser search, cookies, `list_orders` and the login helper |
| `shopee/fixtures/` | Captured Shopee pages and JSON used by the tests |

## Environment

| Variable | Default | Purpose |
|----------|---------|---------|
| `SHOPEE_MIN_INTERVAL_SECONDS` | `10` | Minimum gap between two browser searches |
| `SHOPEE_PROXY` | unset | Proxy URL for the plain HTTP scraper calls |

`SHOPEE_HEADLESS` is no longer used: search requires a visible window.

## Development

Run the tests and the vet check from the repository root:

```bash
go test ./shopee-mcp/...
go vet ./shopee-mcp/...
```

No test requires a real browser. HTTP tests use `httptest` servers through package-level base URL overrides, and parser tests read the captured fixtures under `shopee/fixtures/`.

See [../../../README.md](../../../README.md) for the full repository.
