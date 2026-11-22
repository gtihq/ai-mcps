// Package autodetect probes a device with every known driver and keeps the
// first one that can read its system information. It is used by the setup
// screen so a wrong vendor selection does not block onboarding.
package autodetect

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/huawei"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/huaweijson"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/mikrotik"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/sshcli"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/zte"
)

const probeTimeout = 12 * time.Second

type builder func(device.Config) (device.Driver, error)

var builders = map[device.Vendor]builder{
	device.VendorMikroTik:   mikrotik.New,
	device.VendorHuawei:     huawei.New,
	device.VendorHuaweiJSON: huaweijson.New,
	device.VendorZTE:        zte.New,
	device.VendorSSH:        sshcli.New,
}

var probeOrder = []device.Vendor{
	device.VendorMikroTik,
	device.VendorHuawei,
	device.VendorHuaweiJSON,
	device.VendorZTE,
	device.VendorSSH,
}

// Reason is one short explanation of why a driver failed.
type Reason struct {
	Vendor  device.Vendor
	Message string
	Detail  string
}

// DetectError carries one reason per tried vendor so the UI can show a short
// line instead of a raw stack of internal error strings.
type DetectError struct {
	Reasons []Reason
}

// Error implements the error interface.
func (e *DetectError) Error() string {
	parts := make([]string, 0, len(e.Reasons))
	for _, reason := range e.Reasons {
		parts = append(parts, vendorLabel(reason.Vendor)+": "+reason.Message)
	}
	return "detection failed: " + strings.Join(parts, " · ")
}

// Explanation separates the headline shown in the result row from the full
// list of attempts, which stays behind a details disclosure.
type Explanation struct {
	Headline string
	Detail   string
}

// Explain turns a detection failure into a focused headline for the vendor
// the user chose and keeps every attempt as optional detail.
func Explain(err error, preferred device.Vendor) Explanation {
	var detectErr *DetectError
	if !errors.As(err, &detectErr) {
		headline := shortReason(preferred, err)
		return Explanation{Headline: vendorLabel(preferred) + ": " + headline, Detail: err.Error()}
	}
	headline := ""
	detail := ""
	for _, reason := range detectErr.Reasons {
		if reason.Vendor == preferred {
			headline = vendorLabel(preferred) + ": " + reason.Message
			detail = reason.Detail
			break
		}
	}
	if headline == "" && len(detectErr.Reasons) > 0 {
		headline = vendorLabel(detectErr.Reasons[0].Vendor) + ": " + detectErr.Reasons[0].Message
	}
	parts := make([]string, 0, len(detectErr.Reasons))
	for _, reason := range detectErr.Reasons {
		parts = append(parts, vendorLabel(reason.Vendor)+" "+reason.Message)
	}
	if detail == "" {
		detail = strings.Join(parts, " · ") + ". Check the IP address and credentials."
	} else if len(detail) > 320 {
		detail = detail[:320] + "…"
	}
	return Explanation{Headline: headline, Detail: detail}
}

// vendorLabel is the short name shown to the user.
func vendorLabel(vendor device.Vendor) string {
	switch vendor {
	case device.VendorMikroTik:
		return "MikroTik"
	case device.VendorHuawei, device.VendorHuaweiJSON:
		return "Huawei"
	case device.VendorZTE:
		return "ZTE"
	case device.VendorSSH:
		return "SSH"
	default:
		return "Auto"
	}
}

// shortReason maps an internal error to a plain phrase.
func shortReason(vendor device.Vendor, err error) string {
	if err == nil {
		return "no response"
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "http 404"):
		return "HTTP 404"
	case strings.Contains(text, "cannot obtain csrf token"):
		return "no access token"
	case strings.Contains(text, "login failed"):
		return "login not found on this firmware"
	case strings.Contains(text, "connectex"), strings.Contains(text, "connection refused"), strings.Contains(text, "actively refused"), strings.Contains(text, "recusou ativamente"):
		if vendor == device.VendorSSH {
			return "port 22 closed"
		}
		return "no connection"
	case strings.Contains(text, "i/o timeout"), strings.Contains(text, "context deadline"), strings.Contains(text, "tempo limite"):
		return "no response"
	case strings.Contains(text, "401"), strings.Contains(text, "unauthorized"):
		return "credentials rejected"
	default:
		return "failed"
	}
}

// Driver wraps the driver that answered successfully.
type Driver struct {
	inner device.Driver
}

// New probes the device, trying the preferred vendor first when supplied.
func New(cfg device.Config, preferred ...device.Vendor) (device.Driver, error) {
	order := make([]device.Vendor, 0, len(builders))
	for _, vendor := range preferred {
		if _, ok := builders[vendor]; ok {
			order = append(order, vendor)
		}
	}
	for _, vendor := range probeOrder {
		already := false
		for _, existing := range order {
			if existing == vendor {
				already = true
				break
			}
		}
		if !already {
			order = append(order, vendor)
		}
	}

	reasons := make([]Reason, 0, len(order))
	for _, vendor := range order {
		candidate, err := builders[vendor](cfg)
		if err != nil {
			reasons = append(reasons, Reason{Vendor: vendor, Message: shortReason(vendor, err), Detail: err.Error()})
			continue
		}
		systemDriver, ok := candidate.(device.SystemDriver)
		if !ok {
			reasons = append(reasons, Reason{Vendor: vendor, Message: "sem leitura de sistema"})
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		_, err = systemDriver.SystemInfo(ctx)
		cancel()
		if err == nil {
			return &Driver{inner: candidate}, nil
		}
		_ = candidate.Close()
		reasons = append(reasons, Reason{Vendor: vendor, Message: shortReason(vendor, err), Detail: err.Error()})
	}
	return nil, &DetectError{Reasons: reasons}
}

// Vendor returns the detected vendor.
func (d *Driver) Vendor() device.Vendor { return d.inner.Vendor() }

// Capabilities returns the detected driver's capabilities.
func (d *Driver) Capabilities() []string { return d.inner.Capabilities() }

// Close releases the detected driver.
func (d *Driver) Close() error { return d.inner.Close() }

// SystemInfo delegates to the detected driver.
func (d *Driver) SystemInfo(ctx context.Context) (device.SystemInfo, error) {
	return d.inner.(device.SystemDriver).SystemInfo(ctx)
}

// Interfaces delegates when supported.
func (d *Driver) Interfaces(ctx context.Context) ([]device.Interface, error) {
	if driver, ok := d.inner.(device.InterfaceDriver); ok {
		return driver.Interfaces(ctx)
	}
	return nil, device.ErrUnsupported
}

// IPAddresses delegates when supported.
func (d *Driver) IPAddresses(ctx context.Context) ([]device.IPAddress, error) {
	if driver, ok := d.inner.(device.IPAddressDriver); ok {
		return driver.IPAddresses(ctx)
	}
	return nil, device.ErrUnsupported
}

// Routes delegates when supported.
func (d *Driver) Routes(ctx context.Context) ([]device.Route, error) {
	if driver, ok := d.inner.(device.RouteDriver); ok {
		return driver.Routes(ctx)
	}
	return nil, device.ErrUnsupported
}

// Neighbors delegates when supported.
func (d *Driver) Neighbors(ctx context.Context) ([]device.Neighbor, error) {
	if driver, ok := d.inner.(device.NeighborDriver); ok {
		return driver.Neighbors(ctx)
	}
	return nil, device.ErrUnsupported
}

// DHCPLeases delegates when supported.
func (d *Driver) DHCPLeases(ctx context.Context) ([]device.DHCPLease, error) {
	if driver, ok := d.inner.(device.DHCPDriver); ok {
		return driver.DHCPLeases(ctx)
	}
	return nil, device.ErrUnsupported
}

// WiFiClients delegates when supported.
func (d *Driver) WiFiClients(ctx context.Context) ([]device.WiFiClient, error) {
	if driver, ok := d.inner.(device.WiFiDriver); ok {
		return driver.WiFiClients(ctx)
	}
	return nil, device.ErrUnsupported
}

// Firewall delegates when supported.
func (d *Driver) Firewall(ctx context.Context) ([]device.FirewallRule, error) {
	if driver, ok := d.inner.(device.FirewallDriver); ok {
		return driver.Firewall(ctx)
	}
	return nil, device.ErrUnsupported
}

// PortForwards delegates when supported.
func (d *Driver) PortForwards(ctx context.Context) ([]device.PortForward, error) {
	if driver, ok := d.inner.(device.PortForwardDriver); ok {
		return driver.PortForwards(ctx)
	}
	return nil, device.ErrUnsupported
}

// Logs delegates when supported.
func (d *Driver) Logs(ctx context.Context, limit int, topics string) ([]device.LogEntry, error) {
	if driver, ok := d.inner.(device.LogsDriver); ok {
		return driver.Logs(ctx, limit, topics)
	}
	return nil, device.ErrUnsupported
}

// Ping delegates when supported.
func (d *Driver) Ping(ctx context.Context, target string, count int) (device.PingResult, error) {
	if driver, ok := d.inner.(device.PingDriver); ok {
		return driver.Ping(ctx, target, count)
	}
	return device.PingResult{}, device.ErrUnsupported
}

// ExportConfig delegates when supported.
func (d *Driver) ExportConfig(ctx context.Context) (string, error) {
	if driver, ok := d.inner.(device.ConfigDriver); ok {
		return driver.ExportConfig(ctx)
	}
	return "", device.ErrUnsupported
}

// Reboot delegates when supported.
func (d *Driver) Reboot(ctx context.Context) error {
	if driver, ok := d.inner.(device.RebootDriver); ok {
		return driver.Reboot(ctx)
	}
	return device.ErrUnsupported
}

// Command delegates when supported.
func (d *Driver) Command(ctx context.Context, command string, params map[string]any) (any, error) {
	if driver, ok := d.inner.(device.CommandDriver); ok {
		return driver.Command(ctx, command, params)
	}
	return nil, device.ErrUnsupported
}

// HTTP delegates when supported.
func (d *Driver) HTTP(ctx context.Context, method, path string, body []byte, headers map[string]string) (device.HTTPResponse, error) {
	if driver, ok := d.inner.(device.HTTPDriver); ok {
		return driver.HTTP(ctx, method, path, body, headers)
	}
	return device.HTTPResponse{}, device.ErrUnsupported
}

// Compile-time assertions.
var (
	_ device.Driver       = (*Driver)(nil)
	_ device.SystemDriver = (*Driver)(nil)
)
