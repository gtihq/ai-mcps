package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
	"github.com/gtihq/ai-mcps/netdev-mcp/device"
)

type fakeDriver struct {
	caps []string
	info device.SystemInfo
}

func (f *fakeDriver) Vendor() device.Vendor  { return device.VendorSSH }
func (f *fakeDriver) Capabilities() []string { return f.caps }
func (f *fakeDriver) Close() error           { return nil }
func (f *fakeDriver) SystemInfo(context.Context) (device.SystemInfo, error) {
	return f.info, nil
}

// testRegistry builds a registry containing one prebuilt fake driver.
func testRegistry() *Registry {
	reg := newRegistry(nil)
	driver := &fakeDriver{
		caps: []string{device.CapSystem},
		info: device.SystemInfo{
			Name: "fake", Vendor: device.VendorSSH, Host: "10.0.0.1",
			Hostname: "switch-1", Version: "15.2", Reachable: true,
		},
	}
	reg.mu.Lock()
	reg.configs["fake"] = device.Config{Name: "fake", Vendor: device.VendorSSH, Host: "10.0.0.1", Model: "C9300"}
	reg.order = []string{"fake"}
	reg.drivers["fake"] = driver
	reg.mu.Unlock()
	return reg
}

func callTool(t *testing.T, reg *Registry, name string, args map[string]any) map[string]any {
	t.Helper()
	result, err := findTool(reg, name).Handler(context.Background(), args)
	if err != nil {
		t.Fatalf("%s returned error: %v", name, err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func findTool(reg *Registry, name string) *mcpserver.Tool {
	for _, tool := range tools(reg) {
		if tool.Name == name {
			return tool
		}
	}
	return nil
}

func TestToolSchemasRegistered(t *testing.T) {
	reg := testRegistry()
	server := mcpserver.New("netdev", tools(reg)...)
	line := server.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	for _, name := range []string{
		"netdev_list_devices", "netdev_health", "netdev_system_info",
		"netdev_interfaces", "netdev_wifi_clients", "netdev_reboot",
		"netdev_command", "netdev_http", "netdev_topology",
	} {
		if !strings.Contains(line, `"name":"`+name+`"`) {
			t.Errorf("tool %s missing from tools/list", name)
		}
	}
}

func TestListDevices(t *testing.T) {
	out := callTool(t, testRegistry(), "netdev_list_devices", nil)
	if out["count"] != float64(1) {
		t.Fatalf("unexpected count: %v", out["count"])
	}
	devices := out["devices"].([]any)
	entry := devices[0].(map[string]any)
	if entry["name"] != "fake" || entry["model"] != "C9300" {
		t.Fatalf("unexpected device: %v", entry)
	}
}

func TestSystemInfoTool(t *testing.T) {
	out := callTool(t, testRegistry(), "netdev_system_info", map[string]any{"device": "fake"})
	if out["hostname"] != "switch-1" || out["reachable"] != true {
		t.Fatalf("unexpected system info: %v", out)
	}
}

func TestUnsupportedCapabilityReturnsErrorMap(t *testing.T) {
	out := callTool(t, testRegistry(), "netdev_wifi_clients", map[string]any{"device": "fake"})
	errText, ok := out["error"].(string)
	if !ok || !strings.Contains(errText, "wifi_clients") {
		t.Fatalf("expected capability error, got %v", out)
	}
}

func TestRebootRequiresConfirm(t *testing.T) {
	reg := newRegistry(nil)
	driver := &rebootDriver{fakeDriver: fakeDriver{caps: []string{device.CapReboot}}}
	reg.mu.Lock()
	reg.configs["rb"] = device.Config{Name: "rb", Vendor: device.VendorMikroTik, Host: "192.168.88.1"}
	reg.order = []string{"rb"}
	reg.drivers["rb"] = driver
	reg.mu.Unlock()

	out := callTool(t, reg, "netdev_reboot", map[string]any{"device": "rb"})
	if !strings.Contains(out["error"].(string), "confirm=true") {
		t.Fatalf("expected confirm guard, got %v", out)
	}
	if !driver.rebooted {
		out = callTool(t, reg, "netdev_reboot", map[string]any{"device": "rb", "confirm": true})
		if out["reboot"] != "requested" || !driver.rebooted {
			t.Fatalf("reboot did not run: %v", out)
		}
	}
}

type rebootDriver struct {
	fakeDriver
	rebooted bool
}

func (d *rebootDriver) Capabilities() []string { return []string{device.CapReboot} }
func (d *rebootDriver) Reboot(context.Context) error {
	d.rebooted = true
	return nil
}
