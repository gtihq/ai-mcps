# KaBuM! MCP server

An MCP server that searches products on KaBuM! with live prices (BRL), no API key. It is a Go port of the former Python server and speaks the same JSON-RPC 2.0 stdio protocol.

## Run

Build the binary from this directory:

```bash
go build -o kabum-mcp .
```

On Windows, use `go build -o kabum-mcp.exe .`. You can also run it without building:

```bash
go run .
```

Register the server with your agent. The command it runs is the built binary. For example, for OpenCode:

```bash
opencode mcp add kabum --global -- /absolute/path/to/ai-mcps/kabum-mcp/kabum-mcp
```

## Tools

| Tool | Description |
|------|-------------|
| `search_products` | Keyword search with live prices (BRL), Pix/discount price, stock, rating and product links. |
| `product_details` | Full detail for one product by its numeric id. |
| `zip_lookup` | Resolve a Brazilian CEP to its address via KaBuM!'s shipping service. |
| `shipping_quote` | Quote shipping options and prices for a product to a Brazilian CEP. |

## Layout

| Path | Responsibility |
|------|----------------|
| `main.go` | MCP entry point: builds the server with `mcpserver.New("kabum", ...)` and calls `Run()` |
| `tools.go` | Tool definitions, KaBuM! HTTP calls and response mapping |
| `tools_test.go` | Tests with `testing` and `httptest` |
| `LICENSE` | License for this server |

## Environment

No environment variables are required. The API base URL (`https://servicespub.prod.api.aws.grupokabum.com.br`) and the site URL (`https://www.kabum.com.br`) are constants in `tools.go`. Requests use a Chrome TLS fingerprint and a 40 second timeout.

## Development

Run the tests and the vet check from the repository root:

```bash
go test ./kabum-mcp/...
go vet ./kabum-mcp/...
```

See [../../../README.md](../../../README.md) for the full repository.
