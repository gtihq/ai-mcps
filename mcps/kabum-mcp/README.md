<div align="center">

# KaBuM! MCP

> Product search and shipping quotes on KaBuM!

**It searches products and quotes shipping through KaBuM!'s public HTTP endpoints, speaking the same JSON-RPC 2.0 stdio protocol as the former Python server.**

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](#requirements)
[![Protocol](https://img.shields.io/badge/protocol-stdio%20JSON--RPC-blue.svg)](#overview)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20Windows-lightgrey.svg)](#requirements)

</div>

---

## Overview

An MCP server that searches products on KaBuM! with live prices (BRL), no API key. It is a Go port of the former Python server and speaks the same JSON-RPC 2.0 stdio protocol. Requests use a Chrome TLS fingerprint, but no browser is launched and no session is kept.

## Requirements

- Go 1.25 or later on Linux or Windows.
- No browser and no API key are required.

## Build and run

Build the binary from this directory:

```bash
go build -o kabum-mcp .
```

On Windows, use `go build -o kabum-mcp.exe .`. You can also run it without building:

```bash
go run .
```

## Register with your agent

Register the server with your agent. The command it runs is the built binary. For example, for OpenCode:

```bash
opencode mcp add kabum --global -- /absolute/path/to/ai-mcps/mcps/kabum-mcp/kabum-mcp
```

## Login

No login is required. KaBuM! serves product and shipping data through public HTTP endpoints, so the server makes anonymous requests with a Chrome TLS fingerprint.

## Tools

| Tool | Description |
|------|-------------|
| `search_products` | Keyword search with live prices (BRL), Pix/discount price, stock, rating and product links. |
| `product_details` | Full detail for one product by its numeric id. |
| `zip_lookup` | Resolve a Brazilian CEP to its address via KaBuM!'s shipping service. |
| `shipping_quote` | Quote shipping options and prices for a product to a Brazilian CEP. |

## Environment

No environment variables are required. The API base URL (`https://servicespub.prod.api.aws.grupokabum.com.br`) and the site URL (`https://www.kabum.com.br`) are constants in `tools.go`. Requests use a Chrome TLS fingerprint and a 40 second timeout.

## Layout

| Path | Responsibility |
|------|----------------|
| `main.go` | MCP entry point: builds the server with `mcpserver.New("kabum", ...)` and calls `Run()` |
| `tools.go` | Tool definitions, KaBuM! HTTP calls and response mapping |
| `tools_test.go` | Tests with `testing` and `httptest` |
| `LICENSE` | License for this server |

## Development

Run the tests and the vet check from the repository root:

```bash
go test ./mcps/kabum-mcp/...
go vet ./mcps/kabum-mcp/...
```

See [../../README.md](../../README.md) for the full repository.
