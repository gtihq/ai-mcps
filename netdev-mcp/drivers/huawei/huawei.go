// Package huawei implements a driver for Huawei home gateways, including the
// WiFi BE3 family. It uses the web API session and CSRF token flow shared by
// Huawei routers and lets every endpoint be overridden in the inventory for
// firmware variants that moved their paths.
package huawei

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/webclient"
)

// Default endpoint paths for the Huawei home gateway web API.
const (
	pathToken      = "/api/webserver/SesTokInfo"
	pathLogin      = "/api/user/login"
	pathHome       = "/index.html"
	pathSystem     = "/api/system/routerstatus"
	pathInterfaces = "/api/system/interface"
	pathDHCP       = "/api/dhcp/settings"
	pathWiFi       = "/api/wlan/station/infos"
	pathForwards   = "/api/security/portmapping/entries"
	pathLogs       = "/api/log/loglist"
	pathReboot     = "/api/device/control"
)

// Driver talks to the Huawei web API.
type Driver struct {
	cfg device.Config
	web *webclient.Client
}

// New creates a Huawei driver without opening a connection.
func New(cfg device.Config) (device.Driver, error) {
	web, err := webclient.New(cfg, 80, false)
	if err != nil {
		return nil, err
	}
	return &Driver{cfg: cfg, web: web}, nil
}

// Vendor returns the vendor identifier.
func (d *Driver) Vendor() device.Vendor { return device.VendorHuawei }

// Capabilities lists the tools this driver implements.
func (d *Driver) Capabilities() []string {
	return []string{
		device.CapSystem, device.CapInterfaces, device.CapDHCP,
		device.CapWiFi, device.CapPortForwards, device.CapLogs,
		device.CapReboot, device.CapCommand, device.CapHTTP,
	}
}

// Close releases idle HTTP connections.
func (d *Driver) Close() error {
	d.web.HTTP.CloseIdleConnections()
	return nil
}

// #region Authentication

// ensure performs the token exchange and login once per process.
func (d *Driver) ensure(ctx context.Context) error {
	if d.web.LoggedIn {
		return nil
	}
	if err := d.acquireToken(ctx); err != nil {
		return err
	}
	preferred := device.MapString(d.cfg.Extra["password_type"])
	order := []string{"4", "2", "0"}
	if preferred != "" {
		order = append([]string{preferred}, order...)
	}
	seen := make(map[string]bool)
	for _, passwordType := range order {
		if seen[passwordType] {
			continue
		}
		seen[passwordType] = true
		body := webclient.XMLRequest(map[string]string{
			"Username":      d.cfg.Username,
			"Password":      encodedPassword(d.cfg.Username, d.cfg.Password, d.web.Token, passwordType),
			"password_type": passwordType,
		})
		data, err := d.web.PostXML(ctx, d.cfg.Endpoint("login", pathLogin), body)
		if err != nil {
			continue
		}
		if !loginFailed(data) {
			d.web.LoggedIn = true
			return nil
		}
	}
	return errors.New("huawei: login rejected, check credentials or set extra.password_type and the token endpoint in the inventory")
}

// acquireToken fetches the session and CSRF token. Firmware builds expose it
// on different paths, so several known endpoints are tried in order.
func (d *Driver) acquireToken(ctx context.Context) error {
	candidates := []string{
		d.cfg.Endpoint("token", pathToken),
		"/api/webserver/token",
		d.cfg.Endpoint("login_page", pathHome),
		"/index.html",
		"/html/home.html",
		"/",
	}
	var firstErr error
	seen := make(map[string]bool, len(candidates))
	for _, path := range candidates {
		if seen[path] {
			continue
		}
		seen[path] = true
		data, err := d.web.Get(ctx, path)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		values := webclient.XMLMap(data)
		token := firstNonEmpty(
			device.MapString(values["response.TokInfo"]),
			device.MapString(values["response.Token"]),
			device.MapString(values["response.csrf_token"]),
			webclient.TokenFromBody(data),
		)
		session := firstNonEmpty(
			device.MapString(values["response.SesInfo"]),
			device.MapString(values["response.SessionID"]),
		)
		if token != "" {
			d.web.Token = token
		}
		if session != "" {
			d.web.ExtraHeaders = map[string]string{"Cookie": "SessionID=" + session}
		}
		if token != "" {
			return nil
		}
	}
	if firstErr != nil {
		return fmt.Errorf("huawei: cannot obtain CSRF token: %w", firstErr)
	}
	return errors.New("huawei: no CSRF token found in token endpoint or login page")
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// encodedPassword produces the password encoding expected by the selected
// password_type (4 is the SHA256 plus base64 variant used by most gateways).
func encodedPassword(user, password, token, passwordType string) string {
	switch passwordType {
	case "4":
		inner := sha256.Sum256([]byte(password))
		seed := base64.StdEncoding.EncodeToString(inner[:])
		outer := sha256.Sum256([]byte(user + seed + token))
		return base64.StdEncoding.EncodeToString(outer[:])
	case "2":
		hash := sha256.Sum256([]byte(password))
		return base64.StdEncoding.EncodeToString(hash[:])
	default:
		return password
	}
}

// loginFailed detects the XML error envelope returned by Huawei gateways.
func loginFailed(data []byte) bool {
	text := strings.ToLower(string(data))
	if strings.Contains(text, "<ok>") || strings.Contains(text, "<response>ok</response>") {
		return false
	}
	for _, marker := range []string{"<error>", "125003", "password error", "login failed", `"success":false`} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// #endregion

// #region Normalized capabilities

// fetch returns rows from a configured endpoint, accepting XML or JSON.
func (d *Driver) fetch(ctx context.Context, key, fallback string) ([]map[string]any, error) {
	if err := d.ensure(ctx); err != nil {
		return nil, err
	}
	data, err := d.web.Get(ctx, d.cfg.Endpoint(key, fallback))
	if err != nil {
		return nil, err
	}
	if rows := webclient.XMLRows(data); len(rows) > 0 {
		return rows, nil
	}
	if rows := webclient.ParseRows(data); len(rows) > 0 {
		return rows, nil
	}
	values := webclient.XMLMap(data)
	if len(values) > 0 {
		return []map[string]any{values}, nil
	}
	return nil, nil
}

// SystemInfo reads the router status endpoint.
func (d *Driver) SystemInfo(ctx context.Context) (device.SystemInfo, error) {
	rows, err := d.fetch(ctx, "system", pathSystem)
	if err != nil {
		return device.SystemInfo{}, err
	}
	info := device.SystemInfo{
		Name:      d.cfg.Name,
		Vendor:    device.VendorHuawei,
		Model:     device.StringOr(d.cfg.Model, "WiFi BE3"),
		Host:      d.cfg.Host,
		Site:      d.cfg.Site,
		Reachable: true,
	}
	if len(rows) > 0 {
		r := rows[0]
		info.Hostname = pick(r, "HostName", "DeviceName", "hostname", "response.HostName")
		info.Version = pick(r, "SoftwareVersion", "SoftwareVersionV3", "version", "response.SoftwareVersion")
		info.Uptime = pick(r, "UpTime", "Uptime", "response.UpTime")
		info.Serial = pick(r, "SerialNumber", "SN", "response.SerialNumber")
		info.CPU = pick(r, "CPUUsage", "CpuUsage")
		info.Memory = pick(r, "MemoryUsage", "MemUsage")
		info.Extra = r
	}
	return info, nil
}

// Interfaces reads the interface status endpoint.
func (d *Driver) Interfaces(ctx context.Context) ([]device.Interface, error) {
	rows, err := d.fetch(ctx, "interfaces", pathInterfaces)
	if err != nil {
		return nil, err
	}
	out := make([]device.Interface, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.Interface{
			Name:    pick(r, "Name", "InterfaceName", "ifName", "name"),
			Type:    pick(r, "Type", "InterfaceType"),
			MAC:     pick(r, "MACAddress", "MacAddress"),
			Status:  pick(r, "Status", "LinkStatus", "state"),
			Speed:   pick(r, "Speed", "LinkSpeed", "Rate"),
			RxBytes: device.First(r, "RxBytes", "BytesReceived"),
			TxBytes: device.First(r, "TxBytes", "BytesSent"),
			Extra:   r,
		})
	}
	return out, nil
}

// DHCPLeases reads the DHCP client list.
func (d *Driver) DHCPLeases(ctx context.Context) ([]device.DHCPLease, error) {
	rows, err := d.fetch(ctx, "dhcp", pathDHCP)
	if err != nil {
		return nil, err
	}
	out := make([]device.DHCPLease, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.DHCPLease{
			Hostname: pick(r, "HostName", "HostNameV3", "Name"),
			MAC:      pick(r, "MACAddress", "MacAddress"),
			IP:       pick(r, "IPAddress", "IpAddress", "address"),
			Lease:    pick(r, "LeaseTime", "Lease"),
			Status:   pick(r, "Status", "state"),
			Extra:    r,
		})
	}
	return out, nil
}

// WiFiClients reads the associated stations.
func (d *Driver) WiFiClients(ctx context.Context) ([]device.WiFiClient, error) {
	rows, err := d.fetch(ctx, "wifi", pathWiFi)
	if err != nil {
		return nil, err
	}
	out := make([]device.WiFiClient, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.WiFiClient{
			MAC:       pick(r, "MACAddress", "MacAddress", "MAC"),
			IP:        pick(r, "IPAddress", "IpAddress"),
			SSID:      pick(r, "SSID", "Ssid"),
			Interface: pick(r, "Interface", "Radio", "wlan"),
			Signal:    pick(r, "SignalStrength", "RSSI", "Signal"),
			RxRate:    pick(r, "RxRate"),
			TxRate:    pick(r, "TxRate"),
			Extra:     r,
		})
	}
	return out, nil
}

// PortForwards reads the port mapping table.
func (d *Driver) PortForwards(ctx context.Context) ([]device.PortForward, error) {
	rows, err := d.fetch(ctx, "port_forwards", pathForwards)
	if err != nil {
		return nil, err
	}
	out := make([]device.PortForward, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.PortForward{
			Name:     pick(r, "Name", "PortMappingName", "Description"),
			Protocol: pick(r, "Protocol", "ProtocolType"),
			External: pick(r, "ExternalPort", "WANPort", "ExternalPortStart"),
			Internal: pick(r, "InternalPort", "LocalPort", "InternalPortStart"),
			Target:   pick(r, "InternalAddress", "LocalAddress", "InternalIP", "IPAddress"),
			Enabled:  device.BoolText(pick(r, "Enabled", "Enable", "Status")),
			Extra:    r,
		})
	}
	return out, nil
}

// Logs reads the log endpoint.
func (d *Driver) Logs(ctx context.Context, limit int, topics string) ([]device.LogEntry, error) {
	rows, err := d.fetch(ctx, "logs", pathLogs)
	if err != nil {
		return nil, err
	}
	out := make([]device.LogEntry, 0, len(rows))
	needle := strings.ToLower(topics)
	for _, r := range rows {
		entry := device.LogEntry{
			Time:    pick(r, "Time", "LogTime", "time"),
			Topics:  pick(r, "Type", "Level", "topics"),
			Message: pick(r, "Message", "Content", "Description", "message"),
			Extra:   r,
		}
		if needle != "" && !strings.Contains(strings.ToLower(entry.Topics+entry.Message), needle) {
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

// Reboot posts the standard device control request.
func (d *Driver) Reboot(ctx context.Context) error {
	if err := d.ensure(ctx); err != nil {
		return err
	}
	body := webclient.XMLRequest(map[string]string{"Control": "1"})
	_, err := d.web.PostXML(ctx, d.cfg.Endpoint("reboot", pathReboot), body)
	return err
}

// Command treats the command string as a relative API path.
func (d *Driver) Command(ctx context.Context, command string, params map[string]any) (any, error) {
	return d.HTTP(ctx, webclient.Method(params), command, nil, nil)
}

// HTTP issues a raw request against the Huawei web API.
func (d *Driver) HTTP(ctx context.Context, method, path string, body []byte, headers map[string]string) (device.HTTPResponse, error) {
	if err := d.ensure(ctx); err != nil {
		return device.HTTPResponse{}, err
	}
	contentType := ""
	if body != nil {
		contentType = "text/xml"
		if strings.HasPrefix(strings.TrimSpace(string(body)), "{") {
			contentType = "application/json"
		}
	}
	d.web.ExtraHeaders = mergeHeaders(map[string]string{"Cookie": cookieHeader(d.web.ExtraHeaders)}, headers)
	defer func() {
		d.web.ExtraHeaders = map[string]string{"Cookie": cookieHeader(d.web.ExtraHeaders)}
	}()
	data, respHeaders, err := d.web.Request(ctx, method, path, contentType, body)
	if err != nil {
		return device.HTTPResponse{}, err
	}
	return device.HTTPResponse{Status: d.web.Status, Body: string(data), Headers: webclient.HeaderMap(respHeaders)}, nil
}

// #endregion

// pick returns the first non-empty value among the supplied keys.
func pick(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := row[key]; ok {
			if s := device.MapString(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// mergeHeaders combines the session cookie with caller supplied headers.
func mergeHeaders(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// cookieHeader extracts the current session cookie value.
func cookieHeader(headers map[string]string) string {
	if headers == nil {
		return ""
	}
	return headers["Cookie"]
}

// Compile-time assertions.
var (
	_ device.Driver            = (*Driver)(nil)
	_ device.SystemDriver      = (*Driver)(nil)
	_ device.InterfaceDriver   = (*Driver)(nil)
	_ device.DHCPDriver        = (*Driver)(nil)
	_ device.WiFiDriver        = (*Driver)(nil)
	_ device.PortForwardDriver = (*Driver)(nil)
	_ device.LogsDriver        = (*Driver)(nil)
	_ device.RebootDriver      = (*Driver)(nil)
	_ device.CommandDriver     = (*Driver)(nil)
	_ device.HTTPDriver        = (*Driver)(nil)
)
