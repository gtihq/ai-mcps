// Package device defines the vendor-neutral contracts used by every
// network device driver in netdev-mcp.
package device

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Vendor identifies the driver implementation for a device.
type Vendor string

// Supported vendors.
const (
	VendorMikroTik   Vendor = "mikrotik"
	VendorZTE        Vendor = "zte"
	VendorHuawei     Vendor = "huawei"
	VendorHuaweiJSON Vendor = "huawei_json"
	VendorSSH        Vendor = "ssh"
	VendorAuto       Vendor = "auto"
)

// Capability names advertised by drivers and exposed through the MCP tools.
const (
	CapSystem            = "system_info"
	CapInterfaces        = "interfaces"
	CapIPAddresses       = "ip_addresses"
	CapRoutes            = "routes"
	CapNeighbors         = "neighbors"
	CapDHCP              = "dhcp_leases"
	CapDhcpReservation   = "dhcp_reservation"
	CapRenameReservation = "rename_reservation"
	CapWiFiConfig        = "wifi_config"
	CapWiFi              = "wifi_clients"
	CapFirewall          = "firewall"
	CapPortForwards      = "port_forwards"
	CapLogs              = "logs"
	CapPing              = "ping"
	CapConfig            = "config"
	CapReboot            = "reboot"
	CapCommand           = "command"
	CapHTTP              = "http"
)

// Common errors returned by drivers and by the tool layer.
var (
	ErrUnsupported = errors.New("capability not supported by this device")
	ErrNotFound    = errors.New("device not found")
	ErrDisabled    = errors.New("device is disabled")
	ErrNoResponse  = errors.New("device did not respond")
)

// Config is one entry in the device inventory. Secrets should be supplied as
// ${ENV_VAR} references so the inventory file can be committed safely.
type Config struct {
	Name        string            `json:"name"`
	Vendor      Vendor            `json:"vendor"`
	Model       string            `json:"model,omitempty"`
	Site        string            `json:"site,omitempty"`
	Host        string            `json:"host"`
	Port        int               `json:"port,omitempty"`
	Username    string            `json:"username,omitempty"`
	Password    string            `json:"password,omitempty"`
	TLS         bool              `json:"tls,omitempty"`
	InsecureTLS bool              `json:"insecure_tls,omitempty"`
	Disabled    bool              `json:"disabled,omitempty"`
	Profile     string            `json:"profile,omitempty"`
	Endpoints   map[string]string `json:"endpoints,omitempty"`
	Extra       map[string]any    `json:"extra,omitempty"`
	Timeout     string            `json:"timeout,omitempty"`
}

// Validate checks the minimum fields needed to build a driver.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("device name is required")
	}
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("device %q: host is required", c.Name)
	}
	switch c.Vendor {
	case VendorMikroTik, VendorZTE, VendorHuawei, VendorHuaweiJSON, VendorSSH, VendorAuto:
		return nil
	default:
		return fmt.Errorf("device %q: unknown vendor %q", c.Name, c.Vendor)
	}
}

// Address returns host:port using the supplied fallback port when Config.Port
// is zero.
func (c Config) Address(fallback int) string {
	host := strings.TrimSpace(c.Host)
	port := c.Port
	if port == 0 {
		port = fallback
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
}

// Duration returns the configured timeout or the supplied default.
func (c Config) Duration(fallback time.Duration) time.Duration {
	if strings.TrimSpace(c.Timeout) == "" {
		return fallback
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

// Endpoint returns a configured endpoint override or the supplied default.
func (c Config) Endpoint(key, fallback string) string {
	if v, ok := c.Endpoints[key]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

// Driver is the base contract every device implementation satisfies.
type Driver interface {
	Vendor() Vendor
	Capabilities() []string
	Close() error
}

// SystemDriver reports device identity and resource usage.
type SystemDriver interface {
	SystemInfo(ctx context.Context) (SystemInfo, error)
}

// InterfaceDriver lists physical and logical interfaces.
type InterfaceDriver interface {
	Interfaces(ctx context.Context) ([]Interface, error)
}

// IPAddressDriver lists configured IP addresses.
type IPAddressDriver interface {
	IPAddresses(ctx context.Context) ([]IPAddress, error)
}

// RouteDriver lists routing table entries.
type RouteDriver interface {
	Routes(ctx context.Context) ([]Route, error)
}

// NeighborDriver lists discovered Layer 2 or Layer 3 neighbors.
type NeighborDriver interface {
	Neighbors(ctx context.Context) ([]Neighbor, error)
}

// DHCPDriver lists active DHCP leases.
type DHCPDriver interface {
	DHCPLeases(ctx context.Context) ([]DHCPLease, error)
}

// ReservationDriver creates and renames static DHCP bindings.
type ReservationDriver interface {
	AddReservation(ctx context.Context, name, mac, ip string) error
	RenameReservation(ctx context.Context, oldName, newName string) error
}

// WiFiNetwork is one configured wireless network.
type WiFiNetwork struct {
	SSID    string
	Band    string
	Enabled bool
	ID      string
}

// WiFiConfigDriver reads and updates wireless network settings.
type WiFiConfigDriver interface {
	WiFiNetworks(ctx context.Context) ([]WiFiNetwork, error)
	UpdateWiFi(ctx context.Context, ssid, newSSID, newPassword string) error
}

// WiFiDriver lists associated wireless clients.
type WiFiDriver interface {
	WiFiClients(ctx context.Context) ([]WiFiClient, error)
}

// FirewallDriver lists firewall and NAT rules.
type FirewallDriver interface {
	Firewall(ctx context.Context) ([]FirewallRule, error)
}

// PortForwardDriver lists destination NAT or port forwarding rules.
type PortForwardDriver interface {
	PortForwards(ctx context.Context) ([]PortForward, error)
}

// LogsDriver reads device logs.
type LogsDriver interface {
	Logs(ctx context.Context, limit int, topics string) ([]LogEntry, error)
}

// PingDriver runs a ping from the device itself.
type PingDriver interface {
	Ping(ctx context.Context, target string, count int) (PingResult, error)
}

// ConfigDriver exports the running configuration.
type ConfigDriver interface {
	ExportConfig(ctx context.Context) (string, error)
}

// RebootDriver reboots the device.
type RebootDriver interface {
	Reboot(ctx context.Context) error
}

// CommandDriver executes a raw vendor command.
type CommandDriver interface {
	Command(ctx context.Context, command string, params map[string]any) (any, error)
}

// HTTPDriver issues a raw HTTP request against the device API.
type HTTPDriver interface {
	HTTP(ctx context.Context, method, path string, body []byte, headers map[string]string) (HTTPResponse, error)
}
