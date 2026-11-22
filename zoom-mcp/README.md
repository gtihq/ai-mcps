# Zoom MCP server

An MCP server that searches products on Zoom (zoom.com.br), a Brazilian price comparator. It aggregates KaBuM!, Mercado Livre, Amazon, Magazine Luiza and others, with the best price and the list of stores selling each product. No API key. It is a Go port of the former Python server and speaks the same JSON-RPC 2.0 stdio protocol.

## Run

Build the binary from this directory:

```bash
go build -o zoom-mcp .
```

On Windows, use `go build -o zoom-mcp.exe .`. You can also run it without building:

```bash
go run .
```

Register the server with your agent. The command it runs is the built binary. For example, for OpenCode:

```bash
opencode mcp add zoom --global -- /absolute/path/to/ai-mcps/zoom-mcp/zoom-mcp
```

## Tools

| Tool | Description |
|------|-------------|
| `search_products` | Search products on Zoom with the best price (BRL), best store, all stores, store count, rating and the Zoom product URL. |

## Layout

| Path | Responsibility |
|------|----------------|
| `main.go` | MCP entry point: builds the server with `mcpserver.New("zoom", ...)` and calls `Run()` |
| `tools.go` | Tool definition, Zoom HTTP call and response mapping |
| `tools_test.go` | Tests with `testing` and `httptest` |

## Environment

No environment variables are required. The base URL (`https://www.zoom.com.br`) is a constant in `tools.go` and is also used to build every product URL. Requests use a Chrome TLS fingerprint and a 40 second timeout.

## Development

Run the tests and the vet check from the repository root:

```bash
go test ./zoom-mcp/...
go vet ./zoom-mcp/...
```

See [../../../README.md](../../../README.md) for the full repository.
