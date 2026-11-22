package mikrotik

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
)

const (
	apiPort     = 8728
	apiTLSPort  = 8729
	apiIdleWait = 30 * time.Second
)

// apiSentence is one reply from the RouterOS API: a status word such as
// !re or !done plus its attribute map.
type apiSentence struct {
	Status string
	Attrs  map[string]string
}

// apiClient speaks the RouterOS binary API (words grouped into sentences,
// each sentence terminated by a zero-length word).
type apiClient struct {
	cfg     device.Config
	timeout time.Duration
	conn    net.Conn
	reader  *bufio.Reader
}

// newAPIClient builds an unconnected API client.
func newAPIClient(cfg device.Config, timeout time.Duration) *apiClient {
	return &apiClient{cfg: cfg, timeout: timeout}
}

// connect dials, authenticates and returns an error when the API service is
// unavailable or the credentials are rejected.
func (c *apiClient) connect(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	dialer := &net.Dialer{Timeout: c.timeout}
	address := c.cfg.Address(apiPort)
	if c.cfg.TLS {
		address = c.cfg.Address(apiTLSPort)
	}
	var conn net.Conn
	var err error
	if c.cfg.TLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{
			InsecureSkipVerify: c.cfg.InsecureTLS,
			ServerName:         hostOnly(c.cfg.Host),
		})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("routeros api: %w", err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)
	if err := c.login(ctx); err != nil {
		_ = c.Close()
		return err
	}
	return nil
}

// login tries the modern plaintext login first and falls back to the legacy
// MD5 challenge used by RouterOS before 6.43.
func (c *apiClient) login(ctx context.Context) error {
	replies, err := c.talk(ctx, "/login", "=name="+c.cfg.Username, "=password="+c.cfg.Password)
	if err != nil {
		return err
	}
	if !hasTrap(replies) {
		return nil
	}

	challenge, err := c.talk(ctx, "/login", "=name="+c.cfg.Username)
	if err != nil {
		return err
	}
	ret := ""
	for _, reply := range challenge {
		if v, ok := reply.Attrs["ret"]; ok {
			ret = v
			break
		}
	}
	if ret == "" {
		return fmt.Errorf("routeros api: login rejected: %s", trapMessage(replies))
	}
	seed, err := hex.DecodeString(ret)
	if err != nil {
		return fmt.Errorf("routeros api: invalid login challenge: %w", err)
	}
	hash := md5.New()
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(c.cfg.Password))
	_, _ = hash.Write(seed)
	response := "00" + hex.EncodeToString(hash.Sum(nil))

	replies, err = c.talk(ctx, "/login", "=name="+c.cfg.Username, "=response="+response)
	if err != nil {
		return err
	}
	if hasTrap(replies) {
		return fmt.Errorf("routeros api: login rejected: %s", trapMessage(replies))
	}
	return nil
}

// talk sends one sentence and reads replies until !done. Query words may be
// supplied as additional attributes by callers.
func (c *apiClient) talk(ctx context.Context, words ...string) ([]apiSentence, error) {
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	if err := c.writeSentence(ctx, words); err != nil {
		return nil, err
	}
	out := make([]apiSentence, 0, 4)
	for {
		sentence, err := c.readSentence(ctx)
		if err != nil {
			return nil, err
		}
		if len(sentence) == 0 {
			continue
		}
		reply := apiSentence{Status: sentence[0], Attrs: make(map[string]string)}
		for _, word := range sentence[1:] {
			if !strings.HasPrefix(word, "=") {
				reply.Attrs[word] = ""
				continue
			}
			rest := word[1:]
			if i := strings.Index(rest, "="); i >= 0 {
				reply.Attrs["="+rest[:i]] = rest[i+1:]
			} else {
				reply.Attrs["="+rest] = ""
			}
		}
		out = append(out, reply)
		if reply.Status == "!done" || reply.Status == "!fatal" {
			return out, nil
		}
	}
}

// print runs a /.../print command and returns one map per !re reply.
func (c *apiClient) print(ctx context.Context, path string, query map[string]any) ([]map[string]any, error) {
	words := []string{path}
	if len(query) > 0 {
		for k, v := range query {
			switch strings.ToLower(k) {
			case "proplist":
				words = append(words, "=.proplist="+device.MapString(v))
			case "follow":
				words = append(words, "=follow="+device.MapString(v))
			}
		}
	}
	replies, err := c.talk(ctx, words...)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(replies))
	for _, reply := range replies {
		switch reply.Status {
		case "!re":
			row := make(map[string]any, len(reply.Attrs))
			for k, v := range reply.Attrs {
				row[strings.TrimPrefix(k, "=")] = v
			}
			rows = append(rows, row)
		case "!trap", "!fatal":
			return nil, fmt.Errorf("routeros api: %s", trapMessage(replies))
		}
	}
	return rows, nil
}

// run executes a raw API sentence and returns its replies as ordered rows.
func (c *apiClient) run(ctx context.Context, command string, params map[string]any) (any, error) {
	words := []string{command}
	for k, v := range params {
		switch strings.ToLower(k) {
		case "proplist", "follow":
			words = append(words, "="+k+"="+device.MapString(v))
		default:
			words = append(words, "="+k+"="+device.MapString(v))
		}
	}
	replies, err := c.talk(ctx, words...)
	if err != nil {
		return nil, err
	}
	rows := make([]any, 0, len(replies))
	for _, reply := range replies {
		row := make(map[string]any, len(reply.Attrs)+1)
		row["status"] = reply.Status
		for k, v := range reply.Attrs {
			row[k] = v
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Close releases the TCP connection.
func (c *apiClient) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	c.reader = nil
	return err
}

// writeSentence writes each word followed by a zero-length terminator.
func (c *apiClient) writeSentence(ctx context.Context, words []string) error {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(dl)
	} else {
		_ = c.conn.SetWriteDeadline(time.Now().Add(apiIdleWait))
	}
	for _, word := range words {
		if err := writeWord(c.conn, word); err != nil {
			return err
		}
	}
	return writeWord(c.conn, "")
}

// readSentence reads words until the zero-length terminator.
func (c *apiClient) readSentence(ctx context.Context) ([]string, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.conn.SetReadDeadline(dl)
	} else {
		_ = c.conn.SetReadDeadline(time.Now().Add(apiIdleWait))
	}
	words := make([]string, 0, 8)
	for {
		length, err := readLength(c.reader)
		if err != nil {
			return nil, err
		}
		if length == 0 {
			return words, nil
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(c.reader, buf); err != nil {
			return nil, err
		}
		words = append(words, string(buf))
	}
}

// writeWord encodes a word with the RouterOS compact length prefix.
func writeWord(w io.Writer, word string) error {
	data := []byte(word)
	length := len(data)
	var prefix []byte
	switch {
	case length < 0x80:
		prefix = []byte{byte(length)}
	case length < 0x4000:
		v := uint16(length) | 0x8000
		prefix = []byte{byte(v >> 8), byte(v)}
	case length < 0x200000:
		v := uint32(length) | 0xc00000
		prefix = []byte{byte(v >> 16), byte(v >> 8), byte(v)}
	case length < 0x10000000:
		v := uint32(length) | 0xe0000000
		prefix = []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	default:
		if length > 0xffffffff {
			return fmt.Errorf("routeros api: word too long: %d", length)
		}
		prefix = []byte{0xf0, byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)}
	}
	if _, err := w.Write(prefix); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// readLength decodes the compact length prefix of the next word.
func readLength(r io.ByteReader) (int, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	switch {
	case b&0x80 == 0:
		return int(b), nil
	case b&0xc0 == 0x80:
		next, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		return int(b&0x3f)<<8 | int(next), nil
	case b&0xe0 == 0xc0:
		second, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		third, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		return int(b&0x1f)<<16 | int(second)<<8 | int(third), nil
	case b&0xf0 == 0xe0:
		second, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		third, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		fourth, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		return int(b&0x0f)<<24 | int(second)<<16 | int(third)<<8 | int(fourth), nil
	default:
		parts := make([]byte, 4)
		for i := range parts {
			v, err := r.ReadByte()
			if err != nil {
				return 0, err
			}
			parts[i] = v
		}
		return int(uint32(parts[0])<<24 | uint32(parts[1])<<16 | uint32(parts[2])<<8 | uint32(parts[3])), nil
	}
}

// hasTrap reports whether any reply is an error.
func hasTrap(replies []apiSentence) bool {
	for _, reply := range replies {
		if reply.Status == "!trap" || reply.Status == "!fatal" {
			return true
		}
	}
	return false
}

// trapMessage extracts a readable error from a trap reply.
func trapMessage(replies []apiSentence) string {
	for _, reply := range replies {
		if reply.Status == "!trap" || reply.Status == "!fatal" {
			if msg, ok := reply.Attrs["message"]; ok {
				return msg
			}
			return reply.Status
		}
	}
	return "unknown error"
}

// hostOnly strips brackets and an optional port from a host string.
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return strings.Trim(host, "[]")
}
