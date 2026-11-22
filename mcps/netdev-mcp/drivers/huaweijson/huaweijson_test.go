package huaweijson

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/mcps/netdev-mcp/device"
)

func TestSystemInfoFromRealShapedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/system/deviceinfo" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"custinfo":{"CustDeviceType":"WUKUN-BE32-41"},`+
			`"FriendlyName":"HUAWEI WiFi BE3","DeviceName":"Huawei",`+
			`"SoftwareVersion":"-","HardwareVersion":"VER.A","UpTime":116382,`+
			`"SerialNumber":"a6b6c3b2","csrf_param":"param","csrf_token":"token"}`)
	}))
	defer srv.Close()

	cfg := device.Config{Name: "gio", Vendor: device.VendorHuaweiJSON, Host: strings.TrimPrefix(srv.URL, "http://")}
	built, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	driver := built.(*Driver)
	info, err := driver.SystemInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Model != "WUKUN-BE32-41" || info.Hostname != "Huawei" || info.Serial != "a6b6c3b2" {
		t.Fatalf("unexpected info: %+v", info)
	}
	if !info.Reachable || info.Vendor != device.VendorHuaweiJSON {
		t.Fatalf("unexpected vendor or reachability: %+v", info)
	}
	if info.Extra["csrf_token"] != "token" {
		t.Fatalf("csrf token not carried: %+v", info.Extra)
	}
}

func TestSystemInfoRejectsUnknownPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"hello":"world"}`)
	}))
	defer srv.Close()
	built, _ := New(device.Config{Name: "x", Vendor: device.VendorHuaweiJSON, Host: strings.TrimPrefix(srv.URL, "http://")})
	if _, err := built.(*Driver).SystemInfo(context.Background()); err == nil {
		t.Fatal("expected error for non-Huawei payload")
	}
}
