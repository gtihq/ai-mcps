package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
)

// serveHTTP exposes the same JSON-RPC handler over HTTP. Bind to 0.0.0.0 to
// make the server reachable from other machines on the Tailscale network.
// Set NETDEV_HTTP_TOKEN to require a bearer token on every request.
func serveHTTP(address string, server *mcpserver.Server) error {
	token := os.Getenv("NETDEV_HTTP_TOKEN")
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if token != "" && r.Header.Get("authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !json.Valid(body) {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		response := server.Handle(r.Context(), body)
		if strings.TrimSpace(response) == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, strings.TrimSpace(response))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	return http.ListenAndServe(address, mux)
}

// runServer selects stdio or HTTP transport based on the listen flag.
func runServer(address string, server *mcpserver.Server) error {
	if address == "" {
		return server.Run()
	}
	return serveHTTP(address, server)
}
