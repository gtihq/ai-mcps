# NetDev MCP server

A single Go MCP server for routers and switches from multiple vendors. Instead of running one MCP per device family, it exposes one normalized tool surface and dispatches each call to the right driver. It started as an aggregation of ideas from RouterOS MCP, wiresage/mikrotik-mcp, MikroMCP, generic SSH MCP servers and the Huawei/ZTE home gateway web APIs. It uses the repository `internal/mcpserver` stdio transport.

## Run

Build the binary from this directory:

```bash
go build -o netdev-mcp .
```

On Windows, use `go build -o netdev-mcp.exe .`. You can also run it without building:

```bash
go run .
```

Register the server with your agent. The command it runs is the built binary. For example, for OpenCode:

```bash
opencode mcp add netdev --global -- /absolute/path/to/ai-mcps/netdev-mcp/netdev-mcp
```

Stdio mode is the default. HTTP mode is optional and useful for dev because it is also reachable through Tailscale:

```bash
NETDEV_HTTP_TOKEN=change-me ./netdev-mcp -listen 0.0.0.0:8765
curl -H 'Authorization: Bearer change-me' http://<tailscale-ip>:8765/healthz
```

In HTTP mode POST JSON-RPC messages to `/mcp`. The sequence `initialize`, `tools/list`, `tools/call` works the same as over stdio. Device connections use inventory `host` values directly, so Tailscale IPs and MagicDNS names work without extra setup.

## Setup screen

The first time there are no saved credentials, open the temporary authentication page:

```bash
./netdev-mcp -setup
```

Or call the `netdev_setup` MCP tool. Both are no-ops when the inventory already has a device with username and password: the tool returns `status: not_needed` and `-setup` exits. Use `-force-setup` to reopen the form on purpose. The page shows the current inventory on the left and the add form on the right. It expires after 60 minutes. Use `netdev_setup_status` to check whether it is still open.

## Inventory

Copy `devices.example.json` to `devices.json` (gitignored) or set `NETDEV_INVENTORY` to another path. `NETDEV_DEVICES` accepts a JSON array and skips the file entirely. Every string expands `${ENV_VAR}`, so keep credentials in environment variables.

```json
{
  "devices": [
    {
      "name": "rb5009",
      "vendor": "mikrotik",
      "host": "192.168.88.1",
      "username": "admin",
      "password": "${NETDEV_RB5009_PASSWORD}"
    }
  ]
}
```

Fields: `name`, `vendor`, `model`, `site`, `host`, `port`, `username`, `password`, `tls`, `insecure_tls`, `disabled`, `profile`, `timeout`, `endpoints`, `extra`. Vendor values: `mikrotik`, `zte`, `huawei`, `huawei_json`, `ssh`, `auto`.

## Tools

| Tool | Description |
|------|-------------|
| `netdev_list_devices` | Inventory with vendors, models, sites and capabilities. |
| `netdev_capabilities` | Capabilities of one device or all devices. |
| `netdev_health` | Concurrent reachability and resource check for the fleet. |
| `netdev_system_info` | Hostname, model, version, uptime, CPU and memory. |
| `netdev_interfaces` | Interfaces or switch ports with status, MAC and speed. |
| `netdev_ip_addresses` | Configured addresses and their interfaces. |
| `netdev_routes` | Routing table. |
| `netdev_neighbors` | LLDP, CDP or MNDP neighbors. |
| `netdev_dhcp_leases` | Active DHCP clients and static reservations. |
| `netdev_add_reservation` | Create a static DHCP reservation. Requires `confirm=true`. |
| `netdev_rename_reservation` | Rename an existing DHCP reservation. Requires `confirm=true`. |
| `netdev_wifi_networks` | List configured Wi-Fi SSIDs and bands. |
| `netdev_set_wifi` | Change a Wi-Fi SSID and/or password. Requires `confirm=true`. The owner's network is protected by default (`Gio Furhouse`, `Gio 5Ghz`) and the list can be extended with `extra.protected_ssids`. |
| `netdev_rename_device` | Rename an inventory entry. Requires `confirm=true`. |
| `netdev_wifi_clients` | Associated Wi-Fi stations and signal strength. |
| `netdev_firewall` | Firewall filter and NAT rules. |
| `netdev_port_forwards` | Port forwarding or destination NAT rules. |
| `netdev_logs` | Logs with optional topic filter and limit. |
| `netdev_ping` | Ping from the device itself. |
| `netdev_config` | Running configuration export. |
| `netdev_reboot` | Reboot. Requires `confirm=true`. |
| `netdev_command` | Raw RouterOS API command, SSH CLI command or web path. |
| `netdev_http` | Raw HTTP request against a web-managed device API. |
| `netdev_topology` | Fleet overview with versions and neighbor counts. |
| `netdev_setup` | Open the temporary browser authentication form. |
| `netdev_setup_status` | Check whether the form is open and what was saved. |

All device-scoped tools take a `device` argument with the inventory name. Drivers advertise capabilities, so unsupported operations return a clear `error` object.

## Layout

| Path | Responsibility |
|------|----------------|
| `main.go` | MCP entry point and setup/HTTP flags |
| `config.go` | Inventory loading and env expansion |
| `registry.go` | Lazy driver creation and fleet operations |
| `setup.go`, `setup_pages.go`, `http_transport.go` | Temporary authentication form and HTTP transport |
| `tools.go` | Tool definitions and handlers |
| `device/` | Vendor-neutral types and driver contracts |
| `drivers/` | MikroTik, ZTE, Huawei XML, Huawei JSON, SSH CLI and autodetect drivers |
| `*_test.go` | Tests with `testing`, `httptest` and a fake RouterOS API socket |

## Environment

| Variable | Purpose |
|----------|---------|
| `NETDEV_INVENTORY` | Path to the inventory JSON file |
| `NETDEV_DEVICES` | Inventory as a JSON array, overrides the file |
| `NETDEV_HTTP_TOKEN` | Bearer token required by HTTP mode |
| `NETDEV_DEBUG_ZTE` | Prints the ZTE login flow to stderr |

## Development

Run the tests and the vet check from the repository root:

```bash
go test ./netdev-mcp/...
go vet ./netdev-mcp/...
```

The ZTE driver logs into the H199A Lua web UI with a SHA-256 password challenge and reads DHCP bindings through `menuData` requests. RouterOS uses REST first, then the binary API, then SSH for ping and config export. The Huawei JSON driver authenticates with the router's SCRAM flow. Extra vendor options belong in `extra` and endpoint overrides belong in `endpoints`.

See [../README.md](../README.md) for the full repository.
