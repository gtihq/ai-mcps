package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gtihq/ai-mcps/internal/mcpserver"
	"github.com/gtihq/ai-mcps/internal/omap"
	"github.com/gtihq/ai-mcps/mcps/netdev-mcp/device"
)

// tools builds the unified MCP tool surface. Every device-scoped tool uses
// the same capability gate so unsupported operations produce a clear message
// instead of a driver panic.
func tools(reg *Registry) []*mcpserver.Tool {
	deviceProp := mcpserver.NamedProperty{
		Name: "device",
		Prop: mcpserver.Property{Type: "string", Description: "Device name from the inventory (see netdev_list_devices)."},
	}
	limitProp := mcpserver.NamedProperty{
		Name: "limit",
		Prop: mcpserver.Property{Type: "integer", Description: "Maximum number of entries to return. 0 means all.", Default: 100},
	}
	topicsProp := mcpserver.NamedProperty{
		Name: "topics",
		Prop: mcpserver.Property{Type: "string", Description: "Optional case-insensitive topic or message filter."},
	}

	return []*mcpserver.Tool{
		{
			Name: "netdev_list_devices",
			Description: "List every configured network device with its vendor, model, site and supported capabilities.\n" +
				"Call this first to learn the device names accepted by the other tools.\n" +
				"If the inventory is empty, set NETDEV_INVENTORY or NETDEV_DEVICES and restart the server.",
			Properties: []mcpserver.NamedProperty{},
			Handler: func(_ context.Context, _ map[string]any) (any, error) {
				reports := reg.Reports()
				return omap.From(
					"count", len(reports),
					"inventory_path", inventoryPath(),
					"devices", device.List(reports),
				), nil
			},
		},
		{
			Name: "netdev_capabilities",
			Description: "Show which operations a device supports, or all devices when name is omitted.\n" +
				"Capabilities include system_info, interfaces, ip_addresses, routes, neighbors, dhcp_leases, wifi_clients, firewall, port_forwards, logs, ping, config, reboot, command and http.",
			Properties: []mcpserver.NamedProperty{deviceProp},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				name := argString(args, "device")
				if name == "" {
					return omap.From("devices", device.List(reg.Reports())), nil
				}
				report, err := singleReport(reg, name)
				if err != nil {
					return errorMap(err), nil
				}
				return report.Map(), nil
			},
		},
		{
			Name: "netdev_health",
			Description: "Check all devices concurrently and return reachability, version, uptime and resource usage.\n" +
				"Use this for a quick fleet overview before drilling into one device.",
			Properties: []mcpserver.NamedProperty{},
			Handler: func(ctx context.Context, _ map[string]any) (any, error) {
				infos := reg.checkSystem(ctx)
				online := 0
				for _, info := range infos {
					if info.Reachable {
						online++
					}
				}
				return omap.From(
					"total", len(infos),
					"online", online,
					"offline", len(infos)-online,
					"devices", device.List(infos),
				), nil
			},
		},
		{
			Name:        "netdev_system_info",
			Description: "Get normalized identity and resource information for one device: hostname, model, version, uptime, CPU and memory.",
			Properties:  []mcpserver.NamedProperty{deviceProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapSystem, func(ctx context.Context, driver device.Driver) (any, error) {
				result, err := driver.(device.SystemDriver).SystemInfo(ctx)
				if err != nil {
					return nil, err
				}
				return result.Map(), nil
			}),
		},
		{
			Name:        "netdev_interfaces",
			Description: "List physical and logical interfaces or switch ports with status, MAC, speed and counters when the device exposes them.",
			Properties:  []mcpserver.NamedProperty{deviceProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapInterfaces, func(ctx context.Context, driver device.Driver) (any, error) {
				result, err := driver.(device.InterfaceDriver).Interfaces(ctx)
				if err != nil {
					return nil, err
				}
				return omap.From("device", argsDevice(ctx), "count", len(result), "interfaces", device.List(result)), nil
			}),
		},
		{
			Name:        "netdev_ip_addresses",
			Description: "List the IP addresses configured on a device and the interfaces they belong to.",
			Properties:  []mcpserver.NamedProperty{deviceProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapIPAddresses, func(ctx context.Context, driver device.Driver) (any, error) {
				result, err := driver.(device.IPAddressDriver).IPAddresses(ctx)
				if err != nil {
					return nil, err
				}
				return omap.From("count", len(result), "addresses", device.List(result)), nil
			}),
		},
		{
			Name:        "netdev_routes",
			Description: "List the routing table with destinations, gateways, interfaces and administrative distance.",
			Properties:  []mcpserver.NamedProperty{deviceProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapRoutes, func(ctx context.Context, driver device.Driver) (any, error) {
				result, err := driver.(device.RouteDriver).Routes(ctx)
				if err != nil {
					return nil, err
				}
				return omap.From("count", len(result), "routes", device.List(result)), nil
			}),
		},
		{
			Name:        "netdev_neighbors",
			Description: "List discovered neighboring devices through LLDP, CDP or MNDP with IP, MAC, hostname and local port.",
			Properties:  []mcpserver.NamedProperty{deviceProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapNeighbors, func(ctx context.Context, driver device.Driver) (any, error) {
				result, err := driver.(device.NeighborDriver).Neighbors(ctx)
				if err != nil {
					return nil, err
				}
				return omap.From("count", len(result), "neighbors", device.List(result)), nil
			}),
		},
		{
			Name:        "netdev_dhcp_leases",
			Description: "List active DHCP leases with hostname, MAC, IP and remaining lease time.",
			Properties:  []mcpserver.NamedProperty{deviceProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapDHCP, func(ctx context.Context, driver device.Driver) (any, error) {
				result, err := driver.(device.DHCPDriver).DHCPLeases(ctx)
				if err != nil {
					return nil, err
				}
				return omap.From("count", len(result), "leases", device.List(result)), nil
			}),
		},
		{
			Name:        "netdev_rename_device",
			Description: "Rename a device in the NetDev MCP inventory. Requires confirm=true. This only changes local inventory state, not the device itself.",
			Properties: []mcpserver.NamedProperty{
				deviceProp,
				{Name: "new_name", Prop: mcpserver.Property{Type: "string", Description: "New device name."}},
				{Name: "confirm", Prop: mcpserver.Property{Type: "boolean", Description: "Must be true to apply.", Default: false}},
			},
			Required: []string{"device", "new_name", "confirm"},
			Handler: func(_ context.Context, args map[string]any) (any, error) {
				if !argBool(args, "confirm") {
					return omap.From("error", "Rename cancelled: pass confirm=true to proceed."), nil
				}
				oldName := argString(args, "device")
				newName := argString(args, "new_name")
				if err := reg.Rename(oldName, newName); err != nil {
					return errorMap(err), nil
				}
				return omap.From("status", "renamed", "from", oldName, "to", newName), nil
			},
		},
		{
			Name:        "netdev_add_reservation",
			Description: "Create a static DHCP reservation on a device. Requires confirm=true. Use the current IP and MAC from netdev_dhcp_leases or netdev_wifi_clients.",
			Properties: []mcpserver.NamedProperty{
				deviceProp,
				{Name: "name", Prop: mcpserver.Property{Type: "string", Description: "Reservation name."}},
				{Name: "mac", Prop: mcpserver.Property{Type: "string", Description: "Client MAC address."}},
				{Name: "ip", Prop: mcpserver.Property{Type: "string", Description: "IP address to reserve."}},
				{Name: "confirm", Prop: mcpserver.Property{Type: "boolean", Description: "Must be true to apply.", Default: false}},
			},
			Required: []string{"device", "name", "mac", "ip", "confirm"},
			Handler: withDriver(reg, device.CapDhcpReservation, func(ctx context.Context, driver device.Driver) (any, error) {
				args := argsFromContext(ctx)
				if !argBool(args, "confirm") {
					return omap.From("error", "Reservation cancelled: pass confirm=true to proceed."), nil
				}
				name := argString(args, "name")
				mac := argString(args, "mac")
				ip := argString(args, "ip")
				if err := driver.(device.ReservationDriver).AddReservation(ctx, name, mac, ip); err != nil {
					return nil, err
				}
				return omap.From("device", argsDevice(ctx), "reservation", omap.From("name", name, "mac", mac, "ip", ip), "status", "saved"), nil
			}),
		},
		{
			Name:        "netdev_rename_reservation",
			Description: "Rename an existing DHCP reservation on a device. Requires confirm=true.",
			Properties: []mcpserver.NamedProperty{
				deviceProp,
				{Name: "name", Prop: mcpserver.Property{Type: "string", Description: "Current reservation name."}},
				{Name: "new_name", Prop: mcpserver.Property{Type: "string", Description: "New reservation name."}},
				{Name: "confirm", Prop: mcpserver.Property{Type: "boolean", Description: "Must be true to apply.", Default: false}},
			},
			Required: []string{"device", "name", "new_name", "confirm"},
			Handler: withDriver(reg, device.CapRenameReservation, func(ctx context.Context, driver device.Driver) (any, error) {
				args := argsFromContext(ctx)
				if !argBool(args, "confirm") {
					return omap.From("error", "Rename cancelled: pass confirm=true to proceed."), nil
				}
				name := argString(args, "name")
				newName := argString(args, "new_name")
				if err := driver.(device.ReservationDriver).RenameReservation(ctx, name, newName); err != nil {
					return nil, err
				}
				return omap.From("device", argsDevice(ctx), "from", name, "to", newName, "status", "renamed"), nil
			}),
		},
		{
			Name:        "netdev_wifi_clients",
			Description: "List associated Wi-Fi clients with MAC, IP, SSID, radio and signal strength.",
			Properties:  []mcpserver.NamedProperty{deviceProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapWiFi, func(ctx context.Context, driver device.Driver) (any, error) {
				result, err := driver.(device.WiFiDriver).WiFiClients(ctx)
				if err != nil {
					return nil, err
				}
				return omap.From("count", len(result), "clients", device.List(result)), nil
			}),
		},
		{
			Name:        "netdev_wifi_networks",
			Description: "List the configured Wi-Fi networks (SSID and band) on a device.",
			Properties:  []mcpserver.NamedProperty{deviceProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapWiFiConfig, func(ctx context.Context, driver device.Driver) (any, error) {
				result, err := driver.(device.WiFiConfigDriver).WiFiNetworks(ctx)
				if err != nil {
					return nil, err
				}
				items := make([]any, 0, len(result))
				for _, network := range result {
					items = append(items, omap.From("ssid", network.SSID, "band", network.Band, "enabled", network.Enabled))
				}
				return omap.From("count", len(items), "networks", items), nil
			}),
		},
		{
			Name:        "netdev_set_wifi",
			Description: "Change the SSID and/or password of a Wi-Fi network. Requires confirm=true. The owner's network is protected and cannot be changed.",
			Properties: []mcpserver.NamedProperty{
				deviceProp,
				{Name: "ssid", Prop: mcpserver.Property{Type: "string", Description: "Current SSID to change."}},
				{Name: "new_ssid", Prop: mcpserver.Property{Type: "string", Description: "New SSID. Leave empty to keep the current name."}},
				{Name: "new_password", Prop: mcpserver.Property{Type: "string", Description: "New Wi-Fi password. Leave empty to keep the current password."}},
				{Name: "confirm", Prop: mcpserver.Property{Type: "boolean", Description: "Must be true to apply.", Default: false}},
			},
			Required: []string{"device", "ssid", "confirm"},
			Handler: withDriver(reg, device.CapWiFiConfig, func(ctx context.Context, driver device.Driver) (any, error) {
				args := argsFromContext(ctx)
				if !argBool(args, "confirm") {
					return omap.From("error", "Wi-Fi update cancelled: pass confirm=true to proceed."), nil
				}
				ssid := argString(args, "ssid")
				newSSID := argString(args, "new_ssid")
				newPassword := argString(args, "new_password")
				if err := driver.(device.WiFiConfigDriver).UpdateWiFi(ctx, ssid, newSSID, newPassword); err != nil {
					return errorMap(err), nil
				}
				return omap.From("status", "updated", "ssid", device.StringOr(newSSID, ssid)), nil
			}),
		},
		{
			Name:        "netdev_firewall",
			Description: "List firewall filter and NAT rules with chain, action, protocol, source, target and comments.",
			Properties:  []mcpserver.NamedProperty{deviceProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapFirewall, func(ctx context.Context, driver device.Driver) (any, error) {
				result, err := driver.(device.FirewallDriver).Firewall(ctx)
				if err != nil {
					return nil, err
				}
				return omap.From("count", len(result), "rules", device.List(result)), nil
			}),
		},
		{
			Name:        "netdev_port_forwards",
			Description: "List port forwarding or destination NAT rules with external and internal ports and target host.",
			Properties:  []mcpserver.NamedProperty{deviceProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapPortForwards, func(ctx context.Context, driver device.Driver) (any, error) {
				result, err := driver.(device.PortForwardDriver).PortForwards(ctx)
				if err != nil {
					return nil, err
				}
				return omap.From("count", len(result), "forwards", device.List(result)), nil
			}),
		},
		{
			Name:        "netdev_logs",
			Description: "Read recent device logs. Use topics to filter by topic or message text.",
			Properties:  []mcpserver.NamedProperty{deviceProp, limitProp, topicsProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapLogs, func(ctx context.Context, driver device.Driver) (any, error) {
				args := argsFromContext(ctx)
				result, err := driver.(device.LogsDriver).Logs(ctx, argInt(args, "limit", 100), argString(args, "topics"))
				if err != nil {
					return nil, err
				}
				return omap.From("count", len(result), "logs", device.List(result)), nil
			}),
		},
		{
			Name:        "netdev_ping",
			Description: "Run a ping from the device itself to a target host. Returns sent, received, packet loss and average RTT.",
			Properties: []mcpserver.NamedProperty{
				deviceProp,
				{Name: "target", Prop: mcpserver.Property{Type: "string", Description: "Hostname or IP address to ping."}},
				{Name: "count", Prop: mcpserver.Property{Type: "integer", Description: "Number of packets. Defaults to 4.", Default: 4}},
			},
			Required: []string{"device", "target"},
			Handler: withDriver(reg, device.CapPing, func(ctx context.Context, driver device.Driver) (any, error) {
				args := argsFromContext(ctx)
				result, err := driver.(device.PingDriver).Ping(ctx, argString(args, "target"), argInt(args, "count", 4))
				if err != nil {
					return nil, err
				}
				return result.Map(), nil
			}),
		},
		{
			Name:        "netdev_config",
			Description: "Export the running configuration. RouterOS exports with hide-sensitive; CLI devices return show running-config or the equivalent.",
			Properties:  []mcpserver.NamedProperty{deviceProp},
			Required:    []string{"device"},
			Handler: withDriver(reg, device.CapConfig, func(ctx context.Context, driver device.Driver) (any, error) {
				result, err := driver.(device.ConfigDriver).ExportConfig(ctx)
				if err != nil {
					return nil, err
				}
				return omap.From("device", argsDevice(ctx), "config", result), nil
			}),
		},
		{
			Name:        "netdev_reboot",
			Description: "Reboot a device. This is disruptive: pass confirm=true explicitly. Requires network access to the management interface and credentials with write permission.",
			Properties: []mcpserver.NamedProperty{
				deviceProp,
				{Name: "confirm", Prop: mcpserver.Property{Type: "boolean", Description: "Must be true to actually reboot.", Default: false}},
			},
			Required: []string{"device", "confirm"},
			Handler: withDriver(reg, device.CapReboot, func(ctx context.Context, driver device.Driver) (any, error) {
				if !argBool(argsFromContext(ctx), "confirm") {
					return omap.From("error", "Reboot cancelled: pass confirm=true to proceed."), nil
				}
				if err := driver.(device.RebootDriver).Reboot(ctx); err != nil {
					return nil, err
				}
				return omap.From("device", argsDevice(ctx), "reboot", "requested"), nil
			}),
		},
		{
			Name:        "netdev_command",
			Description: "Escape hatch for raw vendor commands. On RouterOS this is an API command such as /ip/firewall/filter/print; pass params.protocol=api for the binary API. On CLI devices it is a shell command. On web devices it is a relative path.",
			Properties: []mcpserver.NamedProperty{
				deviceProp,
				{Name: "command", Prop: mcpserver.Property{Type: "string", Description: "Vendor command or path."}},
				{Name: "params", Prop: mcpserver.Property{Type: "string", Description: "Optional JSON object with command parameters."}},
			},
			Required: []string{"device", "command"},
			Handler: withDriver(reg, device.CapCommand, func(ctx context.Context, driver device.Driver) (any, error) {
				args := argsFromContext(ctx)
				params, err := argParams(args)
				if err != nil {
					return nil, err
				}
				result, err := driver.(device.CommandDriver).Command(ctx, argString(args, "command"), params)
				if err != nil {
					return nil, err
				}
				return omap.From("device", argsDevice(ctx), "result", result), nil
			}),
		},
		{
			Name:        "netdev_http",
			Description: "Escape hatch for a raw HTTP request against a web-managed device API. Use it to reach endpoints that are not normalized yet, for example a firmware-specific Huawei or ZTE path.",
			Properties: []mcpserver.NamedProperty{
				deviceProp,
				{Name: "method", Prop: mcpserver.Property{Type: "string", Description: "HTTP method. Defaults to GET.", Default: "GET"}},
				{Name: "path", Prop: mcpserver.Property{Type: "string", Description: "Path relative to the device base URL."}},
				{Name: "body", Prop: mcpserver.Property{Type: "string", Description: "Optional request body (XML or JSON)."}},
				{Name: "headers", Prop: mcpserver.Property{Type: "string", Description: "Optional JSON object with extra request headers."}},
			},
			Required: []string{"device", "path"},
			Handler: withDriver(reg, device.CapHTTP, func(ctx context.Context, driver device.Driver) (any, error) {
				args := argsFromContext(ctx)
				headers, err := argHeaders(args)
				if err != nil {
					return nil, err
				}
				result, err := driver.(device.HTTPDriver).HTTP(ctx,
					strings.ToUpper(device.StringOr(argString(args, "method"), "GET")),
					argString(args, "path"),
					[]byte(argString(args, "body")),
					headers,
				)
				if err != nil {
					return nil, err
				}
				return omap.From("device", argsDevice(ctx), "response", result.Map()), nil
			}),
		},
		{
			Name:        "netdev_topology",
			Description: "Build a compact fleet overview: each device with site, model, version, reachability and neighbor count. Useful as a starting point for incident triage.",
			Properties:  []mcpserver.NamedProperty{},
			Handler: func(ctx context.Context, _ map[string]any) (any, error) {
				reports := reg.Reports()
				entries := make([]any, 0, len(reports))
				for _, report := range reports {
					entry := omap.From(
						"name", report.Name,
						"vendor", string(report.Vendor),
						"model", report.Model,
						"site", report.Site,
						"host", report.Host,
						"error", report.Error,
					)
					neighbors := 0
					if driver, err := reg.Driver(report.Name); err == nil {
						if systemDriver, ok := driver.(device.SystemDriver); ok {
							if info, err := systemDriver.SystemInfo(ctx); err == nil {
								entry.Set("version", info.Version)
								entry.Set("uptime", info.Uptime)
								entry.Set("reachable", info.Reachable)
							}
						}
						if neighborDriver, ok := driver.(device.NeighborDriver); ok {
							if items, err := neighborDriver.Neighbors(ctx); err == nil {
								neighbors = len(items)
							}
						}
					}
					entry.Set("neighbor_count", neighbors)
					entries = append(entries, entry)
				}
				return omap.From("count", len(entries), "devices", entries), nil
			},
		},
		{
			Name:        "netdev_setup",
			Description: "Open a temporary web form only when no device in the inventory has credentials. The user picks the type (Auto, MikroTik, ZTE ZXHN H199A, Huawei WiFi BE3 or SSH switch), gives it a name, enters IP, username and password. Auto detects the vendor and a wrong pick falls back to the other drivers.",
			Properties:  []mcpserver.NamedProperty{},
			Handler: func(_ context.Context, _ map[string]any) (any, error) {
				setup, urls, err := getOrStartSetup(reg)
				if errors.Is(err, ErrAlreadyAuthenticated) {
					return omap.From(
						"status", "not_needed",
						"message", "Devices are already authenticated in the inventory. The setup page was not opened.",
						"inventory_path", inventoryPath(),
					), nil
				}
				if err != nil {
					return errorMap(err), nil
				}
				links := make([]any, 0, len(urls))
				for _, url := range urls {
					links = append(links, url)
				}
				_ = setup
				return omap.From(
					"status", "open",
					"instructions", "Open one of the URLs, fill in the IP, username and password, select the type and click Test and save.",
					"urls", links,
					"expires_in", "60m",
					"inventory_path", inventoryPath(),
				), nil
			},
		},
		{
			Name:        "netdev_setup_status",
			Description: "Check whether the temporary authentication form is still open and whether any devices were saved.",
			Properties:  []mcpserver.NamedProperty{},
			Handler: func(_ context.Context, _ map[string]any) (any, error) {
				return setupStatusMap(), nil
			},
		},
	}
}

// #region Context helpers

// contextKey is a private key type for values injected by withDriver.
type contextKey string

const (
	argsContextKey   contextKey = "netdev_args"
	deviceContextKey contextKey = "netdev_device_name"
)

// withDriver resolves a device and checks its capabilities before invoking
// the real handler. It also stores the raw arguments in the context so list
// handlers do not need to repeat argument parsing boilerplate.
func withDriver(reg *Registry, capability string, handler func(context.Context, device.Driver) (any, error)) func(context.Context, map[string]any) (any, error) {
	return func(ctx context.Context, args map[string]any) (any, error) {
		name := argString(args, "device")
		if name == "" {
			return errorMap(fmt.Errorf("missing required argument: device")), nil
		}
		driver, check, err := reg.resolve(name, capability)
		if err != nil {
			return errorMap(err), nil
		}
		if err := check(); err != nil {
			return errorMap(err), nil
		}
		ctx = context.WithValue(ctx, argsContextKey, args)
		ctx = context.WithValue(ctx, deviceContextKey, name)
		result, err := handler(ctx, driver)
		if err != nil {
			return errorMap(err), nil
		}
		return result, nil
	}
}

// argsFromContext returns the original tool arguments.
func argsFromContext(ctx context.Context) map[string]any {
	if args, ok := ctx.Value(argsContextKey).(map[string]any); ok {
		return args
	}
	return map[string]any{}
}

// argsDevice returns the resolved device name.
func argsDevice(ctx context.Context) string {
	if name, ok := ctx.Value(deviceContextKey).(string); ok {
		return name
	}
	return ""
}

// #endregion

// #region Argument helpers

// argString converts a tool argument to a trimmed string.
func argString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// argInt converts a tool argument to an integer.
func argInt(args map[string]any, key string, fallback int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	switch value := v.(type) {
	case float64:
		return int(value)
	case float32:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}

// argBool converts a tool argument to a boolean.
func argBool(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return false
	}
	switch value := v.(type) {
	case bool:
		return value
	case string:
		return device.BoolText(value)
	default:
		return device.MapBool(value)
	}
}

// argParams parses the optional params argument as a JSON object.
func argParams(args map[string]any) (map[string]any, error) {
	value, ok := args["params"]
	if !ok || value == nil {
		return map[string]any{}, nil
	}
	if object, ok := value.(map[string]any); ok {
		return object, nil
	}
	text := argString(args, "params")
	if text == "" {
		return map[string]any{}, nil
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(text), &object); err != nil {
		return nil, fmt.Errorf("params must be a JSON object: %w", err)
	}
	return object, nil
}

// argHeaders parses the optional headers argument as a string map.
func argHeaders(args map[string]any) (map[string]string, error) {
	value, ok := args["headers"]
	if !ok || value == nil {
		return map[string]string{}, nil
	}
	object := map[string]any{}
	switch typed := value.(type) {
	case map[string]any:
		object = typed
	case map[string]string:
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			out[k] = v
		}
		return out, nil
	default:
		text := argString(args, "headers")
		if text == "" {
			return map[string]string{}, nil
		}
		if err := json.Unmarshal([]byte(text), &object); err != nil {
			return nil, fmt.Errorf("headers must be a JSON object: %w", err)
		}
	}
	out := make(map[string]string, len(object))
	for key, value := range object {
		out[key] = device.MapString(value)
	}
	return out, nil
}

// singleReport builds a capability report for one device.
func singleReport(reg *Registry, name string) (device.CapabilityReport, error) {
	cfg, err := reg.Config(name)
	if err != nil {
		return device.CapabilityReport{}, err
	}
	report := device.CapabilityReport{
		Name: name, Vendor: cfg.Vendor, Model: cfg.Model, Site: cfg.Site, Host: cfg.Host,
	}
	driver, err := reg.Driver(name)
	if err != nil {
		report.Error = err.Error()
		return report, nil
	}
	report.Capabilities = driver.Capabilities()
	return report, nil
}

// errorMap turns an error into the structured shape used by the other GTI
// MCP servers, so agents can react without parsing a plain string.
func errorMap(err error) *omap.M {
	return omap.From("error", err.Error())
}

// #endregion
