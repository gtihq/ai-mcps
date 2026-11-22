// Command kabum-mcp is the Go replacement for the Python KaBuM! MCP server.
package main

import (
	"log"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
)

func main() {
	server := mcpserver.New("kabum", tools()...)
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
