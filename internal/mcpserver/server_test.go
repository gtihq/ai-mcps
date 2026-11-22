package mcpserver

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/internal/omap"
)

func probeServer() *Server {
	return New("probe", &Tool{
		Name:        "ping",
		Description: "Ping the server.\n",
		Properties: []NamedProperty{
			{Name: "x", Prop: Property{Type: "integer", Title: "X", Default: 1}},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			x := 1
			if v, ok := args["x"].(float64); ok {
				x = int(v)
			}
			return omap.From("ok", true, "x", x), nil
		},
	})
}

func TestInitializeMatchesFastMCP(t *testing.T) {
	line := probeServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	want := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{"experimental":{},"prompts":{"listChanged":false},"resources":{"subscribe":false,"listChanged":false},"tools":{"listChanged":false}},"serverInfo":{"name":"probe","version":"1.30.0"}}}` + "\n"
	if line != want {
		t.Fatalf("initialize mismatch\n got: %s\nwant: %s", line, want)
	}
}

func TestToolsListMatchesFastMCP(t *testing.T) {
	line := probeServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	want := `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping","description":"Ping the server.\n","inputSchema":{"properties":{"x":{"default":1,"title":"X","type":"integer"}},"required":[],"title":"pingArguments","type":"object"}}]}}` + "\n"
	// FastMCP omits "required" when empty. Accept that difference only.
	wantNoRequired := `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping","description":"Ping the server.\n","inputSchema":{"properties":{"x":{"default":1,"title":"X","type":"integer"}},"title":"pingArguments","type":"object"}}]}}` + "\n"
	if line != want && line != wantNoRequired {
		t.Fatalf("tools/list mismatch\n got: %s\nwant: %s", line, wantNoRequired)
	}
}

func TestToolsCallDictResult(t *testing.T) {
	line := probeServer().Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ping","arguments":{}}}`))
	want := `{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"{\n  \"ok\": true,\n  \"x\": 1\n}"}],"isError":false}}` + "\n"
	if line != want {
		t.Fatalf("tools/call mismatch\n got: %s\nwant: %s", line, want)
	}
}

func TestRunWith(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ping","arguments":{}}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := probeServer().RunWith(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(out.String(), "\n"); lines != 2 {
		t.Fatalf("want 2 response lines, got %d: %q", lines, out.String())
	}
}
