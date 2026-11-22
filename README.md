# GTI AI MCPs

MCP servers used by the GTI team's AI coding agents. Each server is a standalone Python folder with its own `requirements.txt` and README.

## Servers

| Folder | What it does | Entry point |
|--------|--------------|-------------|
| [`kabum-mcp`](kabum-mcp/README.md) | Product search and shipping quotes on KaBuM! | `server.py` |
| [`mercadolivre-mcp`](mercadolivre-mcp/README.md) | Product search, shipping quotes, and orders on Mercado Livre | `server.py` |
| [`shopee-mcp`](shopee-mcp/README.md) | Products, deals, shops, reviews, and orders on Shopee | `mcp_server.py` |
| [`twitter-mcp`](twitter-mcp/README.md) | Read and post on X (Twitter) through a logged-in headless browser | `server.py` |
| [`zoom-mcp`](zoom-mcp/README.md) | Product search on the Zoom price comparator | `server.py` |

## Install

The easiest way is the `gti` skill from [gtihq/ai-skills](https://github.com/gtihq/ai-skills). `/gti update` downloads this repository, keeps one managed copy of each server with its own Python environment, and points every agent's MCP configuration at it.

To run one server by hand, open its folder and follow its README:

```bash
git clone https://github.com/gtihq/ai-mcps.git
cd ai-mcps/kabum-mcp
python -m pip install -r requirements.txt
python server.py
```

## Sessions and secrets

Servers that need a login (Mercado Livre, Shopee, Twitter) store cookies and browser profiles next to the server. These files are ignored by git and must never be committed.
