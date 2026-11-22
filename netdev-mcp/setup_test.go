package main

import (
	"encoding/json"
	"errors"
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

func TestSetupOnlyOpensWithoutCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	t.Setenv(inventoryEnv, path)
	if err := saveDevices([]device.Config{
		{Name: "rb", Vendor: device.VendorMikroTik, Host: "10.0.0.1", Username: "admin", Password: "secret"},
	}); err != nil {
		t.Fatal(err)
	}
	setup, urls, err := getOrStartSetup(newRegistry(nil))
	if !errors.Is(err, ErrAlreadyAuthenticated) || setup != nil || len(urls) != 0 {
		t.Fatalf("setup must not open when authenticated: %v %v %v", setup, urls, err)
	}
}

func TestSetupOpensWhenNoCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	t.Setenv(inventoryEnv, path)
	if err := saveDevices([]device.Config{
		{Name: "rb", Vendor: device.VendorMikroTik, Host: "10.0.0.1"},
	}); err != nil {
		t.Fatal(err)
	}
	setup, urls, err := getOrStartSetup(newRegistry(nil))
	if err != nil || setup == nil || len(urls) == 0 {
		t.Fatalf("setup should open without credentials: %v %v %v", setup, urls, err)
	}
	setup.shutdown()
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
		"NetDev MCP", "Inventory", "Add device",
		"Auto (detect)", "MikroTik RouterOS", "ZTE ZXHN H199A",
		"Huawei WiFi BE3", "Switch SSH", "Add to list",
		"No devices in the list", "Edit", "Show",
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

func TestSetupRemoveAndUndo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	t.Setenv(inventoryEnv, path)
	if err := saveDevices([]device.Config{
		{Name: "rb", Vendor: device.VendorMikroTik, Host: "10.0.0.1"},
	}); err != nil {
		t.Fatal(err)
	}
	setup := newTestSetup(t)

	removeBody := url.Values{}
	removeBody.Set("token", "test-token")
	removeBody.Set("name", "rb")
	removeRequest := httptest.NewRequest(http.MethodPost, "/remove?token=test-token", strings.NewReader(removeBody.Encode()))
	removeRequest.Header.Set("content-type", "application/x-www-form-urlencoded")
	removeResponse := httptest.NewRecorder()
	setup.handleRemove(removeResponse, removeRequest)
	if !strings.Contains(removeResponse.Body.String(), "removed from the inventory") || !strings.Contains(removeResponse.Body.String(), "Undo") {
		t.Fatalf("remove result missing message or undo: %s", removeResponse.Body.String())
	}
	configs, err := loadInventory("")
	if err != nil || len(configs) != 0 {
		t.Fatalf("device was not removed: %+v %v", configs, err)
	}

	undoResponse := httptest.NewRecorder()
	setup.handleUndo(undoResponse, httptest.NewRequest(http.MethodGet, "/undo?token=test-token", nil))
	if !strings.Contains(undoResponse.Body.String(), "restored") {
		t.Fatalf("undo result missing message: %s", undoResponse.Body.String())
	}
	configs, err = loadInventory("")
	if err != nil || len(configs) != 1 || configs[0].Name != "rb" {
		t.Fatalf("device was not restored: %+v %v", configs, err)
	}
}

func TestSetupTestInventoryWithoutNewDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	t.Setenv(inventoryEnv, path)
	router := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/system/resource":
			_, _ = io.WriteString(w, `[{"version":"7.15"}]`)
		case "/rest/system/identity":
			_, _ = io.WriteString(w, `[{"name":"rb"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer router.Close()
	if err := saveDevices([]device.Config{
		{Name: "rb", Vendor: device.VendorMikroTik, Host: strings.TrimPrefix(router.URL, "http://")},
	}); err != nil {
		t.Fatal(err)
	}

	setup := newTestSetup(t)
	values := url.Values{}
	values.Set("token", "test-token")
	values.Set("test_inventory", "1")
	request := httptest.NewRequest(http.MethodPost, "/save?token=test-token", strings.NewReader(values.Encode()))
	request.Header.Set("content-type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	setup.handleSave(response, request)
	body := response.Body.String()
	if !strings.Contains(body, "1 device connected") || !strings.Contains(body, "No changes were saved") {
		t.Fatalf("inventory test result missing: %s", body)
	}
	configs, err := loadInventory("")
	if err != nil || len(configs) != 1 {
		t.Fatalf("inventory changed during test: %+v %v", configs, err)
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
		"Inventory", "Add device", "var devices=[",
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
		if r.URL.Query().Get("_tag") == "login_token" {
			_, _ = io.WriteString(w, `<ajax_response_xml_root>123</ajax_response_xml_root>`)
			return
		}
		if r.URL.Query().Get("_tag") == "login_entry" {
			_, _ = io.WriteString(w, `{"sess_token":"abc"}`)
			return
		}
		if r.URL.Query().Get("_tag") == "lanMgrIpv4" {
			_, _ = io.WriteString(w, `<html>LAN</html>`)
			return
		}
		if r.URL.Query().Get("_tag") == "dhcp4s_dhcphostinfo_m.lua" {
			_, _ = io.WriteString(w, `<ajax_response_xml_root><IF_ERRORSTR>SUCC</IF_ERRORSTR></ajax_response_xml_root>`)
			return
		}
		switch {
		case r.URL.Path == "/":
			_, _ = io.WriteString(w, `<html><input name="Frm_Username"></html>`)
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
	if !strings.Contains(body, "Huawei") || !strings.Contains(body, "ZTE") {
		t.Fatalf("expected authentication result, got %s", body)
	}
	if !strings.Contains(body, "2 devices connected") {
		t.Fatalf("expected both devices saved, got %s", body)
	}
	if !strings.Contains(body, "Finish") {
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
