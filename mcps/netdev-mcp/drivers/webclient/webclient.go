// Package webclient provides the HTTP session, CSRF handling and response
// parsing shared by the consumer-router drivers (ZTE and Huawei).
package webclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/mcps/netdev-mcp/device"
	"golang.org/x/net/html"
)

const defaultTimeout = 15 * time.Second

var (
	metaTokenRE   = regexp.MustCompile(`(?is)<meta[^>]+name=["']csrf_token["'][^>]+content=["']([^"']+)["']`)
	hiddenTokenRE = regexp.MustCompile(`(?is)<input[^>]+name=["']csrf_token["'][^>]+value=["']([^"']+)["']`)
)

// Client is an authenticated web session against one device.
type Client struct {
	Cfg          device.Config
	HTTP         *http.Client
	BaseURL      string
	Token        string
	LoggedIn     bool
	Status       int
	ExtraHeaders map[string]string
	timeout      time.Duration
}

// New builds a web client. The login request itself is deferred until first
// use so constructing a driver never touches the network.
func New(cfg device.Config, defaultPort int, useTLS bool) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Duration(defaultTimeout)
	scheme := "http"
	if useTLS || cfg.TLS {
		scheme = "https"
	}
	return &Client{
		Cfg:     cfg,
		BaseURL: scheme + "://" + cfg.Address(defaultPort),
		timeout: timeout,
		HTTP: &http.Client{
			Timeout: timeout,
			Jar:     jar,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureTLS},
			},
		},
	}, nil
}

// URL resolves a path against the device base URL.
func (c *Client) URL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return c.BaseURL + "/" + strings.TrimPrefix(path, "/")
}

// Request performs one HTTP request with the current CSRF token applied.
func (c *Client) Request(ctx context.Context, method, path, contentType string, body []byte) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.URL(path), bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("accept", "application/json, text/xml, text/html, */*")
	req.Header.Set("x-requested-with", "XMLHttpRequest")
	if c.Token != "" {
		req.Header.Set(c.csrfHeader(), c.Token)
	}
	if contentType != "" {
		req.Header.Set("content-type", contentType)
	}
	for k, v := range c.ExtraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	c.Status = resp.StatusCode
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, err
	}
	if headerToken := resp.Header.Get(c.csrfHeader()); headerToken != "" {
		c.Token = headerToken
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, resp.Header, fmt.Errorf("http %d em %s %s: %s", resp.StatusCode, method, path, strings.TrimSpace(string(data)))
	}
	if token := TokenFromBody(data); token != "" {
		c.Token = token
	}
	return data, resp.Header, nil
}

// csrfHeader returns the configured header name or the Huawei/ZTE default.
func (c *Client) csrfHeader() string {
	if v, ok := c.Cfg.Extra["csrf_header"]; ok && device.MapString(v) != "" {
		return device.MapString(v)
	}
	return "__RequestVerificationToken"
}

// Get fetches a path.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	data, _, err := c.Request(ctx, http.MethodGet, path, "", nil)
	return data, err
}

// PostForm submits urlencoded form data.
func (c *Client) PostForm(ctx context.Context, path string, values url.Values) ([]byte, error) {
	data, _, err := c.Request(ctx, http.MethodPost, path, "application/x-www-form-urlencoded", []byte(values.Encode()))
	return data, err
}

// PostXML submits an XML request body.
func (c *Client) PostXML(ctx context.Context, path, body string) ([]byte, error) {
	data, _, err := c.Request(ctx, http.MethodPost, path, "text/xml", []byte(body))
	return data, err
}

// PostJSON submits a JSON request body.
func (c *Client) PostJSON(ctx context.Context, path string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	result, _, err := c.Request(ctx, http.MethodPost, path, "application/json", data)
	return result, err
}

// TokenFromBody extracts a CSRF token from HTML meta/input tags or a JSON
// response. Both router families expose the token in one of those shapes.
func TokenFromBody(data []byte) string {
	if m := metaTokenRE.FindSubmatch(data); len(m) > 1 {
		return string(m[1])
	}
	if m := hiddenTokenRE.FindSubmatch(data); len(m) > 1 {
		return string(m[1])
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err == nil {
		for _, key := range []string{"csrf_token", "token", "TokInfo", "__RequestVerificationToken"} {
			if v, ok := payload[key]; ok {
				if s := device.MapString(v); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// XMLMap flattens an XML document into a string map. Nested tags are joined
// with dots so callers can address values such as response.State.
func XMLMap(data []byte) map[string]any {
	out := make(map[string]any)
	dec := xml.NewDecoder(bytes.NewReader(data))
	path := make([]string, 0, 4)
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			path = append(path, t.Name.Local)
		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text != "" && len(path) > 0 {
				out[strings.Join(path, ".")] = text
			}
		case xml.EndElement:
			if len(path) > 0 {
				path = path[:len(path)-1]
			}
		}
	}
	return out
}

// XMLRequest builds the standard router XML request envelope.
func XMLRequest(values map[string]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><request>`)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		b.WriteString("<")
		b.WriteString(xmlEscape(k))
		b.WriteString(">")
		b.WriteString(xmlEscape(values[k]))
		b.WriteString("</")
		b.WriteString(xmlEscape(k))
		b.WriteString(">")
	}
	b.WriteString("</request>")
	return b.String()
}

// ParseRows converts a device response into table rows. It accepts a JSON
// array, an object wrapping one, or plain HTML tables.
func ParseRows(data []byte) []map[string]any {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '[' || trimmed[0] == '{' {
		var value any
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.UseNumber()
		if err := dec.Decode(&value); err == nil {
			return jsonRows(value)
		}
	}
	return htmlRows(trimmed)
}

// jsonRows normalizes the common JSON envelope shapes.
func jsonRows(value any) []map[string]any {
	if items := device.MapSlice(value); items != nil {
		return mapsOnly(items)
	}
	obj := device.MapAny(value)
	if obj == nil {
		return nil
	}
	for _, key := range []string{"data", "rows", "list", "result", "items", "List"} {
		if rows := jsonRows(obj[key]); len(rows) > 0 {
			return rows
		}
	}
	if len(obj) > 0 {
		return []map[string]any{obj}
	}
	return nil
}

// mapsOnly drops scalar elements from a JSON array.
func mapsOnly(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m := device.MapAny(item); m != nil {
			out = append(out, m)
		}
	}
	return out
}

// htmlRows extracts the first HTML table with a header row, falling back to
// treating the first row as data.
func htmlRows(data []byte) []map[string]any {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	var tables []*html.Node
	collectTables(doc, &tables)
	for _, table := range tables {
		if rows := tableRows(table); len(rows) > 0 {
			return rows
		}
	}
	return nil
}

// collectTables walks a node tree and appends every table element.
func collectTables(node *html.Node, out *[]*html.Node) {
	if node.Type == html.ElementNode && node.Data == "table" {
		*out = append(*out, node)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectTables(child, out)
	}
}

// tableRows turns table rows into maps using the first row as header when it
// contains th cells.
func tableRows(table *html.Node) []map[string]any {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "tr" {
			cells := make([]string, 0)
			var cellWalk func(*html.Node)
			cellWalk = func(child *html.Node) {
				if child.Type == html.ElementNode && (child.Data == "td" || child.Data == "th") {
					cells = append(cells, nodeText(child))
					return
				}
				for c := child.FirstChild; c != nil; c = c.NextSibling {
					cellWalk(c)
				}
			}
			cellWalk(node)
			if len(cells) > 0 {
				rows = append(rows, cells)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(table)
	if len(rows) < 2 {
		return nil
	}
	header := rows[0]
	hasHeader := false
	for _, cell := range header {
		if cell != "" {
			hasHeader = true
		}
	}
	out := make([]map[string]any, 0, len(rows))
	start := 0
	if hasHeader {
		start = 1
	} else {
		header = make([]string, len(rows[0]))
		for i := range header {
			header[i] = fmt.Sprintf("column_%d", i+1)
		}
	}
	for _, row := range rows[start:] {
		entry := make(map[string]any, len(header))
		for i, value := range row {
			if i >= len(header) {
				break
			}
			entry[header[i]] = value
		}
		out = append(out, entry)
	}
	return out
}

// nodeText returns the visible text of a node.
func nodeText(node *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(b.String()), " ")
}

// sortStrings sorts a slice in place. Kept local to avoid importing sort in
// every driver that builds XML bodies.
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// xmlEscape escapes the five XML predefined entities.
func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	return strings.ReplaceAll(value, "'", "&apos;")
}

// xmlNode is a minimal tree used to find repeated XML elements.
type xmlNode struct {
	name     string
	text     string
	children []*xmlNode
}

// XMLRows extracts repeated XML elements as maps. Huawei gateways wrap lists
// in tags such as <StationInfos><StationInfo>..., so the deepest repeated
// group of sibling elements is the row set.
func XMLRows(data []byte) []map[string]any {
	dec := xml.NewDecoder(bytes.NewReader(data))
	root := parseXMLNode(dec, xml.StartElement{})
	if root == nil {
		return nil
	}
	var best []*xmlNode
	var walk func(*xmlNode)
	walk = func(node *xmlNode) {
		groups := make(map[string][]*xmlNode)
		for _, child := range node.children {
			groups[child.name] = append(groups[child.name], child)
		}
		for _, group := range groups {
			if len(group) >= 2 && len(group[0].children) > 0 {
				if len(best) == 0 || len(group[0].children) > len(best[0].children) {
					best = group
				}
			}
		}
		for _, child := range node.children {
			walk(child)
		}
	}
	walk(root)
	if len(best) == 0 {
		best = findFirstRowNode(root)
	}
	out := make([]map[string]any, 0, len(best))
	for _, row := range best {
		entry := make(map[string]any, len(row.children))
		for _, child := range row.children {
			if len(child.children) == 0 {
				if text := strings.TrimSpace(child.text); text != "" {
					entry[child.name] = text
				}
			}
		}
		if len(entry) > 0 {
			out = append(out, entry)
		}
	}
	return out
}

// parseXMLNode recursively consumes one XML element.
func parseXMLNode(dec *xml.Decoder, start xml.StartElement) *xmlNode {
	node := &xmlNode{name: start.Name.Local}
	for {
		tok, err := dec.Token()
		if err != nil {
			return node
		}
		switch value := tok.(type) {
		case xml.CharData:
			node.text += string(value)
		case xml.StartElement:
			child := parseXMLNode(dec, value)
			if child != nil {
				node.children = append(node.children, child)
			}
		case xml.EndElement:
			return node
		}
	}
}

// findFirstRowNode returns the first node that has scalar children, used for
// single-row XML responses.
func findFirstRowNode(node *xmlNode) []*xmlNode {
	for _, child := range node.children {
		if len(child.children) > 0 {
			hasScalar := false
			for _, grandchild := range child.children {
				if len(grandchild.children) == 0 {
					hasScalar = true
					break
				}
			}
			if hasScalar {
				return []*xmlNode{child}
			}
			if found := findFirstRowNode(child); len(found) > 0 {
				return found
			}
		}
	}
	return nil
}

// Method extracts an HTTP method from a raw command parameter map.
func Method(params map[string]any) string {
	method := strings.ToUpper(device.MapString(params["method"]))
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
		return method
	default:
		return http.MethodGet
	}
}

// HeaderMap converts http.Header into a plain string map.
func HeaderMap(header http.Header) map[string]string {
	out := make(map[string]string, len(header))
	for key, values := range header {
		if len(values) > 0 {
			out[key] = values[0]
		}
	}
	return out
}
