// Package sshcli implements a vendor-neutral SSH CLI driver for switches and
// routers that expose a Cisco-like or Huawei VRP-like command shell.
package sshcli

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gtihq/ai-mcps/mcps/netdev-mcp/device"
	"github.com/gtihq/ai-mcps/mcps/netdev-mcp/drivers/sshutil"
)

const defaultTimeout = 15 * time.Second

// Profile is a set of CLI commands for one vendor dialect.
type Profile struct {
	System     string
	Interfaces string
	Routes     string
	Neighbors  string
	Config     string
	Ping       string
}

// profiles ships the dialects used by the target fleet. Inventories can add
// more through extra.profile_commands without changing the server.
var profiles = map[string]Profile{
	"cisco": {
		System:     "show version",
		Interfaces: "show interfaces status",
		Routes:     "show ip route",
		Neighbors:  "show lldp neighbors detail",
		Config:     "show running-config",
		Ping:       "ping <target> repeat <count>",
	},
	"huawei_vrp": {
		System:     "display version",
		Interfaces: "display interface brief",
		Routes:     "display ip routing-table",
		Neighbors:  "display lldp neighbor detail",
		Config:     "display current-configuration",
		Ping:       "ping -c <count> <target>",
	},
}

var (
	versionRE = regexp.MustCompile(`(?i)(?:software|version)\s+(?:version\s+)?([0-9][0-9A-Za-z.()\-]*)`)
	uptimeRE  = regexp.MustCompile(`(?i)uptime\s+(?:is\s+)?([^\r\n]+)`)
	modelRE   = regexp.MustCompile(`(?i)(?:model|product)\s+(?:number\s+)?([A-Za-z0-9][A-Za-z0-9\-]*)`)
	serialRE  = regexp.MustCompile(`(?i)(?:serial|sn)\s+(?:number\s+)?([A-Za-z0-9]+)`)
)

// Driver implements the CLI capabilities.
type Driver struct {
	cfg     device.Config
	timeout time.Duration
	profile Profile
}

// New creates a CLI driver. Unknown profiles fall back to the Cisco dialect.
func New(cfg device.Config) (device.Driver, error) {
	profileName := device.StringOr(cfg.Profile, "cisco")
	profile, ok := profiles[profileName]
	if !ok {
		profile = profiles["cisco"]
	}
	if raw, ok := cfg.Extra["profile_commands"]; ok {
		applyOverrides(&profile, raw)
	}
	return &Driver{cfg: cfg, timeout: cfg.Duration(defaultTimeout), profile: profile}, nil
}

// Vendor returns the vendor identifier.
func (d *Driver) Vendor() device.Vendor { return device.VendorSSH }

// Capabilities lists the tools this driver implements.
func (d *Driver) Capabilities() []string {
	return []string{
		device.CapSystem, device.CapInterfaces, device.CapRoutes,
		device.CapNeighbors, device.CapConfig, device.CapPing,
		device.CapCommand,
	}
}

// Close is a no-op because each SSH session is short lived.
func (d *Driver) Close() error { return nil }

// run executes one CLI command.
func (d *Driver) run(ctx context.Context, command string) (string, error) {
	return sshutil.Exec(ctx, d.cfg, d.timeout, command)
}

// #region Normalized capabilities

// SystemInfo parses the version banner for identity and uptime.
func (d *Driver) SystemInfo(ctx context.Context) (device.SystemInfo, error) {
	out, err := d.run(ctx, d.profile.System)
	if err != nil {
		return device.SystemInfo{}, err
	}
	info := device.SystemInfo{
		Name:      d.cfg.Name,
		Vendor:    device.VendorSSH,
		Model:     d.cfg.Model,
		Host:      d.cfg.Host,
		Site:      d.cfg.Site,
		Reachable: true,
		Extra:     map[string]any{"raw": out},
	}
	if m := versionRE.FindStringSubmatch(out); len(m) > 1 {
		info.Version = m[1]
	}
	if m := uptimeRE.FindStringSubmatch(out); len(m) > 1 {
		info.Uptime = strings.TrimSpace(m[1])
	}
	if m := modelRE.FindStringSubmatch(out); len(m) > 1 {
		info.Model = device.StringOr(info.Model, m[1])
	}
	if m := serialRE.FindStringSubmatch(out); len(m) > 1 {
		info.Serial = m[1]
	}
	return info, nil
}

// Interfaces parses a status table.
func (d *Driver) Interfaces(ctx context.Context) ([]device.Interface, error) {
	out, err := d.run(ctx, d.profile.Interfaces)
	if err != nil {
		return nil, err
	}
	rows := parseTable(out)
	result := make([]device.Interface, 0, len(rows))
	for _, row := range rows {
		result = append(result, device.Interface{
			Name:    pickCI(row, "port", "interface", "name", "iface"),
			Type:    pickCI(row, "type", "media"),
			MAC:     pickCI(row, "mac", "macaddress"),
			Status:  pickCI(row, "status", "state", "physical", "link"),
			Speed:   pickCI(row, "speed", "duplex"),
			Comment: pickCI(row, "vlan", "description"),
			Extra:   row,
		})
	}
	return result, nil
}

// Routes parses a routing table.
func (d *Driver) Routes(ctx context.Context) ([]device.Route, error) {
	out, err := d.run(ctx, d.profile.Routes)
	if err != nil {
		return nil, err
	}
	rows := parseTable(out)
	result := make([]device.Route, 0, len(rows))
	for _, row := range rows {
		result = append(result, device.Route{
			Destination: pickCI(row, "destination", "network", "prefix", "destination/mask"),
			Gateway:     pickCI(row, "gateway", "nexthop", "via"),
			Interface:   pickCI(row, "interface", "outgoing", "iface"),
			Distance:    device.First(row, "distance", "metric", "preference"),
			Extra:       row,
		})
	}
	return result, nil
}

// Neighbors parses LLDP or CDP output into a flat list.
func (d *Driver) Neighbors(ctx context.Context) ([]device.Neighbor, error) {
	out, err := d.run(ctx, d.profile.Neighbors)
	if err != nil {
		return nil, err
	}
	rows := parseTable(out)
	result := make([]device.Neighbor, 0, len(rows))
	for _, row := range rows {
		result = append(result, device.Neighbor{
			IP:           pickCI(row, "ip", "ipaddress", "managementaddress"),
			MAC:          pickCI(row, "mac", "macaddress"),
			Hostname:     pickCI(row, "deviceid", "hostname", "systemname", "neighbor"),
			Platform:     pickCI(row, "platform", "capability", "systemdescription"),
			Interface:    pickCI(row, "localport", "interface", "portid", "port"),
			Capabilities: pickCI(row, "capabilities", "capability"),
			Extra:        row,
		})
	}
	return result, nil
}

// ExportConfig returns the running configuration verbatim.
func (d *Driver) ExportConfig(ctx context.Context) (string, error) {
	return d.run(ctx, d.profile.Config)
}

// Ping runs the profile specific ping command and parses the summary.
func (d *Driver) Ping(ctx context.Context, target string, count int) (device.PingResult, error) {
	if count <= 0 {
		count = 4
	}
	command := strings.ReplaceAll(d.profile.Ping, "<target>", target)
	command = strings.ReplaceAll(command, "<count>", fmt.Sprint(count))
	out, err := d.run(ctx, command)
	if err != nil {
		return device.PingResult{}, err
	}
	result := device.PingResult{Target: target, Extra: map[string]any{"raw": out}}
	if m := regexp.MustCompile(`(?i)([0-9]+)\s+packets transmitted`).FindStringSubmatch(out); len(m) > 1 {
		result.Sent = m[1]
	}
	if m := regexp.MustCompile(`(?i)([0-9]+)\s+(?:packets received|received)`).FindStringSubmatch(out); len(m) > 1 {
		result.Received = m[1]
	}
	if m := regexp.MustCompile(`(?i)([0-9.]+)%\s*packet loss`).FindStringSubmatch(out); len(m) > 1 {
		result.Loss = m[1] + "%"
	}
	if m := regexp.MustCompile(`(?i)(?:avg|average|round-trip)[^\d]{0,20}([0-9.]+)\s*ms`).FindStringSubmatch(out); len(m) > 1 {
		result.AvgRTT = m[1] + "ms"
	}
	return result, nil
}

// #endregion

// Command executes an arbitrary CLI command.
func (d *Driver) Command(ctx context.Context, command string, params map[string]any) (any, error) {
	return d.run(ctx, command)
}

// applyOverrides merges extra.profile_commands into a profile.
func applyOverrides(profile *Profile, raw any) {
	commands := device.MapAny(raw)
	if commands == nil {
		return
	}
	values := map[string]*string{
		"system":     &profile.System,
		"interfaces": &profile.Interfaces,
		"routes":     &profile.Routes,
		"neighbors":  &profile.Neighbors,
		"config":     &profile.Config,
		"ping":       &profile.Ping,
	}
	for key, target := range values {
		if value := device.MapString(commands[key]); value != "" {
			*target = value
		}
	}
}

// parseTable turns whitespace aligned CLI output into maps. The first line
// with three or more tokens is treated as the header.
func parseTable(output string) []map[string]any {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	header := []string(nil)
	rows := make([]map[string]any, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "!") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if header == nil {
			if len(fields) >= 3 && !containsDigits(trimmed) {
				header = lowerFields(fields)
			}
			continue
		}
		if len(fields) < 2 {
			continue
		}
		row := make(map[string]any, len(header))
		for i, key := range header {
			if i < len(fields) {
				row[key] = fields[i]
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// containsDigits reports whether a header candidate has digits, which helps
// skip banner lines such as "Cisco IOS Software, Version 15.2".
func containsDigits(value string) bool {
	for _, r := range value {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// lowerFields lowercases and deduplicates header names.
func lowerFields(fields []string) []string {
	out := make([]string, len(fields))
	seen := make(map[string]int, len(fields))
	for i, field := range fields {
		key := strings.ToLower(field)
		seen[key]++
		if seen[key] > 1 {
			key = fmt.Sprintf("%s_%d", key, seen[key])
		}
		out[i] = key
	}
	return out
}

// pickCI finds a key case insensitively.
func pickCI(row map[string]any, keys ...string) string {
	for _, key := range keys {
		for actual, value := range row {
			if strings.EqualFold(actual, key) {
				if s := device.MapString(value); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// Compile-time assertions.
var (
	_ device.Driver          = (*Driver)(nil)
	_ device.SystemDriver    = (*Driver)(nil)
	_ device.InterfaceDriver = (*Driver)(nil)
	_ device.RouteDriver     = (*Driver)(nil)
	_ device.NeighborDriver  = (*Driver)(nil)
	_ device.ConfigDriver    = (*Driver)(nil)
	_ device.PingDriver      = (*Driver)(nil)
	_ device.CommandDriver   = (*Driver)(nil)
)
