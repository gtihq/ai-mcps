// Package zte implements a driver for ZTE ZXHN home gateways (including the
// H199A). Those devices expose a Lua based web UI rather than a public API,
// so the driver logs into that UI and reads the configured pages. Endpoint
// paths are overridable because they change between ISP firmware builds.
package zte

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gtihq/ai-mcps/mcps/netdev-mcp/device"
	"github.com/gtihq/ai-mcps/mcps/netdev-mcp/drivers/webclient"
)

// Default endpoint paths for the common ZTE ZXHN firmware family.
const (
	pathLoginPage  = "/login.html"
	pathLogin      = "/login.lua"
	pathSystem     = "/getpage.lua?pid=1000"
	pathInterfaces = "/getpage.lua?pid=1001"
	pathDHCP       = "/getpage.lua?pid=1002"
	pathWiFi       = "/getpage.lua?pid=1003"
	pathForwards   = "/getpage.lua?pid=1004"
	pathLogs       = "/getpage.lua?pid=1005"

	// Endpoints used by the ZXHN H199A V9.1 web UI. The browser keeps the same
	// URL and loads data through these menuData/menuView requests.
	pathTokenLua    = "/?_type=loginData&_tag=login_token"
	pathLoginLua    = "/?_type=loginData&_tag=login_entry"
	pathLandingLua  = "/?_type=menuView&_tag=lanMgrIpv4"
	pathHostInfoLua = "/?_type=menuData&_tag=dhcp4s_dhcphostinfo_m.lua"
	pathBindingLua  = "/?_type=menuData&_tag=dhcp4s_dhcpbinding_m.lua"
)

// Driver talks to the ZTE web UI.
type Driver struct {
	cfg          device.Config
	web          *webclient.Client
	tlsTried     bool
	sessionToken string
}

// New creates a ZTE driver without opening a connection.
func New(cfg device.Config) (device.Driver, error) {
	web, err := webclient.New(cfg, 80, false)
	if err != nil {
		return nil, err
	}
	web.ExtraHeaders = map[string]string{
		"User-Agent":       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"X-Requested-With": "XMLHttpRequest",
	}
	return &Driver{cfg: cfg, web: web}, nil
}

// Vendor returns the vendor identifier.
func (d *Driver) Vendor() device.Vendor { return device.VendorZTE }

// Capabilities lists the tools this driver implements.
func (d *Driver) Capabilities() []string {
	caps := []string{
		device.CapSystem, device.CapInterfaces, device.CapDHCP,
		device.CapDhcpReservation, device.CapRenameReservation,
		device.CapWiFi, device.CapPortForwards, device.CapLogs,
		device.CapCommand, device.CapHTTP,
	}
	if d.cfg.Endpoints["reboot"] != "" {
		caps = append(caps, device.CapReboot)
	}
	return caps
}

// Close is a no-op because the HTTP client keeps no persistent connection.
func (d *Driver) Close() error {
	d.web.HTTP.CloseIdleConnections()
	return nil
}

// loginCandidate is one endpoint, format and pair of field names to try.
type loginCandidate struct {
	path   string
	format string
	user   string
	pass   string
}

// LoginError records every attempt so the UI can show what the device answered.
type LoginError struct {
	Attempts []string
}

// Error implements the error interface.
func (e *LoginError) Error() string {
	return "zte: login failed on all known endpoints: " + strings.Join(e.Attempts, "; ")
}

// ensure performs the web UI login once per process. ZTE firmware changes
// both the endpoint and the field names, so several combinations are tried
// and the session is confirmed with a follow up request.
func (d *Driver) ensure(ctx context.Context) error {
	if d.web.LoggedIn {
		return nil
	}
	err := d.tryLogin(ctx)
	if err == nil {
		return nil
	}
	if d.cfg.TLS || d.tlsTried {
		return err
	}
	d.tlsTried = true
	tlsCfg := d.cfg
	tlsCfg.TLS = true
	web, buildErr := webclient.New(tlsCfg, 443, true)
	if buildErr != nil {
		return err
	}
	d.web = web
	return d.tryLogin(ctx)
}

// tryLogin walks every known login combination over the current client.
func (d *Driver) tryLogin(ctx context.Context) error {
	if d.tryMenuLogin(ctx) {
		return nil
	}
	loginPages := []string{
		d.cfg.Endpoint("login_page", pathLoginPage),
		"/",
		"/login.lua",
		"/cgi-bin/login.html",
		"/html/login.html",
	}
	seenPage := make(map[string]bool, len(loginPages))
	for _, page := range loginPages {
		if seenPage[page] {
			continue
		}
		seenPage[page] = true
		if _, err := d.web.Get(ctx, page); err == nil {
			break
		}
	}

	configuredPath := d.cfg.Endpoint("login", pathLogin)
	userField := device.StringOr(device.MapString(d.cfg.Extra["username_field"]), "username")
	passField := device.StringOr(device.MapString(d.cfg.Extra["password_field"]), "password")
	candidates := []loginCandidate{
		{configuredPath, "form", userField, passField},
		{configuredPath, "form", "user", "pass"},
		{configuredPath, "form", "Username", "Password"},
		{"/login.lua", "form", userField, passField},
		{"/cgi-bin/login.cgi", "form", userField, passField},
		{"/cgi-bin/login.cgi", "form", "user", "pass"},
		{"/login.cgi", "form", userField, passField},
		{"/goform/login", "form", userField, passField},
		{"/api/user/login", "json", "username", "password"},
	}
	seen := make(map[string]bool)
	attempts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		key := candidate.path + "|" + candidate.user
		if seen[key] {
			continue
		}
		seen[key] = true
		var data []byte
		var err error
		if candidate.format == "json" {
			data, err = d.web.PostJSON(ctx, candidate.path, map[string]any{
				candidate.user: d.cfg.Username,
				candidate.pass: d.cfg.Password,
			})
		} else {
			values := url.Values{}
			values.Set(candidate.user, d.cfg.Username)
			values.Set(candidate.pass, d.cfg.Password)
			values.Set("csrf_token", d.web.Token)
			values.Set("loginsubmit", "Login")
			values.Set("login", "Login")
			data, err = d.web.PostForm(ctx, candidate.path, values)
		}
		if err != nil {
			attempts = append(attempts, candidate.path+": "+briefError(err))
			continue
		}
		if loginFailed(string(data)) {
			attempts = append(attempts, candidate.path+": resposta de erro")
			continue
		}
		if !d.sessionConfirmed(ctx) {
			attempts = append(attempts, candidate.path+": session not confirmed")
			continue
		}
		d.web.LoggedIn = true
		return nil
	}
	return &LoginError{Attempts: attempts}
}

// tryMenuLogin handles the ZXHN H199A V9.1 flow: read the one-time token,
// post the form credentials, then confirm with a menuData request.
func (d *Driver) tryMenuLogin(ctx context.Context) bool {
	if _, err := d.web.Get(ctx, "/"); err != nil {
		return false
	}
	// The page first reads a session token from login_entry, then hashes the
	// password with the token returned by login_token.
	sessionData, err := d.web.Get(ctx, pathLoginLua)
	if err != nil {
		return false
	}
	sessionToken := ""
	if match := regexp.MustCompile(`"sess_token":"([^"]+)"`).FindSubmatch(sessionData); len(match) > 1 {
		sessionToken = string(match[1])
	}
	tokenData, err := d.web.Get(ctx, pathTokenLua)
	if err != nil {
		return false
	}
	token := ""
	if match := regexp.MustCompile(`>([0-9]+)<`).FindSubmatch(tokenData); len(match) > 1 {
		token = string(match[1])
	}
	digest := sha256.Sum256([]byte(d.cfg.Password + token))
	values := url.Values{}
	values.Set("Password", hex.EncodeToString(digest[:]))
	values.Set("Username", d.cfg.Username)
	values.Set("action", "login")
	values.Set("_sessionTOKEN", sessionToken)
	data, err := d.web.PostForm(ctx, pathLoginLua, values)
	if err != nil {
		return false
	}
	text := string(data)
	if !strings.Contains(text, `"sess_token"`) || strings.Contains(text, `"lockingTime":-1`) {
		return false
	}
	if strings.Contains(text, `"loginErrMsg":"`) && !strings.Contains(text, `"loginErrMsg":""`) {
		return false
	}
	if match := regexp.MustCompile(`"sess_token":"([^"]+)"`).FindStringSubmatch(text); len(match) > 1 {
		d.sessionToken = match[1]
	}
	// Loading the landing page after login rotates the SID cookie and makes
	// the menuData endpoints available. Without this step every read returns
	// SessionTimeout.
	if _, err := d.web.Get(ctx, d.cfg.Endpoint("landing", pathLandingLua)); err != nil {
		return false
	}
	if !d.sessionConfirmed(ctx) {
		return false
	}
	d.web.LoggedIn = true
	return true
}

// sessionConfirmed checks that a follow up page is no longer the login form.
func (d *Driver) sessionConfirmed(ctx context.Context) bool {
	path := d.cfg.Endpoint("session_check", pathHostInfoLua)
	data, err := d.web.Get(ctx, path)
	if err == nil && strings.Contains(strings.ToLower(string(data)), "sessiontimeout") {
		return false
	}
	if err != nil {
		pathData, fallbackErr := d.web.Get(ctx, "/")
		if fallbackErr != nil {
			return false
		}
		data = pathData
	}
	lower := strings.ToLower(string(data))
	for _, marker := range []string{"name=\"password\"", "name='password'", "frm_password", "/login.lua", "<title>login"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

// briefError reduces a transport error to a short phrase for the result page.
func briefError(err error) string {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "http 404"):
		return "HTTP 404"
	case strings.Contains(text, "http 403"):
		return "HTTP 403"
	case strings.Contains(text, "http 401"):
		return "HTTP 401"
	case strings.Contains(text, "connectex"), strings.Contains(text, "connection refused"), strings.Contains(text, "recusou"):
		return "no connection"
	case strings.Contains(text, "timeout"), strings.Contains(text, "tempo limite"):
		return "no response"
	default:
		return "failed"
	}
}

// loginFailed looks for common rejection messages in HTML or JSON replies.
func loginFailed(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range []string{
		`"result":false`, `"success":false`, `"code":1`, "wrong password",
		"invalid password", "invalid user", "用户名或密码", "密码错误",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// fetch returns parsed rows from a configured endpoint.
func (d *Driver) fetch(ctx context.Context, key, fallback string) ([]map[string]any, error) {
	if err := d.ensure(ctx); err != nil {
		return nil, err
	}
	data, err := d.web.Get(ctx, d.cfg.Endpoint(key, fallback))
	if err != nil {
		return nil, err
	}
	return webclient.ParseRows(data), nil
}

// #region Normalized capabilities

// SystemInfo reads the ZTE device information page.
func (d *Driver) SystemInfo(ctx context.Context) (device.SystemInfo, error) {
	rows, err := d.fetch(ctx, "system", pathSystem)
	if err != nil {
		return device.SystemInfo{}, err
	}
	info := device.SystemInfo{
		Name:      d.cfg.Name,
		Vendor:    device.VendorZTE,
		Model:     device.StringOr(d.cfg.Model, "ZXHN H199A"),
		Host:      d.cfg.Host,
		Site:      d.cfg.Site,
		Reachable: true,
	}
	if len(rows) > 0 {
		r := rows[0]
		info.Hostname = pick(r, "HostName", "DeviceName", "hostname", "Device Name")
		info.Version = pick(r, "SoftwareVersion", "FirmwareVersion", "Software Version", "version")
		info.Uptime = pick(r, "UpTime", "Uptime", "Uptime(second)", "uptime")
		info.Serial = pick(r, "SerialNumber", "SN", "serial")
		info.Model = device.StringOr(info.Model, pick(r, "Model", "DeviceModel"))
		info.Extra = device.Extra(r, "HostName", "SoftwareVersion", "UpTime", "SerialNumber")
	}
	return info, nil
}

// Interfaces reads the LAN and WAN status table.
func (d *Driver) Interfaces(ctx context.Context) ([]device.Interface, error) {
	rows, err := d.fetch(ctx, "interfaces", pathInterfaces)
	if err != nil {
		return nil, err
	}
	out := make([]device.Interface, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.Interface{
			Name:    pick(r, "Name", "Interface", "Port", "interfaceName"),
			Type:    pick(r, "Type", "PortType"),
			MAC:     pick(r, "MAC", "MACAddress", "MacAddress"),
			Status:  pick(r, "Status", "LinkStatus", "state"),
			Speed:   pick(r, "Speed", "Rate", "LinkSpeed"),
			RxBytes: first(r, "RxBytes", "BytesReceived"),
			TxBytes: first(r, "TxBytes", "BytesSent"),
			Extra:   r,
		})
	}
	return out, nil
}

// DHCPLeases reads the LAN client list.
func (d *Driver) DHCPLeases(ctx context.Context) ([]device.DHCPLease, error) {
	if err := d.ensure(ctx); err != nil {
		return nil, err
	}
	data, err := d.web.Get(ctx, d.cfg.Endpoint("dhcp_binding", pathBindingLua))
	if err != nil {
		return nil, err
	}
	rows := parseMenuInstances(data)
	if len(rows) == 0 {
		rows, err = d.fetch(ctx, "dhcp", pathDHCP)
		if err != nil {
			return nil, err
		}
	}
	out := make([]device.DHCPLease, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.DHCPLease{
			Hostname: pick(r, "Name", "HostName", "Host", "hostname"),
			MAC:      pick(r, "MACAddr", "MACAddress", "MAC", "MacAddress"),
			IP:       pick(r, "IPAddr", "IPAddress", "IP", "IpAddress", "address"),
			Lease:    pick(r, "LeaseTime", "Lease", "Expires"),
			Status:   device.StringOr(pick(r, "Status", "state"), "reserved"),
			Extra:    r,
		})
	}
	return out, nil
}

// parseMenuInstances reads the ParaName/ParaValue pairs returned by the ZTE
// menuData endpoints. Each <Instance> becomes one map.
func parseMenuInstances(data []byte) []map[string]any {
	text := strings.ReplaceAll(string(data), "&#32;", " ")
	reInstance := regexp.MustCompile(`(?s)<Instance>(.*?)</Instance>`)
	rePair := regexp.MustCompile(`(?s)<ParaName>(.*?)</ParaName>\s*<ParaValue>(.*?)</ParaValue>`)
	rows := make([]map[string]any, 0)
	for _, instance := range reInstance.FindAllStringSubmatch(text, -1) {
		row := make(map[string]any)
		for _, pair := range rePair.FindAllStringSubmatch(instance[1], -1) {
			key := pair[1]
			if i := strings.LastIndex(key, "."); i >= 0 {
				key = key[i+1:]
			}
			row[key] = strings.TrimSpace(pair[2])
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	return rows
}

// postBindingRaw sends one reservation row using the exact payload shape the
// H199A web UI produces. The fields have no row suffix, a new row uses
// _OBJ_InstID=-1, and the Check header is sha256 of the full body.
func (d *Driver) postBindingRaw(ctx context.Context, instanceID, name, mac, ip string) error {
	sessionData, err := d.web.Get(ctx, pathLoginLua)
	if err != nil {
		return err
	}
	token := ""
	if match := regexp.MustCompile(`"sess_token":"([^"]+)"`).FindSubmatch(sessionData); len(match) > 1 {
		token = string(match[1])
	}
	payload := "IF_ACTION=Apply" +
		"&OBJ_DHCPBIND_ID._OBJ_InstID=" + instanceID +
		"&OBJ_DHCPBIND_ID.Name=" + url.QueryEscape(name) +
		"&OBJ_DHCPBIND_ID.MACAddr=" + url.QueryEscape(mac) +
		"&OBJ_DHCPBIND_ID.IPAddr=" + url.QueryEscape(ip) +
		"&=" +
		"&_sessionTOKEN=" + token
	sum := sha256.Sum256([]byte(payload))
	d.web.ExtraHeaders = map[string]string{
		"Check":            hex.EncodeToString(sum[:]),
		"X-Requested-With": "XMLHttpRequest",
	}
	defer func() {
		d.web.ExtraHeaders = nil
	}()
	_, _, err = d.web.Request(ctx, http.MethodPost, d.cfg.Endpoint("dhcp_binding", pathBindingLua),
		"application/x-www-form-urlencoded; charset=UTF-8", []byte(payload))
	return err
}

// AddReservation creates a static DHCP binding.
func (d *Driver) AddReservation(ctx context.Context, name, mac, ip string) error {
	if err := d.ensure(ctx); err != nil {
		return err
	}
	if mac == "" || ip == "" {
		return fmt.Errorf("mac and ip are required")
	}
	if err := d.postBindingRaw(ctx, "-1", name, mac, ip); err != nil {
		return err
	}
	check, err := d.web.Get(ctx, d.cfg.Endpoint("dhcp_binding", pathBindingLua))
	if err != nil {
		return err
	}
	for _, row := range parseMenuInstances(check) {
		if strings.EqualFold(pick(row, "IPAddr"), ip) && strings.EqualFold(pick(row, "MACAddr"), mac) {
			return nil
		}
	}
	return fmt.Errorf("reservation %s -> %s was not saved", mac, ip)
}

// RenameReservation changes the name of an existing static binding.
func (d *Driver) RenameReservation(ctx context.Context, oldName, newName string) error {
	if err := d.ensure(ctx); err != nil {
		return err
	}
	data, err := d.web.Get(ctx, d.cfg.Endpoint("dhcp_binding", pathBindingLua))
	if err != nil {
		return err
	}
	for _, row := range parseMenuInstances(data) {
		if !strings.EqualFold(pick(row, "Name"), oldName) {
			continue
		}
		return d.postBindingRaw(ctx, pick(row, "_OBJ_InstID"), newName, pick(row, "MACAddr"), pick(row, "IPAddr"))
	}
	return fmt.Errorf("reservation %q not found", oldName)
}

// WiFiClients reads the associated wireless stations.
func (d *Driver) WiFiClients(ctx context.Context) ([]device.WiFiClient, error) {
	rows, err := d.fetch(ctx, "wifi", pathWiFi)
	if err != nil {
		return nil, err
	}
	out := make([]device.WiFiClient, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.WiFiClient{
			MAC:       pick(r, "MAC", "MACAddress", "MacAddress"),
			IP:        pick(r, "IP", "IPAddress", "IpAddress"),
			SSID:      pick(r, "SSID", "Ssid"),
			Interface: pick(r, "Interface", "Radio"),
			Signal:    pick(r, "RSSI", "Signal", "SignalStrength"),
			RxRate:    pick(r, "RxRate"),
			TxRate:    pick(r, "TxRate"),
			Extra:     r,
		})
	}
	return out, nil
}

// PortForwards reads the NAT rules table.
func (d *Driver) PortForwards(ctx context.Context) ([]device.PortForward, error) {
	rows, err := d.fetch(ctx, "port_forwards", pathForwards)
	if err != nil {
		return nil, err
	}
	out := make([]device.PortForward, 0, len(rows))
	for _, r := range rows {
		out = append(out, device.PortForward{
			Name:     pick(r, "Name", "RuleName", "Description"),
			Protocol: pick(r, "Protocol", "ProtocolType"),
			External: pick(r, "ExternalPort", "PublicPort", "WANPort", "Port"),
			Internal: pick(r, "InternalPort", "PrivatePort", "LocalPort"),
			Target:   pick(r, "InternalIP", "InternalAddress", "LocalIP", "IPAddress"),
			Enabled:  device.BoolText(pick(r, "Enabled", "Enable", "Status")),
			Extra:    r,
		})
	}
	return out, nil
}

// Logs reads the system log page and applies a simple topic filter.
func (d *Driver) Logs(ctx context.Context, limit int, topics string) ([]device.LogEntry, error) {
	rows, err := d.fetch(ctx, "logs", pathLogs)
	if err != nil {
		return nil, err
	}
	out := make([]device.LogEntry, 0, len(rows))
	needle := strings.ToLower(topics)
	for _, r := range rows {
		entry := device.LogEntry{
			Time:    pick(r, "Time", "Date", "time"),
			Topics:  pick(r, "Type", "Level", "Module", "topics"),
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

// Reboot triggers the configured reboot endpoint. The inventory must opt in
// explicitly because the path differs across firmware builds.
func (d *Driver) Reboot(ctx context.Context) error {
	path := d.cfg.Endpoint("reboot", "")
	if path == "" {
		return fmt.Errorf("%w: set endpoints.reboot for %s (for example /reboot.lua)", device.ErrUnsupported, d.cfg.Name)
	}
	if err := d.ensure(ctx); err != nil {
		return err
	}
	_, err := d.web.PostForm(ctx, path, url.Values{"confirm": {"1"}})
	return err
}

// Command treats the supplied string as a relative web UI path, or as a JSON
// object with method, path and body when a write is needed.
func (d *Driver) Command(ctx context.Context, command string, params map[string]any) (any, error) {
	return d.HTTP(ctx, webclient.Method(params), command, nil, nil)
}

// HTTP issues a raw request against the ZTE web UI.
func (d *Driver) HTTP(ctx context.Context, method, path string, body []byte, headers map[string]string) (device.HTTPResponse, error) {
	if err := d.ensure(ctx); err != nil {
		return device.HTTPResponse{}, err
	}
	contentType := ""
	if body != nil {
		contentType = "application/x-www-form-urlencoded"
		if strings.HasPrefix(strings.TrimSpace(string(body)), "{") {
			contentType = "application/json"
		}
	}
	d.web.ExtraHeaders = headers
	defer func() {
		d.web.ExtraHeaders = nil
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

// first returns the first non-nil value among the supplied keys.
func first(row map[string]any, keys ...string) any {
	return device.First(row, keys...)
}

// Compile-time assertions.
var (
	_ device.Driver            = (*Driver)(nil)
	_ device.SystemDriver      = (*Driver)(nil)
	_ device.InterfaceDriver   = (*Driver)(nil)
	_ device.DHCPDriver        = (*Driver)(nil)
	_ device.ReservationDriver = (*Driver)(nil)
	_ device.WiFiDriver        = (*Driver)(nil)
	_ device.PortForwardDriver = (*Driver)(nil)
	_ device.LogsDriver        = (*Driver)(nil)
	_ device.RebootDriver      = (*Driver)(nil)
	_ device.CommandDriver     = (*Driver)(nil)
	_ device.HTTPDriver        = (*Driver)(nil)
)
