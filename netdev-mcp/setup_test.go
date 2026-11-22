package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
)

func newTestSetup(t *testing.T) *setupServer {
	t.Helper()
	setup := &setupServer{
		registry: newRegistry(nil),
		token:    "test-token",
		done:     make(chan struct{}),
	}
	return setup
}

func TestSetupPageRequiresToken(t *testing.T) {
	setup := newTestSetup(t)
	request := httptest.NewRequest(http.MethodGet, "/setup", nil)
	response := httptest.NewRecorder()
	setup.handleRoot(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("want 403 without token, got %d", response.Code)
	}
}

func TestSetupPageAsksTypeAndStartsEmpty(t *testing.T) {
	setup := newTestSetup(t)
	request := httptest.NewRequest(http.MethodGet, "/setup?token=test-token", nil)
	response := httptest.NewRecorder()
	setup.handleRoot(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", response.Code)
	}
	body := response.Body.String()
	for _, marker := range []string{
		"Painel de rede", "Inventário", "Adicionar dispositivo",
		"Auto (detectar)", "MikroTik RouterOS", "ZTE ZXHN H199A",
		"Huawei WiFi BE3", "Switch SSH", "Adicionar à lista",
		"Nenhum dispositivo na lista", "Editar", "Mostrar",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("form is missing %q", marker)
		}
	}
	for _, stale := range []string{"mikrotik-rb", "zxhn-h199a", "wifi-be3"} {
		if strings.Contains(body, stale) {
			t.Errorf("form still has prefilled device %q", stale)
		}
	}
}

func TestSetupShowsInventorySideBySide(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	t.Setenv(inventoryEnv, path)
	if err := saveDevices([]device.Config{
		{Name: "roteador-sala", Vendor: device.VendorMikroTik, Host: "10.0.0.1"},
		{Name: "sw-office", Vendor: device.VendorSSH, Host: "10.0.0.2"},
	}); err != nil {
		t.Fatal(err)
	}
	setup := newTestSetup(t)
	setup.lastQueued = []device.Config{{Name: "novo", Vendor: device.VendorZTE, Host: "10.0.0.3"}}
	response := httptest.NewRecorder()
	setup.handleRoot(response, httptest.NewRequest(http.MethodGet, "/setup?token=test-token", nil))
	body := response.Body.String()
	for _, marker := range []string{
		"roteador-sala", "sw-office", "10.0.0.1", "10.0.0.2",
		"Inventário", "Adicionar dispositivo", "var devices=[",
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("workspace is missing %q", marker)
		}
	}
}

func TestSetupEditRestoresQueuedDevices(t *testing.T) {
	setup := newTestSetup(t)
	setup.lastQueued = []device.Config{
		{Name: "rb1", Vendor: device.VendorMikroTik, Host: "10.0.0.1", Password: "secret"},
	}
	request := httptest.NewRequest(http.MethodGet, "/setup?token=test-token&edit=1", nil)
	response := httptest.NewRecorder()
	setup.handleRoot(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", response.Code)
	}
	body := response.Body.String()
	for _, marker := range []string{"rb1", "10.0.0.1", "var devices=[", "selected"} {
		if !strings.Contains(body, marker) {
			t.Errorf("edit page is missing %q", marker)
		}
	}
}

func TestSaveDevicesMergesByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	t.Setenv(inventoryEnv, path)
	first := []device.Config{{Name: "rb", Vendor: device.VendorMikroTik, Host: "10.0.0.1", Password: "old"}}
	if err := saveDevices(first); err != nil {
		t.Fatal(err)
	}
	second := []device.Config{
		{Name: "rb", Vendor: device.VendorMikroTik, Host: "10.0.0.2", Password: "new"},
		{Name: "be3", Vendor: device.VendorHuawei, Host: "10.0.0.3", Password: "secret"},
	}
	if err := saveDevices(second); err != nil {
		t.Fatal(err)
	}
	configs, err := loadInventory("")
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 2 {
		t.Fatalf("want 2 devices, got %d", len(configs))
	}
	if configs[0].Password != "new" || configs[0].Host != "10.0.0.2" {
		t.Fatalf("merge did not replace by name: %+v", configs[0])
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("inventory must be 0600, got %o", info.Mode().Perm())
	}
}

func TestSetupSaveAllowsMultipleMikroTiksWithCustomNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	t.Setenv(inventoryEnv, path)

	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/system/resource":
			_, _ = io.WriteString(w, `[{"version":"7.15","uptime":"1d"}]`)
		case "/rest/system/identity":
			_, _ = io.WriteString(w, `[{"name":"rb"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer router.Close()
	host := strings.TrimPrefix(router.URL, "http://")

	setup := newTestSetup(t)
	values := url.Values{}
	values.Set("token", "test-token")
	for _, name := range []string{"roteador-sala", "roteador-garagem"} {
		values.Add("name", name)
		values.Add("vendor", "mikrotik")
		values.Add("host", host)
		values.Add("username", "admin")
		values.Add("password", "secret")
		values.Add("tls", "")
	}
	request := httptest.NewRequest(http.MethodPost, "/save?token=test-token", strings.NewReader(values.Encode()))
	request.Header.Set("content-type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	setup.handleSave(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", response.Code)
	}

	configs, err := loadInventory("")
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 2 {
		t.Fatalf("want 2 MikroTiks, got %d", len(configs))
	}
	if configs[0].Name != "roteador-sala" || configs[1].Name != "roteador-garagem" {
		t.Fatalf("custom names not preserved: %+v", configs)
	}
	if configs[0].Vendor != device.VendorMikroTik || configs[1].Vendor != device.VendorMikroTik {
		t.Fatalf("unexpected vendors: %+v", configs)
	}
}

func TestSetupSaveAuthenticatesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	t.Setenv(inventoryEnv, path)

	huaweiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/webserver/SesTokInfo":
			_, _ = io.WriteString(w, `<response><SesInfo>sess</SesInfo><TokInfo>tok</TokInfo></response>`)
		case "/api/user/login":
			_, _ = io.WriteString(w, `<response>OK</response>`)
		case "/api/system/routerstatus":
			_, _ = io.WriteString(w, `<response><HostName>BE3</HostName><SoftwareVersion>1.2.3</SoftwareVersion></response>`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer huaweiServer.Close()

	zteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login.html":
			_, _ = io.WriteString(w, `<meta name="csrf_token" content="csrf"/>`)
		case r.URL.Path == "/login.lua":
			_, _ = io.WriteString(w, `<html>Welcome</html>`)
		case r.URL.Path == "/getpage.lua":
			_, _ = io.WriteString(w, `<table><tr><th>HostName</th><th>SoftwareVersion</th></tr>`+
				`<tr><td>H199A</td><td>V2</td></tr></table>`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer zteServer.Close()

	setup := newTestSetup(t)
	values := url.Values{}
	values.Set("token", "test-token")
	values.Add("name", "be3")
	values.Add("vendor", "huawei")
	values.Add("host", strings.TrimPrefix(huaweiServer.URL, "http://"))
	values.Add("username", "admin")
	values.Add("password", "secret")
	values.Add("name", "h199a")
	values.Add("vendor", "zte")
	values.Add("host", strings.TrimPrefix(zteServer.URL, "http://"))
	values.Add("username", "admin")
	values.Add("password", "secret")

	request := httptest.NewRequest(http.MethodPost, "/save?token=test-token", strings.NewReader(values.Encode()))
	request.Header.Set("content-type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	setup.handleSave(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "Conectado como") {
		t.Fatalf("expected authentication result, got %s", body)
	}
	if !strings.Contains(body, "2 dispositivos conectados") {
		t.Fatalf("expected both devices saved, got %s", body)
	}
	if !strings.Contains(body, "Concluir") {
		t.Fatalf("result panel must offer the finish action, got %s", body)
	}
	if !strings.Contains(body, "var devices=[") || !strings.Contains(body, "be3") {
		t.Fatalf("queued devices must stay on the page after saving, got %s", body)
	}
	if !strings.Contains(body, "be3") || !strings.Contains(body, "h199a") {
		t.Fatalf("inventory must be listed next to the form, got %s", body)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Devices []device.Config `json:"devices"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Devices) != 2 {
		t.Fatalf("want 2 saved devices, got %d", len(document.Devices))
	}
	if _, err := setup.registry.Driver("be3"); err != nil {
		t.Fatalf("registry was not reloaded: %v", err)
	}
}
