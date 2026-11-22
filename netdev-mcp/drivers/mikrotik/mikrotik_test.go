package mikrotik

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
)

func TestWordCodecRoundTrip(t *testing.T) {
	values := []string{"", "a", "hello", strings.Repeat("x", 200), strings.Repeat("y", 20000)}
	var buf bytes.Buffer
	for _, value := range values {
		if err := writeWord(&buf, value); err != nil {
			t.Fatal(err)
		}
	}
	reader := bufio.NewReader(&buf)
	for _, want := range values {
		length, err := readLength(reader)
		if err != nil {
			t.Fatal(err)
		}
		data := make([]byte, length)
		if _, err := readFull(reader, data); err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("word mismatch: got %q want %q", string(data), want)
		}
	}
}

// readFull is a tiny helper that avoids importing io in the test file.
func readFull(reader *bufio.Reader, data []byte) (int, error) {
	filled := 0
	for filled < len(data) {
		b, err := reader.ReadByte()
		if err != nil {
			return filled, err
		}
		data[filled] = b
		filled++
	}
	return filled, nil
}

func TestTranslateCommand(t *testing.T) {
	cases := []struct {
		command string
		path    string
		method  string
	}{
		{"/ip/firewall/filter/print", "/ip/firewall/filter", http.MethodGet},
		{"/interface", "/interface", http.MethodGet},
		{"/ip/firewall/filter/add", "/ip/firewall/filter/add", http.MethodPost},
	}
	for _, tc := range cases {
		path, method, _, _ := translateCommand(tc.command, map[string]any{})
		if path != tc.path || method != tc.method {
			t.Errorf("%s: got %s %s, want %s %s", tc.command, method, path, tc.method, tc.path)
		}
	}
}

func TestParseRouterOSPing(t *testing.T) {
	output := " SEQ HOST        SIZE TTL TIME   STATUS\n" +
		"   0 8.8.8.8       56  64 10ms\n" +
		"   1 8.8.8.8       56  64 12ms\n" +
		"   2 8.8.8.8       56  64  0ms   timeout\n"
	result := parseRouterOSPing("8.8.8.8", output)
	if result.Sent != 3 || result.Received != 2 {
		t.Fatalf("unexpected ping counts: %+v", result)
	}
	if result.Loss != float64(1)/float64(3)*100 {
		t.Fatalf("unexpected loss: %v", result.Loss)
	}
	if result.AvgRTT != "11.00ms" {
		t.Fatalf("unexpected average RTT: %v", result.AvgRTT)
	}
}

func TestRESTSystemInfoAndInterfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			t.Errorf("bad basic auth: %q %q", user, pass)
		}
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/rest/system/resource":
			_, _ = w.Write([]byte(`[{"version":"7.15.3","uptime":"2w3d","cpu-load":"12","free-memory":"100","total-memory":"512","board-name":"RB5009UG"}]`))
		case "/rest/system/identity":
			_, _ = w.Write([]byte(`[{"name":"core-gw"}]`))
		case "/rest/interface":
			_, _ = w.Write([]byte(`[{"name":"ether1","type":"ether","mac-address":"AA:BB","running":"true","mtu":"1500"}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := device.Config{Name: "rb", Vendor: device.VendorMikroTik, Host: "127.0.0.1", Username: "admin", Password: "secret"}
	built, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	driver := built.(*Driver)
	driver.baseURL = srv.URL + "/rest"

	info, err := driver.SystemInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "7.15.3" || info.Hostname != "core-gw" || info.Model != "RB5009UG" {
		t.Fatalf("unexpected system info: %+v", info)
	}

	interfaces, err := driver.Interfaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(interfaces) != 1 || interfaces[0].Name != "ether1" || interfaces[0].Status != "up" {
		t.Fatalf("unexpected interfaces: %+v", interfaces)
	}
}

func TestAPIClientLoginAndPrint(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = listener.Close()
	}()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() {
			_ = conn.Close()
		}()
		reader := bufio.NewReader(conn)
		for {
			sentence, err := readAPISentence(reader)
			if err != nil {
				return
			}
			if len(sentence) == 0 {
				continue
			}
			switch sentence[0] {
			case "/login":
				writeAPISentence(t, conn, "!done")
			case "/system/resource/print":
				writeAPISentence(t, conn, "!re", "=version=7.15.3", "=uptime=1d")
				writeAPISentence(t, conn, "!done")
			default:
				writeAPISentence(t, conn, "!trap", "=message=unknown command")
			}
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	cfg := device.Config{
		Name: "rb", Vendor: device.VendorMikroTik, Host: "127.0.0.1",
		Port: port, Username: "admin", Password: "secret",
	}
	client := newAPIClient(cfg, defaultTimeout)
	rows, err := client.print(context.Background(), "/system/resource/print", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["version"] != "7.15.3" {
		data, _ := json.Marshal(rows)
		t.Fatalf("unexpected API rows: %s", data)
	}
	_ = client.Close()
}

func readAPISentence(reader *bufio.Reader) ([]string, error) {
	words := make([]string, 0, 8)
	for {
		length, err := readLength(reader)
		if err != nil {
			return words, err
		}
		if length == 0 {
			return words, nil
		}
		data := make([]byte, length)
		if _, err := readFull(reader, data); err != nil {
			return words, err
		}
		words = append(words, string(data))
	}
}

func writeAPISentence(t *testing.T, conn net.Conn, words ...string) {
	t.Helper()
	for _, word := range words {
		if err := writeWord(conn, word); err != nil {
			t.Error(err)
			return
		}
	}
	if err := writeWord(conn, ""); err != nil {
		t.Error(err)
	}
}
