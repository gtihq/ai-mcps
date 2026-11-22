package main

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/autodetect"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/huawei"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/huaweijson"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/mikrotik"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/sshcli"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/zte"
)

// factory builds a driver from an inventory entry.
type factory func(device.Config) (device.Driver, error)

// Registry owns the device inventory and lazily created drivers. Drivers are
// shared between tool calls so web sessions and API connections are reused.
type Registry struct {
	mu        sync.Mutex
	configs   map[string]device.Config
	order     []string
	drivers   map[string]device.Driver
	factories map[device.Vendor]factory
}

// newRegistry builds a registry from the supplied inventory.
func newRegistry(configs []device.Config) *Registry {
	r := &Registry{
		configs: make(map[string]device.Config, len(configs)),
		order:   make([]string, 0, len(configs)),
		drivers: make(map[string]device.Driver),
		factories: map[device.Vendor]factory{
			device.VendorMikroTik:   mikrotik.New,
			device.VendorZTE:        zte.New,
			device.VendorHuawei:     huawei.New,
			device.VendorHuaweiJSON: huaweijson.New,
			device.VendorSSH:        sshcli.New,
			device.VendorAuto: func(cfg device.Config) (device.Driver, error) {
				return autodetect.New(cfg)
			},
		},
	}
	for _, cfg := range configs {
		r.configs[cfg.Name] = cfg
		r.order = append(r.order, cfg.Name)
	}
	sort.Strings(r.order)
	return r
}

// Reload replaces the inventory and drops cached drivers. Existing driver
// connections are closed so a reconfiguration takes effect immediately.
func (r *Registry) Reload(configs []device.Config) {
	r.mu.Lock()
	old := r.drivers
	r.configs = make(map[string]device.Config, len(configs))
	r.order = make([]string, 0, len(configs))
	r.drivers = make(map[string]device.Driver)
	for _, cfg := range configs {
		r.configs[cfg.Name] = cfg
		r.order = append(r.order, cfg.Name)
	}
	sort.Strings(r.order)
	r.mu.Unlock()
	for name, driver := range old {
		if err := driver.Close(); err != nil {
			fmt.Printf("netdev-mcp: closing driver %s: %v\n", name, err)
		}
	}
}

// Rename changes an inventory name and updates the in-memory registry.
func (r *Registry) Rename(oldName, newName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.drivers[oldName]; ok {
		if driver := r.drivers[oldName]; driver != nil {
			_ = driver.Close()
		}
		delete(r.drivers, oldName)
	}
	if err := renameDeviceInInventory(oldName, newName); err != nil {
		return err
	}
	configs, err := loadInventory("")
	if err != nil {
		return err
	}
	r.configs = make(map[string]device.Config, len(configs))
	r.order = make([]string, 0, len(configs))
	for _, cfg := range configs {
		r.configs[cfg.Name] = cfg
		r.order = append(r.order, cfg.Name)
	}
	sort.Strings(r.order)
	return nil
}

// Names returns all device names in stable order.
func (r *Registry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

// Config returns one inventory entry.
func (r *Registry) Config(name string) (device.Config, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, ok := r.configs[name]
	if !ok {
		return device.Config{}, fmt.Errorf("%w: %s", device.ErrNotFound, name)
	}
	if cfg.Disabled {
		return device.Config{}, fmt.Errorf("%w: %s", device.ErrDisabled, name)
	}
	return cfg, nil
}

// Driver returns a cached driver or builds one on first use.
func (r *Registry) Driver(name string) (device.Driver, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, ok := r.configs[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", device.ErrNotFound, name)
	}
	if cfg.Disabled {
		return nil, fmt.Errorf("%w: %s", device.ErrDisabled, name)
	}
	if driver, ok := r.drivers[name]; ok {
		return driver, nil
	}
	build, ok := r.factories[cfg.Vendor]
	if !ok {
		return nil, fmt.Errorf("unsupported vendor %q for %s", cfg.Vendor, name)
	}
	driver, err := build(cfg)
	if err != nil {
		return nil, err
	}
	r.drivers[name] = driver
	return driver, nil
}

// Reports describes every configured device and its capabilities.
func (r *Registry) Reports() []device.CapabilityReport {
	r.mu.Lock()
	configs := make([]device.Config, 0, len(r.order))
	for _, name := range r.order {
		configs = append(configs, r.configs[name])
	}
	r.mu.Unlock()

	reports := make([]device.CapabilityReport, 0, len(configs))
	for _, cfg := range configs {
		report := device.CapabilityReport{
			Name:   cfg.Name,
			Vendor: cfg.Vendor,
			Model:  cfg.Model,
			Site:   cfg.Site,
			Host:   cfg.Host,
		}
		driver, err := r.Driver(cfg.Name)
		if err != nil {
			report.Error = err.Error()
		} else {
			report.Capabilities = driver.Capabilities()
		}
		reports = append(reports, report)
	}
	return reports
}

// resolve is the shared entry point for device-scoped tools. It returns the
// driver and a capability checker closure.
func (r *Registry) resolve(name string, capability string) (device.Driver, func() error, error) {
	driver, err := r.Driver(name)
	if err != nil {
		return nil, nil, err
	}
	for _, cap := range driver.Capabilities() {
		if cap == capability {
			return driver, func() error { return nil }, nil
		}
	}
	return driver, func() error {
		return fmt.Errorf("%w: %s on %s", device.ErrUnsupported, capability, name)
	}, nil
}

// checkSystem runs a task on every enabled device concurrently and returns
// one result per device. Used by the fleet health tool.
func (r *Registry) checkSystem(ctx context.Context) []device.SystemInfo {
	names := r.Names()
	results := make([]device.SystemInfo, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(index int, deviceName string) {
			defer wg.Done()
			driver, err := r.Driver(deviceName)
			if err != nil {
				results[index] = device.SystemInfo{Name: deviceName, Host: inventoryHost(r, deviceName), Reachable: false}
				return
			}
			systemDriver, ok := driver.(device.SystemDriver)
			if !ok {
				results[index] = device.SystemInfo{Name: deviceName, Reachable: false}
				return
			}
			info, err := systemDriver.SystemInfo(ctx)
			if err != nil {
				info.Name = deviceName
				info.Reachable = false
				info.Extra = map[string]any{"error": err.Error()}
			}
			results[index] = info
		}(i, name)
	}
	wg.Wait()
	return results
}

// inventoryHost returns the configured host for a device name.
func inventoryHost(r *Registry, name string) string {
	cfg, err := r.Config(name)
	if err != nil {
		return ""
	}
	return cfg.Host
}
