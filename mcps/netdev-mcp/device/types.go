package device

import (
	"fmt"
	"strings"

	"github.com/gtihq/ai-mcps/internal/omap"
)

// SystemInfo is the normalized identity and resource snapshot of a device.
type SystemInfo struct {
	Name      string
	Vendor    Vendor
	Model     string
	Host      string
	Hostname  string
	Version   string
	Uptime    string
	CPU       string
	Memory    string
	Serial    string
	Site      string
	Reachable bool
	Extra     map[string]any
}

// Map converts the value into an ordered MCP-friendly object.
func (v SystemInfo) Map() *omap.M {
	m := omap.From(
		"name", v.Name,
		"vendor", string(v.Vendor),
		"model", v.Model,
		"host", v.Host,
		"hostname", v.Hostname,
		"version", v.Version,
		"uptime", v.Uptime,
		"cpu", v.CPU,
		"memory", v.Memory,
		"serial", v.Serial,
		"site", v.Site,
		"reachable", v.Reachable,
	)
	return withExtra(m, v.Extra)
}

// Interface is one network interface or switch port.
type Interface struct {
	Name    string
	Type    string
	MAC     string
	Status  string
	Speed   string
	MTU     string
	RxBytes any
	TxBytes any
	Comment string
	Extra   map[string]any
}

// Map converts the value into an ordered MCP-friendly object.
func (v Interface) Map() *omap.M {
	m := omap.From(
		"name", v.Name,
		"type", v.Type,
		"mac", v.MAC,
		"status", v.Status,
		"speed", v.Speed,
		"mtu", v.MTU,
		"rx_bytes", v.RxBytes,
		"tx_bytes", v.TxBytes,
		"comment", v.Comment,
	)
	return withExtra(m, v.Extra)
}

// IPAddress binds an address to an interface.
type IPAddress struct {
	Interface string
	Address   string
	Network   string
	Extra     map[string]any
}

// Map converts the value into an ordered MCP-friendly object.
func (v IPAddress) Map() *omap.M {
	m := omap.From(
		"interface", v.Interface,
		"address", v.Address,
		"network", v.Network,
	)
	return withExtra(m, v.Extra)
}

// Route is one entry in the routing table.
type Route struct {
	Destination string
	Gateway     string
	Interface   string
	Distance    any
	Metric      any
	Active      bool
	Comment     string
	Extra       map[string]any
}

// Map converts the value into an ordered MCP-friendly object.
func (v Route) Map() *omap.M {
	m := omap.From(
		"destination", v.Destination,
		"gateway", v.Gateway,
		"interface", v.Interface,
		"distance", v.Distance,
		"metric", v.Metric,
		"active", v.Active,
		"comment", v.Comment,
	)
	return withExtra(m, v.Extra)
}

// Neighbor is a discovered adjacent device.
type Neighbor struct {
	IP           string
	MAC          string
	Hostname     string
	Platform     string
	Interface    string
	Capabilities string
	Extra        map[string]any
}

// Map converts the value into an ordered MCP-friendly object.
func (v Neighbor) Map() *omap.M {
	m := omap.From(
		"ip", v.IP,
		"mac", v.MAC,
		"hostname", v.Hostname,
		"platform", v.Platform,
		"interface", v.Interface,
		"capabilities", v.Capabilities,
	)
	return withExtra(m, v.Extra)
}

// DHCPLease is one active DHCP client.
type DHCPLease struct {
	Hostname string
	MAC      string
	IP       string
	Lease    string
	Status   string
	Extra    map[string]any
}

// Map converts the value into an ordered MCP-friendly object.
func (v DHCPLease) Map() *omap.M {
	m := omap.From(
		"hostname", v.Hostname,
		"mac", v.MAC,
		"ip", v.IP,
		"lease", v.Lease,
		"status", v.Status,
	)
	return withExtra(m, v.Extra)
}

// WiFiClient is one associated wireless station.
type WiFiClient struct {
	MAC       string
	IP        string
	SSID      string
	Interface string
	Signal    string
	RxRate    string
	TxRate    string
	Extra     map[string]any
}

// Map converts the value into an ordered MCP-friendly object.
func (v WiFiClient) Map() *omap.M {
	m := omap.From(
		"mac", v.MAC,
		"ip", v.IP,
		"ssid", v.SSID,
		"interface", v.Interface,
		"signal", v.Signal,
		"rx_rate", v.RxRate,
		"tx_rate", v.TxRate,
	)
	return withExtra(m, v.Extra)
}

// FirewallRule is one firewall or NAT rule.
type FirewallRule struct {
	Chain    string
	Action   string
	Protocol string
	Source   string
	Target   string
	Port     string
	Comment  string
	Disabled bool
	Kind     string
	Extra    map[string]any
}

// Map converts the value into an ordered MCP-friendly object.
func (v FirewallRule) Map() *omap.M {
	m := omap.From(
		"chain", v.Chain,
		"action", v.Action,
		"protocol", v.Protocol,
		"source", v.Source,
		"target", v.Target,
		"port", v.Port,
		"comment", v.Comment,
		"disabled", v.Disabled,
		"kind", v.Kind,
	)
	return withExtra(m, v.Extra)
}

// PortForward maps an external port to an internal host.
type PortForward struct {
	Name     string
	Protocol string
	External string
	Internal string
	Target   string
	Enabled  bool
	Extra    map[string]any
}

// Map converts the value into an ordered MCP-friendly object.
func (v PortForward) Map() *omap.M {
	m := omap.From(
		"name", v.Name,
		"protocol", v.Protocol,
		"external", v.External,
		"internal", v.Internal,
		"target", v.Target,
		"enabled", v.Enabled,
	)
	return withExtra(m, v.Extra)
}

// LogEntry is one device log line.
type LogEntry struct {
	Time    string
	Topics  string
	Message string
	Extra   map[string]any
}

// Map converts the value into an ordered MCP-friendly object.
func (v LogEntry) Map() *omap.M {
	m := omap.From(
		"time", v.Time,
		"topics", v.Topics,
		"message", v.Message,
	)
	return withExtra(m, v.Extra)
}

// PingResult summarizes a ping run from the device.
type PingResult struct {
	Target     string
	Sent       any
	Received   any
	Loss       any
	AvgRTT     any
	PacketLoss string
	Extra      map[string]any
}

// Map converts the value into an ordered MCP-friendly object.
func (v PingResult) Map() *omap.M {
	m := omap.From(
		"target", v.Target,
		"sent", v.Sent,
		"received", v.Received,
		"packet_loss", firstNonNil(v.Loss, v.PacketLoss),
		"avg_rtt", v.AvgRTT,
	)
	return withExtra(m, v.Extra)
}

// HTTPResponse is the normalized result of a raw HTTP request.
type HTTPResponse struct {
	Status  int
	Body    string
	Headers map[string]string
}

// Map converts the value into an ordered MCP-friendly object.
func (v HTTPResponse) Map() *omap.M {
	headers := omap.New()
	for _, k := range sortedKeys(v.Headers) {
		headers.Set(k, v.Headers[k])
	}
	return omap.From(
		"status", v.Status,
		"body", v.Body,
		"headers", headers,
	)
}

// CapabilityReport describes one device and what it can do.
type CapabilityReport struct {
	Name         string
	Vendor       Vendor
	Model        string
	Site         string
	Host         string
	Capabilities []string
	Error        string
}

// Map converts the value into an ordered MCP-friendly object.
func (v CapabilityReport) Map() *omap.M {
	return omap.From(
		"name", v.Name,
		"vendor", string(v.Vendor),
		"model", v.Model,
		"site", v.Site,
		"host", v.Host,
		"capabilities", v.Capabilities,
		"error", v.Error,
	)
}

// withExtra appends optional keys after the standard shape so callers can
// carry vendor-specific fields without changing the common output.
func withExtra(m *omap.M, extra map[string]any) *omap.M {
	for _, k := range sortedKeys(extra) {
		if _, exists := m.Get(k); !exists {
			m.Set(k, extra[k])
		}
	}
	return m
}

// firstNonNil returns the first value that is not nil.
func firstNonNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

// List maps a slice of values into a slice of ordered objects.
func List[T interface{ Map() *omap.M }](items []T) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.Map())
	}
	return out
}

// BoolText interprets common vendor boolean spellings.
func BoolText(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "1", "enabled", "up", "running", "on":
		return true
	default:
		return false
	}
}

// StringOr returns s when non-empty, otherwise the fallback.
func StringOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// FormatInt converts an integer-like value to string without scientific
// notation, which some agents render poorly.
func FormatInt(v any) string {
	switch n := v.(type) {
	case nil:
		return ""
	case float64:
		return fmt.Sprintf("%.0f", n)
	default:
		return fmt.Sprint(n)
	}
}
