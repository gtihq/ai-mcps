package huaweijson

import (
	"context"
	"strings"
	"testing"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
)

func TestOwnerWiFiIsProtected(t *testing.T) {
	built, err := New(device.Config{Name: "be3", Vendor: device.VendorHuaweiJSON, Host: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	driver := built.(*Driver)

	for _, name := range []string{"Gio Furhouse", "gio 5ghz"} {
		err := driver.UpdateWiFi(context.Background(), name, "Other", "password123")
		if err == nil || !strings.Contains(err.Error(), "protected") {
			t.Fatalf("expected protected error for %q, got %v", name, err)
		}
	}
	err = driver.UpdateWiFi(context.Background(), "Guest", "Gio Furhouse", "password123")
	if err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("expected protected error when renaming to owner SSID, got %v", err)
	}
}

func TestProtectedSSIDsFromInventory(t *testing.T) {
	built, _ := New(device.Config{
		Name: "be3", Vendor: device.VendorHuaweiJSON, Host: "127.0.0.1",
		Extra: map[string]any{"protected_ssids": "Gio Furhouse, Work-SSID"},
	})
	driver := built.(*Driver)
	list := driver.protectedSSIDs()
	if len(list) != 2 || list[1] != "Work-SSID" {
		t.Fatalf("unexpected protected list: %v", list)
	}
}
