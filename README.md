# GTI AI MCPs

MCP servers used by the GTI team's AI coding agents. Every server is a standalone Go command in its own folder, built from one Go module at the repository root. The servers speak MCP over stdio (JSON-RPC 2.0, newline delimited) and work on Linux and Windows.

## Servers

| Folder | What it does | Build | Tools |
|--------|--------------|-------|-------|
| [`kabum-mcp`](kabum-mcp/README.md) | Product search and shipping quotes on KaBuM! | `go build ./kabum-mcp` | 4 |
| [`mercadolivre-mcp`](mercadolivre-mcp/README.md) | Product search, shipping quotes and orders on Mercado Livre | `go build ./mercadolivre-mcp` | 3 |
| [`shopee-mcp`](shopee-mcp/README.md) | Products, deals, shops, reviews and orders on Shopee | `go build ./shopee-mcp` | 12 |
| [`twitter-mcp`](twitter-mcp/README.md) | Read and post on X through a logged-in browser | `go build ./twitter-mcp` | 12 |
| [`zoom-mcp`](zoom-mcp/README.md) | Product search on the Zoom price comparator | `go build ./zoom-mcp` | 1 |
| `netdev-mcp` | Manage MikroTik, ZTE, Huawei and SSH network devices | `go build ./netdev-mcp` | see source |

## Requirements

- Go 1.25 or later on Linux or Windows.
- Google Chrome for the three servers that drive a real browser (Mercado Livre, Shopee, Twitter). `PLAYWRIGHT_BROWSERS_PATH` and the playwright-go driver follow the usual Go module setup.
- No API keys are required. Login-based servers keep a browser profile next to the binary.

## Build and run

From the repository root:

```bash
go build -o kabum-mcp ./kabum-mcp
./kabum-mcp
```

On Windows PowerShell:

```powershell
go build -o kabum-mcp.exe .\kabum-mcp
.\kabum-mcp.exe
```

Every server can also be started directly with `go run ./<folder>`. Servers that need a login expose a `--login` flag; `netdev-mcp` also has `--setup` and `-listen`, where `-listen 0.0.0.0:8765` makes it reachable over Tailscale.

## Verify

```bash
go test ./...
go vet ./...
GOOS=linux GOARCH=amd64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...
```

The `tools/list` schemas of the five ported servers are compared against the original Python servers in automated checks and match 1:1, including descriptions, defaults, key order and output schemas.

## Sessions and secrets

Servers that need a login (Mercado Livre, Shopee, Twitter) store cookies and browser profiles next to the server. These files are ignored by git and must never be committed.
