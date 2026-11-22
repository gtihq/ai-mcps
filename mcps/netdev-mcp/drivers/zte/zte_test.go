package zte

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gtihq/ai-mcps/mcps/netdev-mcp/device"
)

func TestZTELoginAndTables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("_type") == "loginData" && r.URL.Query().Get("_tag") == "login_token" {
			_, _ = io.WriteString(w, `<ajax_response_xml_root>123456</ajax_response_xml_root>`)
			return
		}
		if r.URL.Query().Get("_type") == "loginData" && r.URL.Query().Get("_tag") == "login_entry" {
			_, _ = io.WriteString(w, `{"sess_token":"abc"}`)
			return
		}
		if r.URL.Query().Get("_type") == "menuView" && r.URL.Query().Get("_tag") == "lanMgrIpv4" {
			_, _ = io.WriteString(w, `<html>LAN</html>`)
			return
		}
		if r.URL.Query().Get("_type") == "menuData" && r.URL.Query().Get("_tag") == "dhcp4s_dhcphostinfo_m.lua" {
			_, _ = io.WriteString(w, `<ajax_response_xml_root><IF_ERRORSTR>SUCC</IF_ERRORSTR></ajax_response_xml_root>`)
			return
		}
		if r.URL.Query().Get("_type") == "menuData" && r.URL.Query().Get("_tag") == "dhcp4s_dhcpbinding_m.lua" {
			_, _ = io.WriteString(w, `<ajax_response_xml_root><OBJ_DHCPBIND_ID><Instance>`+
				`<ParaName>OBJ_DHCPBIND_ID.Name</ParaName><ParaValue>laptop</ParaValue>`+
				`<ParaName>OBJ_DHCPBIND_ID.MACAddr</ParaName><ParaValue>AA:BB</ParaValue>`+
				`<ParaName>OBJ_DHCPBIND_ID.IPAddr</ParaName><ParaValue>192.168.1.10</ParaValue>`+
				`</Instance></OBJ_DHCPBIND_ID></ajax_response_xml_root>`)
			return
		}
		switch {
		case r.URL.Path == "/":
			_, _ = io.WriteString(w, `<html><input name="Frm_Username"></html>`)
		case r.URL.Path == "/login.html":
			w.Header().Set("set-cookie", "SESSIONID=abc123; Path=/")
			_, _ = io.WriteString(w, `<html><head><meta name="csrf_token" content="csrf-zte"/></head></html>`)
		case r.URL.Path == "/login.lua":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("username") != "admin" || r.Form.Get("password") != "secret" {
				t.Errorf("bad credentials: %v", r.Form)
			}
			if r.Header.Get("csrf_token") == "" && r.Form.Get("csrf_token") == "" {
				t.Error("missing CSRF token")
			}
			_, _ = io.WriteString(w, `<html><body>Welcome admin</body></html>`)
		case r.URL.Path == "/getpage.lua" && r.URL.Query().Get("pid") == "1000":
			_, _ = io.WriteString(w, `<table><tr><th>HostName</th><th>SoftwareVersion</th><th>UpTime</th></tr>`+
				`<tr><td>ZXHN-H199A</td><td>V1.2.3</td><td>86400</td></tr></table>`)
		case r.URL.Path == "/getpage.lua" && r.URL.Query().Get("pid") == "1002":
			_, _ = io.WriteString(w, `<table><tr><th>HostName</th><th>MAC</th><th>IP</th></tr>`+
				`<tr><td>laptop</td><td>AA:BB</td><td>192.168.1.10</td></tr>`+
				`<tr><td>phone</td><td>CC:DD</td><td>192.168.1.11</td></tr></table>`)
		default:
			t.Errorf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := device.Config{
		Name: "h199a", Vendor: device.VendorZTE, Host: "127.0.0.1",
		Username: "admin", Password: "secret",
	}
	built, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	driver := built.(*Driver)
	driver.web.BaseURL = srv.URL

	info, err := driver.SystemInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Hostname != "ZXHN-H199A" || info.Version != "V1.2.3" {
		t.Fatalf("unexpected system info: %+v", info)
	}

	leases, err := driver.DHCPLeases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 1 || leases[0].IP != "192.168.1.10" || leases[0].Hostname != "laptop" {
		t.Fatalf("unexpected leases: %+v", leases)
	}
}

func TestParseMenuInstances(t *testing.T) {
	data := []byte(`<ajax_response_xml_root><OBJ_DHCPBIND_ID><Instance>` +
		`<ParaName>OBJ_DHCPBIND_ID.IPAddr</ParaName><ParaValue>192.168.2.10</ParaValue>` +
		`<ParaName>OBJ_DHCPBIND_ID.MACAddr</ParaName><ParaValue>9c:6b:00:0b:ba:4a</ParaValue>` +
		`<ParaName>OBJ_DHCPBIND_ID.Name</ParaName><ParaValue>Gio</ParaValue>` +
		`</Instance><Instance><ParaName>OBJ_DHCPBIND_ID.IPAddr</ParaName><ParaValue>192.168.2.2</ParaValue>` +
		`<ParaName>OBJ_DHCPBIND_ID.Name</ParaName><ParaValue>Roteador&#32;-&#32;Sala</ParaValue>` +
		`</Instance></OBJ_DHCPBIND_ID></ajax_response_xml_root>`)
	rows := parseMenuInstances(data)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0]["IPAddr"] != "192.168.2.10" || rows[0]["Name"] != "Gio" {
		t.Fatalf("unexpected first row: %v", rows[0])
	}
	if rows[1]["Name"] != "Roteador - Sala" {
		t.Fatalf("entity decoding failed: %v", rows[1])
	}
}

func TestRebootRequiresExplicitEndpoint(t *testing.T) {
	built, err := New(device.Config{Name: "h199a", Vendor: device.VendorZTE, Host: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	driver := built.(*Driver)
	if err := driver.Reboot(context.Background()); err == nil {
		t.Fatal("expected reboot to be disabled without an endpoint override")
	}
}
