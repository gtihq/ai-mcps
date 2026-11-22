// Package mcpserver implements the small subset of the Model Context Protocol
// used by the GTI MCP servers: JSON-RPC 2.0 over newline-delimited stdio,
// initialize, tools/list and tools/call.
//
// The wire format mirrors mcp 1.30.0 FastMCP, including the tool schemas and
// the content-block shape, so agents see exactly what they saw with Python.
package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gtihq/ai-mcps/internal/omap"
)

// Version is the version FastMCP reported in initialize.
const Version = "1.30.0"

// DefaultProtocolVersion is used when the client does not send one.
const DefaultProtocolVersion = "2025-03-26"

// Property describes one input schema property.
type Property struct {
	Type        string
	Title       string
	Default     any
	Description string
	Enum        []string
	// Nullable emits anyOf [{"type": T}, {"type": "null"}] as the first key
	// and omits the top-level "type", matching Python's T | None annotations.
	Nullable bool
	// HasDefault emits a "default" key when Default is nil, so Optional
	// parameters carry "default": null on the wire.
	HasDefault bool
}

// Tool is one MCP tool.
type Tool struct {
	Name        string
	Description string
	Properties  []NamedProperty
	Required    []string
	// OutputSchema adds the string-result output schema FastMCP emits for
	// tools whose Python return annotation is str.
	OutputSchema bool
	Handler      func(ctx context.Context, args map[string]any) (any, error)
}

// NamedProperty pairs a property name with its schema.
type NamedProperty struct {
	Name string
	Prop Property
}

// Server is an MCP stdio server.
type Server struct {
	Name    string
	Tools   []*Tool
	version string
}

// New creates a server. version defaults to FastMCP's version.
func New(name string, tools ...*Tool) *Server {
	return &Server{Name: name, Tools: tools, version: Version}
}

func (s *Server) tool(name string) *Tool {
	for _, t := range s.Tools {
		if t.Name == name {
			return t
		}
	}
	return nil
}

func propJSON(name string, p Property) *omap.M {
	m := omap.New()
	typ := p.Type
	if typ == "" {
		typ = "string"
	}
	if p.Nullable {
		anyOf := []any{omap.From("type", typ), omap.From("type", "null")}
		m.Set("anyOf", anyOf)
	}
	if p.Default != nil || p.HasDefault {
		m.Set("default", p.Default)
	}
	if p.Description != "" {
		m.Set("description", p.Description)
	}
	if len(p.Enum) > 0 {
		m.Set("enum", p.Enum)
	}
	title := p.Title
	if title == "" {
		parts := strings.Split(name, "_")
		for i, part := range parts {
			if part == "" {
				continue
			}
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
		title = strings.Join(parts, "")
	}
	m.Set("title", title)
	if !p.Nullable {
		m.Set("type", typ)
	}
	return m
}

func (s *Server) toolJSON(t *Tool) *omap.M {
	schema := omap.New()
	props := omap.New()
	for _, np := range t.Properties {
		props.Set(np.Name, propJSON(np.Name, np.Prop))
	}
	schema.Set("properties", props)
	if len(t.Required) > 0 {
		req := make([]any, 0, len(t.Required))
		for _, r := range t.Required {
			req = append(req, r)
		}
		schema.Set("required", req)
	}
	schema.Set("title", t.Name+"Arguments")
	schema.Set("type", "object")
	out := omap.New()
	out.Set("name", t.Name)
	out.Set("description", t.Description)
	out.Set("inputSchema", schema)
	if t.OutputSchema {
		result := omap.New()
		result.Set("title", "Result")
		result.Set("type", "string")
		resultProps := omap.New()
		resultProps.Set("result", result)
		outSchema := omap.New()
		outSchema.Set("properties", resultProps)
		outSchema.Set("required", []any{"result"})
		outSchema.Set("title", t.Name+"Output")
		outSchema.Set("type", "object")
		out.Set("outputSchema", outSchema)
	}
	return out
}

func response(id any, result any) *omap.M {
	return omap.From("jsonrpc", "2.0", "id", id, "result", result)
}

func errorResponse(id any, code int, message string) *omap.M {
	return omap.From(
		"jsonrpc", "2.0",
		"id", id,
		"error", omap.From("code", code, "message", message),
	)
}

func textResult(text string, isError bool) *omap.M {
	content := []any{omap.From("type", "text", "text", text)}
	return omap.From("content", content, "isError", isError)
}

func resultContent(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	if text, ok := value.(string); ok {
		return text, nil
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Handle processes one JSON-RPC request and returns the response line, or an
// empty string for notifications.
func (s *Server) Handle(ctx context.Context, line []byte) string {
	var req struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      any            `json:"id"`
		Method  string         `json:"method"`
		Params  map[string]any `json:"params"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return marshalLine(errorResponse(nil, -32700, "Parse error"))
	}
	if req.Method == "" {
		return marshalLine(errorResponse(req.ID, -32600, "Invalid Request"))
	}
	if strings.HasPrefix(req.Method, "notifications/") {
		return ""
	}
	switch req.Method {
	case "initialize":
		protocol := DefaultProtocolVersion
		if v, ok := req.Params["protocolVersion"].(string); ok && v != "" {
			protocol = v
		}
		caps := omap.From(
			"experimental", omap.New(),
			"prompts", omap.From("listChanged", false),
			"resources", omap.From("subscribe", false, "listChanged", false),
			"tools", omap.From("listChanged", false),
		)
		info := omap.From("name", s.Name, "version", s.version)
		return marshalLine(response(req.ID, omap.From(
			"protocolVersion", protocol,
			"capabilities", caps,
			"serverInfo", info,
		)))
	case "ping":
		return marshalLine(response(req.ID, omap.New()))
	case "tools/list":
		list := make([]any, 0, len(s.Tools))
		for _, t := range s.Tools {
			list = append(list, s.toolJSON(t))
		}
		return marshalLine(response(req.ID, omap.From("tools", list)))
	case "tools/call":
		name, _ := req.Params["name"].(string)
		t := s.tool(name)
		if t == nil {
			return marshalLine(errorResponse(req.ID, -32602, fmt.Sprintf("Unknown tool: %s", name)))
		}
		args := map[string]any{}
		if raw, ok := req.Params["arguments"]; ok && raw != nil {
			if a, ok := raw.(map[string]any); ok {
				args = a
			}
		}
		value, err := t.Handler(ctx, args)
		if err != nil {
			return marshalLine(response(req.ID, textResult(err.Error(), true)))
		}
		text, err := resultContent(value)
		if err != nil {
			return marshalLine(response(req.ID, textResult(err.Error(), true)))
		}
		return marshalLine(response(req.ID, textResult(text, false)))
	default:
		return marshalLine(errorResponse(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method)))
	}
}

func marshalLine(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`
	}
	return string(b) + "\n"
}

// Run reads JSON-RPC requests from stdin and writes responses to stdout until
// stdin closes. Diagnostics must go to stderr only.
func (s *Server) Run() error {
	return s.RunWith(context.Background(), os.Stdin, os.Stdout)
}

// RunWith is Run with explicit streams, used by tests.
func (s *Server) RunWith(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := bytesTrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		resp := s.Handle(ctx, line)
		if resp == "" {
			continue
		}
		if _, err := io.WriteString(out, resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
