package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadInventoryFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	content := `{
  "devices": [
    {
      "name": "rb5009",
      "vendor": "mikrotik",
      "host": "192.168.88.1",
      "username": "admin",
      "password": "${TEST_ROUTER_PASS}",
      "site": "home"
    },
    {
      "name": "be3",
      "vendor": "huawei",
      "host": "192.168.3.1",
      "disabled": true
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_ROUTER_PASS", "secret123")

	configs, err := loadInventory(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 2 {
		t.Fatalf("want 2 devices, got %d", len(configs))
	}
	if configs[0].Password != "secret123" {
		t.Fatalf("env expansion failed: %q", configs[0].Password)
	}
	if !configs[1].Disabled {
		t.Fatal("second device should be disabled")
	}
}

func TestLoadInventoryFromEnvironment(t *testing.T) {
	t.Setenv(devicesEnv, `[{"name":"sw1","vendor":"ssh","host":"10.0.0.2","profile":"cisco"}]`)
	configs, err := loadInventory("")
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 1 || configs[0].Name != "sw1" || configs[0].Profile != "cisco" {
		t.Fatalf("unexpected inventory: %+v", configs)
	}
}

func TestLoadInventoryRejectsDuplicateNames(t *testing.T) {
	t.Setenv(devicesEnv, `[{"name":"a","vendor":"ssh","host":"1.1.1.1"},{"name":"a","vendor":"ssh","host":"2.2.2.2"}]`)
	if _, err := loadInventory(""); err == nil {
		t.Fatal("expected duplicate name error")
	}
}

func TestLoadInventoryMissingFileIsEmpty(t *testing.T) {
	t.Setenv(devicesEnv, "")
	configs, err := loadInventory(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 0 {
		t.Fatalf("want empty inventory, got %d", len(configs))
	}
}
