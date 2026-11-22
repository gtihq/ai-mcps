package huawei

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
)

func TestEncodedPasswordType4(t *testing.T) {
	got := encodedPassword("admin", "pass", "token", "4")
	if got == "" || got == "pass" {
		t.Fatalf("unexpected encoded password %q", got)
	}
	if encodedPassword("admin", "pass", "token", "0") != "pass" {
		t.Fatal("password type 0 must stay plaintext")
	}
}

func TestHuaweiLoginAndWiFiClients(t *testing.T) {
	var loginBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/webserver/SesTokInfo":
			_, _ = io.WriteString(w, `<?xml version="1.0"?><response><SesInfo>session-abc</SesInfo><TokInfo>token-xyz</TokInfo></response>`)
		case "/api/user/login":
			if r.Header.Get("__RequestVerificationToken") != "token-xyz" {
				t.Errorf("missing CSRF token on login: %q", r.Header.Get("__RequestVerificationToken"))
			}
			if r.Header.Get("Cookie") != "SessionID=session-abc" {
				t.Errorf("missing session cookie on login: %q", r.Header.Get("Cookie"))
			}
			data, _ := io.ReadAll(r.Body)
			loginBody = string(data)
			_, _ = io.WriteString(w, `<?xml version="1.0"?><response>OK</response>`)
		case "/api/wlan/station/infos":
			_, _ = io.WriteString(w, `<?xml version="1.0"?><response><StationInfos>`+
				`<StationInfo><MACAddress>AA:BB:CC:DD</MACAddress><IPAddress>192.168.3.20</IPAddress><SSID>Home</SSID><SignalStrength>-45</SignalStrength></StationInfo>`+
				`<StationInfo><MACAddress>11:22:33:44</MACAddress><IPAddress>192.168.3.21</IPAddress><SSID>Guest</SSID><SignalStrength>-60</SignalStrength></StationInfo>`+
				`</StationInfos></response>`)
		case "/api/system/routerstatus":
			_, _ = io.WriteString(w, `<?xml version="1.0"?><response><HostName>Huawei-BE3</HostName><SoftwareVersion>1.0.0</SoftwareVersion></response>`)
		case "/api/device/control":
			_, _ = io.WriteString(w, `<?xml version="1.0"?><response>OK</response>`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := device.Config{
		Name: "be3", Vendor: device.VendorHuawei, Host: "127.0.0.1",
		Username: "admin", Password: "pass",
	}
	built, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	driver := built.(*Driver)
	driver.web.BaseURL = srv.URL

	clients, err := driver.WiFiClients(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 2 {
		t.Fatalf("want 2 clients, got %d", len(clients))
	}
	if clients[0].MAC != "AA:BB:CC:DD" || clients[1].SSID != "Guest" || clients[0].Signal != "-45" {
		t.Fatalf("unexpected clients: %+v", clients)
	}
	if !strings.Contains(loginBody, "<password_type>4</password_type>") {
		t.Fatalf("unexpected login body: %s", loginBody)
	}

	info, err := driver.SystemInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Hostname != "Huawei-BE3" || info.Version != "1.0.0" {
		t.Fatalf("unexpected system info: %+v", info)
	}

	if err := driver.Reboot(context.Background()); err != nil {
		t.Fatal(err)
	}
}
