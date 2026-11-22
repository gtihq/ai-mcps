// Package mikrotik implements the device drivers for MikroTik RouterOS.
// It speaks the REST API (v7), the binary API on ports 8728/8729 and SSH for
// the operations that have no API equivalent, such as ping and config export.
package mikrotik

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/mcps/netdev-mcp/device"
)

const defaultTimeout = 15 * time.Second

// restStatusError marks an HTTP error reply so the API fallback is only used
// for transport failures, not for a device answering 404 on a wrong path.
type restStatusError struct {
	status int
	body   string
}

// Error implements the error interface.
func (e *restStatusError) Error() string {
	return fmt.Sprintf("routeros rest: HTTP %d: %s", e.status, e.body)
}

// Driver implements all read capabilities plus a raw command escape hatch.
type Driver struct {
	cfg     device.Config
	timeout time.Duration
	client  *http.Client
	baseURL string
	api     *apiClient
}

// New creates a RouterOS driver. No connection is opened until first use.
func New(cfg device.Config) (device.Driver, error) {
	timeout := cfg.Duration(defaultTimeout)
	port := 80
	scheme := "http"
	if cfg.TLS {
		port = 443
		scheme = "https"
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureTLS},
		},
	}
	return &Driver{
		cfg:     cfg,
		timeout: timeout,
		client:  client,
		baseURL: scheme + "://" + cfg.Address(port) + "/rest",
		api:     newAPIClient(cfg, timeout),
	}, nil
}

// Vendor returns the vendor identifier.
func (d *Driver) Vendor() device.Vendor { return device.VendorMikroTik }

// Capabilities lists the tools this driver implements.
func (d *Driver) Capabilities() []string {
	return []string{
		device.CapSystem, device.CapInterfaces, device.CapIPAddresses,
		device.CapRoutes, device.CapNeighbors, device.CapDHCP, device.CapWiFi,
		device.CapFirewall, device.CapPortForwards, device.CapLogs,
		device.CapPing, device.CapConfig, device.CapReboot, device.CapCommand,
	}
}

// Close releases the binary API connection when one was opened.
func (d *Driver) Close() error {
	d.client.CloseIdleConnections()
	return d.api.Close()
}

// #region REST transport

// rest performs one request against the RouterOS REST API and decodes a JSON
// response. UseNumber keeps integer spellings intact for agent output.
func (d *Driver) rest(ctx context.Context, method, path string, query url.Values, body any) (any, error) {
	target := d.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(d.cfg.Username, d.cfg.Password)
	req.Header.Set("accept", "application/json")
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &restStatusError{status: resp.StatusCode, body: strings.TrimSpace(string(data))}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// rows returns all objects from a REST print path, falling back to the binary
// API when REST is disabled on the router.
func (d *Driver) rows(ctx context.Context, path string) ([]map[string]any, error) {
	value, err := d.rest(ctx, http.MethodGet, path, nil, nil)
	if err == nil {
		out := make([]map[string]any, 0)
		for _, item := range device.MapSlice(value) {
			if row := device.MapAny(item); row != nil {
				out = append(out, row)
			}
		}
		if obj := device.MapAny(value); obj != nil {
			out = append(out, obj)
		}
		return out, nil
	}
	var statusErr *restStatusError
	if errors.As(err, &statusErr) {
		return nil, err
	}
	return d.api.print(ctx, apiPath(path), nil)
}

// apiPath converts a REST path to its binary API print command.
func apiPath(path string) string {
	clean := "/" + strings.Trim(path, "/")
	if strings.HasSuffix(clean, "/print") {
		return clean
	}
	return clean + "/print"
}

// #endregion

// #region Normalized capabilities

// SystemInfo merges /system/resource, /system/identity and /system/clock.
func (d *Driver) SystemInfo(ctx context.Context) (device.SystemInfo, error) {
	resource, err := d.rows(ctx, "/system/resource")
	if err != nil {
		return device.SystemInfo{}, err
	}
	info := device.SystemInfo{
		Name:      d.cfg.Name,
		Vendor:    device.VendorMikroTik,
		Model:     d.cfg.Model,
		Host:      d.cfg.Host,
		Site:      d.cfg.Site,
		Reachable: true,
	}
	if len(resource) > 0 {
		r := resource[0]
		info.Version = device.Str(r, "version")
		info.Uptime = first(info.Uptime, r, "uptime")
		info.CPU = device.Str(r, "cpu-load", "cpu")
		info.Memory = memory(r)
		info.Serial = device.Str(r, "board-name", "serial-number")
		if info.Model == "" {
			info.Model = device.Str(r, "board-name")
		}
		info.Extra = device.Extra(r, "version", "uptime", "cpu-load", "cpu", "free-memory", "total-memory")
	}
	if identity, err := d.rows(ctx, "/system/identity"); err == nil && len(identity) > 0 {
		info.Hostname = device.Str(identity[0], "name")
	}
	return info, nil
}

// first returns the first non-empty field from a row.
func first(current string, row map[string]any, keys ...string) string {
	if current != "" {
		return current
	}
	return device.Str(row, keys...)
}

// memory formats free and total memory when both are available.
func memory(row map[string]any) string {
	free := device.Str(row, "free-memory")
	total := device.Str(row, "total-memory")
	if free == "" {
		return ""
	}
	if total == "" {
		return free
	}
	return free + "/" + total
}

// Interfaces lists all RouterOS interfaces.
func (d *Driver) Interfaces(ctx context.Context) ([]device.Interface, error) {
	rows, err := d.rows(ctx, "/interface")
	if err != nil {
		return nil, err
	}
	out := make([]device.Interface, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.Interface{
			Name:    device.Str(r, "name", ".id"),
			Type:    device.Str(r, "type"),
			MAC:     device.Str(r, "mac-address"),
			Status:  runningStatus(r),
			Speed:   device.Str(r, "speed"),
			MTU:     device.Str(r, "mtu"),
			RxBytes: device.First(r, "rx-byte"),
			TxBytes: device.First(r, "tx-byte"),
			Comment: device.Str(r, "comment"),
			Extra:   device.Extra(r, "name", "type", "mac-address", "running", "disabled", "speed", "mtu", "rx-byte", "tx-byte", "comment"),
		})
	}
	return out, nil
}

// runningStatus combines running and disabled into one status word.
func runningStatus(row map[string]any) string {
	if device.MapBool(row["disabled"]) {
		return "disabled"
	}
	if device.MapBool(row["running"]) {
		return "up"
	}
	return "down"
}

// IPAddresses lists all configured addresses.
func (d *Driver) IPAddresses(ctx context.Context) ([]device.IPAddress, error) {
	rows, err := d.rows(ctx, "/ip/address")
	if err != nil {
		return nil, err
	}
	out := make([]device.IPAddress, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.IPAddress{
			Interface: device.Str(r, "interface"),
			Address:   device.Str(r, "address"),
			Network:   device.Str(r, "network"),
			Extra:     device.Extra(r, "interface", "address", "network"),
		})
	}
	return out, nil
}

// Routes lists IPv4 routes.
func (d *Driver) Routes(ctx context.Context) ([]device.Route, error) {
	rows, err := d.rows(ctx, "/ip/route")
	if err != nil {
		return nil, err
	}
	out := make([]device.Route, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.Route{
			Destination: device.Str(r, "dst-address", "destination"),
			Gateway:     device.Str(r, "gateway"),
			Interface:   device.Str(r, "immediate-gw", "interface"),
			Distance:    device.First(r, "distance"),
			Metric:      device.First(r, "route-tag"),
			Active:      !device.MapBool(r["disabled"]) && !device.MapBool(r["inactive"]),
			Comment:     device.Str(r, "comment"),
			Extra:       device.Extra(r, "dst-address", "gateway", "immediate-gw", "distance", "route-tag", "disabled", "inactive", "comment"),
		})
	}
	return out, nil
}

// Neighbors lists LLDP, CDP and MNDP neighbors.
func (d *Driver) Neighbors(ctx context.Context) ([]device.Neighbor, error) {
	rows, err := d.rows(ctx, "/ip/neighbor")
	if err != nil {
		return nil, err
	}
	out := make([]device.Neighbor, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.Neighbor{
			IP:           device.Str(r, "address", "ip-address"),
			MAC:          device.Str(r, "mac-address"),
			Hostname:     device.Str(r, "identity", "host-name"),
			Platform:     device.Str(r, "platform"),
			Interface:    device.Str(r, "interface"),
			Capabilities: device.Str(r, "interface-name", "capabilities"),
			Extra:        device.Extra(r, "address", "mac-address", "identity", "platform", "interface"),
		})
	}
	return out, nil
}

// DHCPLeases lists DHCP server leases.
func (d *Driver) DHCPLeases(ctx context.Context) ([]device.DHCPLease, error) {
	rows, err := d.rows(ctx, "/ip/dhcp-server/lease")
	if err != nil {
		return nil, err
	}
	out := make([]device.DHCPLease, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.DHCPLease{
			Hostname: device.Str(r, "host-name"),
			MAC:      device.Str(r, "mac-address"),
			IP:       device.Str(r, "address"),
			Lease:    device.Str(r, "expires-after", "lease-time"),
			Status:   device.Str(r, "status", "dynamic", "state"),
			Extra:    device.Extra(r, "host-name", "mac-address", "address", "expires-after", "status"),
		})
	}
	return out, nil
}

// WiFiClients tries the v7 wifi registration table first, then the v6 path.
func (d *Driver) WiFiClients(ctx context.Context) ([]device.WiFiClient, error) {
	rows, err := d.rows(ctx, "/interface/wifi/registration-table")
	if err != nil || len(rows) == 0 {
		rows, err = d.rows(ctx, "/interface/wireless/registration-table")
	}
	if err != nil {
		return nil, err
	}
	out := make([]device.WiFiClient, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.WiFiClient{
			MAC:       device.Str(r, "mac-address"),
			IP:        device.Str(r, "address", "ip-address"),
			SSID:      device.Str(r, "ssid"),
			Interface: device.Str(r, "interface", "radio"),
			Signal:    device.Str(r, "signal-strength", "rssi"),
			RxRate:    device.Str(r, "rx-rate"),
			TxRate:    device.Str(r, "tx-rate"),
			Extra:     device.Extra(r, "mac-address", "signal-strength", "rx-rate", "tx-rate", "ssid"),
		})
	}
	return out, nil
}

// Firewall merges filter and NAT rules into one list.
func (d *Driver) Firewall(ctx context.Context) ([]device.FirewallRule, error) {
	filter, err := d.rows(ctx, "/ip/firewall/filter")
	if err != nil {
		return nil, err
	}
	nat, err := d.rows(ctx, "/ip/firewall/nat")
	if err != nil {
		return nil, err
	}
	out := make([]device.FirewallRule, 0, len(filter)+len(nat))
	for _, kind := range []struct {
		name string
		rows []map[string]any
	}{{"filter", filter}, {"nat", nat}} {
		for _, r := range kind.rows {
			port := device.Str(r, "dst-port", "to-ports", "src-port")
			if protocol := device.Str(r, "protocol"); protocol != "" && port != "" {
				port = protocol + "/" + port
			}
			out = append(out, device.FirewallRule{
				Chain:    device.Str(r, "chain"),
				Action:   device.Str(r, "action"),
				Protocol: device.Str(r, "protocol"),
				Source:   device.Str(r, "src-address"),
				Target:   device.Str(r, "dst-address", "to-addresses"),
				Port:     port,
				Comment:  device.Str(r, "comment"),
				Disabled: device.MapBool(r["disabled"]),
				Kind:     kind.name,
				Extra:    device.Extra(r, "chain", "action", "protocol", "src-address", "dst-address", "to-addresses", "comment", "disabled"),
			})
		}
	}
	return out, nil
}

// PortForwards extracts destination NAT rules.
func (d *Driver) PortForwards(ctx context.Context) ([]device.PortForward, error) {
	rules, err := d.Firewall(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]device.PortForward, 0)
	for _, r := range rules {
		if r.Kind != "nat" || !strings.EqualFold(r.Action, "dst-nat") {
			continue
		}
		out = append(out, device.PortForward{
			Name:     device.StringOr(r.Comment, device.Str(r.Extra, ".id")),
			Protocol: r.Protocol,
			External: r.Port,
			Internal: device.Str(r.Extra, "to-ports"),
			Target:   device.Str(r.Extra, "to-addresses"),
			Enabled:  !r.Disabled,
			Extra:    r.Extra,
		})
	}
	return out, nil
}

// Logs reads recent log entries and optionally filters topics by substring.
func (d *Driver) Logs(ctx context.Context, limit int, topics string) ([]device.LogEntry, error) {
	rows, err := d.rows(ctx, "/log")
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(topics)
	out := make([]device.LogEntry, 0, len(rows))
	for _, r := range rows {
		entry := device.LogEntry{
			Time:    device.Str(r, "time"),
			Topics:  device.Str(r, "topics"),
			Message: device.Str(r, "message"),
			Extra:   device.Extra(r, "time", "topics", "message"),
		}
		if needle != "" && !strings.Contains(strings.ToLower(entry.Topics), needle) {
			continue
		}
		out = append(out, entry)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// #endregion

// #region Actions

// Ping runs /ping over SSH because REST has no streaming equivalent.
func (d *Driver) Ping(ctx context.Context, target string, count int) (device.PingResult, error) {
	if count <= 0 {
		count = 4
	}
	command := "/ping " + shellQuote(target) + " count=" + strconv.Itoa(count)
	out, err := sshExec(ctx, d.cfg, d.timeout, command)
	if err != nil {
		return device.PingResult{}, err
	}
	return parseRouterOSPing(target, out), nil
}

// pingTimeRE matches both tabular (10ms) and key=value (time=10ms) RTTs.
var pingTimeRE = regexp.MustCompile(`(?i)^(?:time=)?([0-9]+(?:\.[0-9]+)?)(us|ms|s)$`)

// parseRouterOSPing turns the tabular /ping output into a summary.
func parseRouterOSPing(target, output string) device.PingResult {
	result := device.PingResult{Target: target, Extra: map[string]any{"raw": output}}
	sent, received := 0, 0
	rtts := make([]float64, 0)
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(strings.ToUpper(trimmed), "SEQ") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		if _, err := strconv.Atoi(fields[0]); err != nil {
			continue
		}
		sent++
		failed := strings.Contains(strings.ToLower(trimmed), "timeout") || strings.Contains(strings.ToLower(trimmed), "no reply")
		for _, field := range fields {
			match := pingTimeRE.FindStringSubmatch(field)
			if match == nil {
				continue
			}
			value, err := strconv.ParseFloat(match[1], 64)
			if err != nil {
				continue
			}
			if !failed {
				switch strings.ToLower(match[2]) {
				case "us":
					value /= 1000
				case "s":
					value *= 1000
				}
				received++
				rtts = append(rtts, value)
			}
			break
		}
	}
	result.Sent = sent
	result.Received = received
	if sent > 0 {
		result.Loss = float64(sent-received) / float64(sent) * 100
	}
	if len(rtts) > 0 {
		sum := 0.0
		for _, v := range rtts {
			sum += v
		}
		result.AvgRTT = fmt.Sprintf("%.2fms", sum/float64(len(rtts)))
	}
	return result
}

// ExportConfig returns a sensitive-field-free config export over SSH.
func (d *Driver) ExportConfig(ctx context.Context) (string, error) {
	return sshExec(ctx, d.cfg, d.timeout, "/export hide-sensitive")
}

// Reboot reboots the router through REST, with the binary API as fallback.
func (d *Driver) Reboot(ctx context.Context) error {
	_, err := d.rest(ctx, http.MethodPost, "/system/reboot", nil, map[string]any{})
	if err == nil {
		return nil
	}
	_, apiErr := d.api.run(ctx, "/system/reboot", nil)
	return apiErr
}

// Command executes a raw RouterOS command. REST is used by default; pass
// {"protocol":"api"} in params to force the binary API.
func (d *Driver) Command(ctx context.Context, command string, params map[string]any) (any, error) {
	if params == nil {
		params = map[string]any{}
	}
	if strings.EqualFold(device.MapString(params["protocol"]), "api") {
		return d.api.run(ctx, command, withoutKeys(params, "protocol"))
	}
	path, method, body, query := translateCommand(command, params)
	value, err := d.rest(ctx, method, path, query, body)
	if err != nil {
		return nil, err
	}
	return value, nil
}

// translateCommand maps a RouterOS CLI style command to a REST call.
func translateCommand(command string, params map[string]any) (path, method string, body any, query url.Values) {
	command = "/" + strings.TrimSpace(strings.TrimPrefix(command, "/"))
	parts := strings.Split(strings.Trim(command, "/"), "/")
	verb := ""
	if len(parts) > 0 {
		candidate := parts[len(parts)-1]
		switch candidate {
		case "print", "getall", "add", "remove", "set", "enable", "disable", "reboot":
			verb = candidate
			parts = parts[:len(parts)-1]
		}
	}
	path = "/" + strings.Join(parts, "/")
	if verb == "" || verb == "print" || verb == "getall" {
		return path, http.MethodGet, nil, nil
	}
	clean := withoutKeys(params, "protocol")
	if verb == "remove" {
		id := device.MapString(clean[".id"])
		query = url.Values{}
		query.Set(".id", id)
		return path, http.MethodDelete, nil, query
	}
	return path + "/" + verb, http.MethodPost, clean, nil
}

// withoutKeys copies a parameter map while dropping the listed keys.
func withoutKeys(params map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(params))
	for k, v := range params {
		skip := false
		for _, key := range keys {
			if k == key {
				skip = true
				break
			}
		}
		if !skip {
			out[k] = v
		}
	}
	return out
}

// shellQuote wraps a value in single quotes for safe CLI interpolation.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// #endregion

// Compile-time assertions that Driver satisfies every advertised interface.
var (
	_ device.Driver            = (*Driver)(nil)
	_ device.SystemDriver      = (*Driver)(nil)
	_ device.InterfaceDriver   = (*Driver)(nil)
	_ device.IPAddressDriver   = (*Driver)(nil)
	_ device.RouteDriver       = (*Driver)(nil)
	_ device.NeighborDriver    = (*Driver)(nil)
	_ device.DHCPDriver        = (*Driver)(nil)
	_ device.WiFiDriver        = (*Driver)(nil)
	_ device.FirewallDriver    = (*Driver)(nil)
	_ device.PortForwardDriver = (*Driver)(nil)
	_ device.LogsDriver        = (*Driver)(nil)
	_ device.PingDriver        = (*Driver)(nil)
	_ device.ConfigDriver      = (*Driver)(nil)
	_ device.RebootDriver      = (*Driver)(nil)
	_ device.CommandDriver     = (*Driver)(nil)
)
