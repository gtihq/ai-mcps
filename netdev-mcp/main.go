// Command netdev-mcp is a single MCP server that manages heterogeneous
// network devices: MikroTik RouterOS, ZTE ZXHN home gateways, Huawei home
// gateways and any switch reachable through a Cisco-like or VRP-like SSH CLI.
package main

import (
	"flag"
	"log"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
)

func main() {
	inventory := flag.String("inventory", "", "path to the device inventory JSON file")
	listen := flag.String("listen", "", "serve JSON-RPC over HTTP on this address, for example 0.0.0.0:8765 for Tailscale access")
	authSetup := flag.Bool("setup", false, "open the temporary browser form only when no device has credentials, then exit")
	forceSetup := flag.Bool("force-setup", false, "open the form even when the inventory already has credentials")
	flag.Parse()

	configs, err := loadInventory(*inventory)
	if err != nil {
		log.Fatalf("netdev-mcp: %v", err)
	}
	log.Printf("netdev-mcp: loaded %d device(s) from %s", len(configs), inventoryPath())

	registry := newRegistry(configs)
	if *authSetup {
		if inventoryAuthenticated() && !*forceSetup {
			log.Printf("netdev-mcp: devices already authenticated, skipping setup (use -force-setup to override)")
			return
		}
		setup, urls, err := startSetup(registry)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("netdev-mcp: temporary setup page (expires in 60 minutes):")
		for _, url := range urls {
			log.Printf("  %s", url)
		}
		<-setup.done
		log.Printf("netdev-mcp: setup finished, inventory at %s", inventoryPath())
		return
	}

	server := mcpserver.New("netdev", tools(registry)...)
	if err := runServer(*listen, server); err != nil {
		log.Fatal(err)
	}
}
