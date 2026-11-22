package autodetect

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
)

func TestAutodetectFallsBackToCorrectVendor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/webserver/SesTokInfo":
			_, _ = io.WriteString(w, `<response><SesInfo>sess</SesInfo><TokInfo>tok</TokInfo></response>`)
		case "/api/user/login":
			_, _ = io.WriteString(w, `<response>OK</response>`)
		case "/api/system/routerstatus":
			_, _ = io.WriteString(w, `<response><HostName>BE3</HostName><SoftwareVersion>1.0</SoftwareVersion></response>`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := device.Config{
		Name:     "be3",
		Vendor:   device.VendorMikroTik,
		Host:     strings.TrimPrefix(srv.URL, "http://"),
		Username: "admin",
		Password: "pass",
	}
	driver, err := New(cfg, cfg.Vendor)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = driver.Close()
	}()
	if driver.Vendor() != device.VendorHuawei {
		t.Fatalf("expected huawei to be detected, got %s", driver.Vendor())
	}
	if len(driver.Capabilities()) == 0 {
		t.Fatal("detected driver has no capabilities")
	}
}
