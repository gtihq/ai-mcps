// Command mercadolivre-mcp is the Go replacement for the Python Mercado Livre MCP server.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
)

func main() {
	login := flag.Bool("login", false, "run the one-time Mercado Livre login helper")
	flag.Parse()
	if *login {
		if err := runLogin(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	server := mcpserver.New("mercadolivre", tools()...)
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
