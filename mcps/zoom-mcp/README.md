<div align="center">

# Zoom MCP

> Product search on the Zoom price comparator

**It queries Zoom's public search over HTTP and returns the best price together with the stores selling each product.**

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](#requirements)
[![Protocol](https://img.shields.io/badge/protocol-stdio%20JSON--RPC-blue.svg)](#overview)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20Windows-lightgrey.svg)](#requirements)

</div>

---

## Overview

An MCP server that searches products on Zoom (zoom.com.br), a Brazilian price comparator. It aggregates KaBuM!, Mercado Livre, Amazon, Magazine Luiza and others, with the best price and the list of stores selling each product. No API key. It is a Go port of the former Python server and speaks the same JSON-RPC 2.0 stdio protocol.

## Requirements

- Go 1.25 or later on Linux or Windows.
- No browser, login and API key are required.

## Build and run

Build the binary from this directory:

```bash
go build -o zoom-mcp .
```

On Windows, use `go build -o zoom-mcp.exe .`. You can also run it without building:

```bash
go run .
```

## Register with your agent

Register the server with your agent. The command it runs is the built binary. For example, for OpenCode:

```bash
opencode mcp add zoom --global -- /absolute/path/to/ai-mcps/mcps/zoom-mcp/zoom-mcp
```

## Login

No login is required. Zoom's public search and product endpoints are queried over plain HTTP with a Chrome TLS fingerprint.

## Tools

| Tool | Description |
|------|-------------|
| `search_products` | Search products on Zoom with the best price (BRL), best store, all stores, store count, rating and the Zoom product URL. |

## Environment

No environment variables are required. The base URL (`https://www.zoom.com.br`) is a constant in `tools.go` and is also used to build every product URL. Requests use a Chrome TLS fingerprint and a 40 second timeout.

## Layout

| Path | Responsibility |
|------|----------------|
| `main.go` | MCP entry point: builds the server with `mcpserver.New("zoom", ...)` and calls `Run()` |
| `tools.go` | Tool definition, Zoom HTTP call and response mapping |
| `tools_test.go` | Tests with `testing` and `httptest` |

## Development

Run the tests and the vet check from the repository root:

```bash
go test ./mcps/zoom-mcp/...
go vet ./mcps/zoom-mcp/...
```

See [../../README.md](../../README.md) for the full repository.
