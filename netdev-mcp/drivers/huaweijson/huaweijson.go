// Package huaweijson implements read-only support for the newer Huawei home
// gateway API used by the WiFi BE3 (WUKUN firmware). Unlike the classic XML
// web API, these devices expose JSON status endpoints without a login.
package huaweijson

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
)

const (
	defaultTimeout = 10 * time.Second
	pathDeviceInfo = "/api/system/deviceinfo"
)

// Driver reads the public JSON status endpoints of a Huawei BE3.
type Driver struct {
	cfg     device.Config
	client  *http.Client
	baseURL string
}

// New creates the driver without opening a connection.
func New(cfg device.Config) (device.Driver, error) {
	port := 80
	scheme := "http"
	if cfg.TLS {
		port = 443
		scheme = "https"
	}
	return &Driver{
		cfg:     cfg,
		client:  &http.Client{Timeout: cfg.Duration(defaultTimeout)},
		baseURL: scheme + "://" + cfg.Address(port),
	}, nil
}

// Vendor returns the JSON API variant identifier so the inventory keeps
// using this driver on later tool calls.
func (d *Driver) Vendor() device.Vendor { return device.VendorHuaweiJSON }

// Capabilities lists what this firmware exposes without a login.
func (d *Driver) Capabilities() []string {
	return []string{device.CapSystem}
}

// Close releases idle connections.
func (d *Driver) Close() error {
	d.client.CloseIdleConnections()
	return nil
}

// SystemInfo reads /api/system/deviceinfo. The endpoint is public on WUKUN
// firmware and already carries the CSRF tokens used by the web UI.
func (d *Driver) SystemInfo(ctx context.Context) (device.SystemInfo, error) {
	data, err := d.get(ctx, d.cfg.Endpoint("system", pathDeviceInfo))
	if err != nil {
		return device.SystemInfo{}, err
	}
	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return device.SystemInfo{}, fmt.Errorf("huawei json: invalid deviceinfo: %w", err)
	}
	if _, ok := payload["custinfo"]; !ok {
		return device.SystemInfo{}, fmt.Errorf("huawei json: deviceinfo does not look like a Huawei gateway")
	}

	info := device.SystemInfo{
		Name:      d.cfg.Name,
		Vendor:    d.Vendor(),
		Model:     device.StringOr(d.cfg.Model, device.Str(payload, "FriendlyName", "DeviceName")),
		Host:      d.cfg.Host,
		Site:      d.cfg.Site,
		Hostname:  device.Str(payload, "DeviceName", "FriendlyName"),
		Version:   device.Str(payload, "SoftwareVersion", "CurrentVersion"),
		Uptime:    device.Str(payload, "UpTime"),
		Serial:    device.Str(payload, "SerialNumber"),
		Reachable: true,
	}
	if model := device.Str(device.MapAny(payload["custinfo"]), "CustDeviceType", "CustDeviceName"); model != "" && d.cfg.Model == "" {
		info.Model = model
	}
	info.Extra = map[string]any{
		"csrf_param": device.Str(payload, "csrf_param"),
		"csrf_token": device.Str(payload, "csrf_token"),
		"hardware":   device.Str(payload, "HardwareVersion"),
	}
	return info, nil
}

// get performs one GET and returns the response body.
func (d *Driver) get(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("accept", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d em GET %s: %s", resp.StatusCode, path, strings.TrimSpace(string(data)))
	}
	return string(data), nil
}

// Compile-time assertions.
var (
	_ device.Driver       = (*Driver)(nil)
	_ device.SystemDriver = (*Driver)(nil)
)
