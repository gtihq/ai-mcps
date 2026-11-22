package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
)

const (
	inventoryEnv = "NETDEV_INVENTORY"
	devicesEnv   = "NETDEV_DEVICES"
)

// inventoryCandidates are checked in order when no path was supplied. The
// first entry matches running from the repository root, the second matches
// running from inside the netdev-mcp folder.
var inventoryCandidates = []string{"netdev-mcp/devices.json", "devices.json"}

// resolveInventoryPath picks the explicit path, the environment override or
// the first existing candidate.
func resolveInventoryPath(path string) string {
	if path != "" {
		return path
	}
	if env := os.Getenv(inventoryEnv); env != "" {
		return env
	}
	for _, candidate := range inventoryCandidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return inventoryCandidates[0]
}

// loadInventory reads device definitions from the NETDEV_DEVICES JSON string
// or from the inventory file. Every string supports ${ENV_VAR} expansion so
// passwords never need to live in the repository.
func loadInventory(path string) ([]device.Config, error) {
	raw := strings.TrimSpace(os.Getenv(devicesEnv))
	if raw == "" {
		path = resolveInventoryPath(path)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return []device.Config{}, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read inventory %s: %w", path, err)
		}
		raw = string(data)
	}
	raw = os.ExpandEnv(raw)

	var document struct {
		Devices []device.Config `json:"devices"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		var list []device.Config
		if listErr := json.Unmarshal([]byte(raw), &list); listErr == nil {
			document.Devices = list
		} else {
			return nil, fmt.Errorf("parse inventory: %w", err)
		}
	}

	seen := make(map[string]bool, len(document.Devices))
	out := make([]device.Config, 0, len(document.Devices))
	for _, cfg := range document.Devices {
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		if seen[cfg.Name] {
			return nil, fmt.Errorf("duplicate device name %q", cfg.Name)
		}
		seen[cfg.Name] = true
		out = append(out, cfg)
	}
	return out, nil
}

// inventoryPath returns the path shown to users in error and help messages.
func inventoryPath() string {
	path, err := filepath.Abs(resolveInventoryPath(""))
	if err != nil {
		return resolveInventoryPath("")
	}
	return path
}
