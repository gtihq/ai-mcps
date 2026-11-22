// Command zoom-mcp is the Go replacement for the Python Zoom MCP server.
package main

import (
	"log"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
)

func main() {
	server := mcpserver.New("zoom", tools()...)
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
