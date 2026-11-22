# netdev-mcp

One Go MCP server for routers and switches from multiple vendors. Instead of running one MCP per device family, this server exposes a single normalized tool surface and dispatches each call to the right driver.

The design aggregates features found in existing community MCP servers:

- [sevaepsteyn/routeros_mcp](https://github.com/sevaepsteyn/routeros_mcp): multi-device inventory, API/SSH fallback, system info, interfaces, routes, neighbors, logs, config export, ping, raw command.
- [wiresage/mikrotik-mcp](https://github.com/wiresage/mikrotik-mcp): broad RouterOS API coverage and fleet inventory.
- [MikroMCP](https://mikromcp.com/): RouterOS and SwOS coverage.
- [uarlouski/ssh-mcp-server](https://github.com/uarlouski/ssh-mcp-server) and [cisco-ssh-mcp](https://mcpservers.org/servers/aroshak/cisco-ssh-mcp): generic SSH access to CLI network devices.
- Huawei home gateway web API patterns (`/api/webserver/SesTokInfo`, `__RequestVerificationToken`, `/api/device/control`) and ZTE ZXHN Lua web UI paths.

Everything is implemented natively in Go with the repository `internal/mcpserver` stdio transport.

## Supported devices

| Vendor value | Devices | Transport | Capabilities |
|---|---|---|---|
| `mikrotik` | RouterOS 6 and 7, SwOS-like CLI devices | REST 80/443, API 8728/8729, SSH 22 | system, interfaces, IPs, routes, neighbors, DHCP, Wi-Fi, firewall, port forwards, logs, ping, config, reboot, raw command |
| `zte` | ZXHN H199A and similar ZTE home gateways | Web UI over HTTP | system, interfaces, DHCP, Wi-Fi, port forwards, logs, reboot (opt in), raw HTTP |
| `huawei` | Older Huawei home gateways | Classic XML web API over HTTP | system, interfaces, DHCP, Wi-Fi, port forwards, logs, reboot, raw HTTP |
| `huawei_json` | Huawei WiFi BE3 with WUKUN firmware | New JSON web API over HTTP | system info from the public `/api/system/deviceinfo` endpoint, including CSRF tokens |
| `ssh` | Cisco IOS, Huawei VRP, HP and other CLI switches | SSH with `cisco` or `huawei_vrp` profile | system, interfaces, routes, neighbors, config, ping, raw command |

Drivers advertise their capabilities. A tool call against an unsupported capability returns a clear `error` object rather than failing silently.

## Build and run

From the repository root:

```bash
go build -o netdev-mcp.exe ./netdev-mcp
```

Stdio mode, the default used by MCP clients:

```bash
netdev-mcp.exe -inventory ./netdev-mcp/devices.json
```

HTTP mode, useful for dev when the server must also be reachable over Tailscale:

```bash
NETDEV_HTTP_TOKEN=change-me netdev-mcp.exe -listen 0.0.0.0:8765
curl -H 'Authorization: Bearer change-me' http://<tailscale-ip>:8765/healthz
```

In HTTP mode POST a JSON-RPC message to `/mcp`. The transport is stateless, so the usual `initialize` then `tools/list` then `tools/call` sequence works the same as over stdio. Binding to `0.0.0.0` makes the listener available on the Tailscale interface; the token header is strongly recommended.

Device connections also work over Tailscale out of the box: put the Tailscale IP or MagicDNS name in the inventory `host` field and the driver dials it directly.

## Easy authentication screen

There are two ways to open the temporary onboarding page:

```bash
netdev-mcp.exe -setup
```

Or call the `netdev_setup` MCP tool from your agent. Both print a short lived URL containing a random token, including the addresses of all local interfaces so you can pick the Tailscale one.

The screen uses a two column workspace: the current inventory on the left and the add form on the right, always visible. Pick what you are adding (Auto, MikroTik RouterOS, ZTE ZXHN H199A, Huawei WiFi BE3 or SSH switch), give it a name, enter IP or host, username and password, then click **Adicionar à lista**. Each queued row can be edited or removed. `Auto` probes the device with every driver and keeps the one that answers, so a wrong pick does not block onboarding. Even when a specific type is chosen, the setup flow verifies it and falls back to the other drivers if it returns 404 or rejects the login. Nothing is written until you click **Testar e salvar**. The queued list and the inventory stay on screen after saving, so nothing appears to reset. The result panel sits above the columns and the closing screen has a **Fechar página** button.

When you save, the server authenticates every queued device concurrently, keeps only the ones that replied, writes them to the inventory and reloads the MCP registry immediately. A result page shows one status line per device and a **Concluir e fechar servidor** button that shuts the temporary server down. It also closes itself after 60 minutes even if nobody clicks.

Use `netdev_setup_status` from the agent to check whether the page is still open and what was saved.

The page has no external assets, the token is required on every request, passwords are never logged, and the inventory file is written with owner-only permissions on platforms that support it.

## Inventory

Copy `devices.example.json` to `devices.json` (gitignored) or set `NETDEV_INVENTORY` to another path. You can also set `NETDEV_DEVICES` to a JSON array to skip the file entirely. Every string expands `${ENV_VAR}`, so keep credentials in environment variables.

Minimal example:

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

Common fields: `name`, `vendor`, `model`, `site`, `host`, `port`, `username`, `password`, `tls`, `insecure_tls`, `disabled`, `profile` (SSH dialect), `timeout`, `endpoints` (path overrides) and `extra` (vendor options such as `private_key_path`, `password_type`, `csrf_header` or `profile_commands`).

For the RouterOS API fallback, SSH config export and ping, you can pass an inline PEM key in `extra.private_key` or a path in `extra.private_key_path`.

## Tools

| Tool | Purpose |
|---|---|
| `netdev_list_devices` | Inventory with vendors, models, sites and capabilities. Call this first. |
| `netdev_capabilities` | Capabilities of one device or all devices. |
| `netdev_health` | Concurrent reachability and resource check for the whole fleet. |
| `netdev_system_info` | Hostname, model, version, uptime, CPU and memory. |
| `netdev_interfaces` | Interfaces or switch ports with status, MAC and speed. |
| `netdev_ip_addresses` | Configured addresses and their interfaces. |
| `netdev_routes` | Routing table. |
| `netdev_neighbors` | LLDP, CDP or MNDP neighbors. |
| `netdev_dhcp_leases` | Active DHCP clients. |
| `netdev_wifi_clients` | Associated Wi-Fi stations and signal strength. |
| `netdev_firewall` | Firewall filter and NAT rules. |
| `netdev_port_forwards` | Port forwarding or destination NAT rules. |
| `netdev_logs` | Logs with optional topic filter and limit. |
| `netdev_ping` | Ping from the device itself. |
| `netdev_config` | Running configuration export. |
| `netdev_reboot` | Reboot. Requires `confirm=true`. |
| `netdev_command` | Raw RouterOS API command, SSH CLI command or web path. |
| `netdev_http` | Raw HTTP request against a web-managed device API. |
| `netdev_topology` | Compact fleet overview with versions and neighbor counts. |
| `netdev_setup` | Open the temporary browser form to authenticate devices and save them to the inventory. |
| `netdev_setup_status` | Check whether the form is still open and what was saved. |

All device-scoped tools take a `device` argument with the inventory name.

## Vendor notes and endpoint discovery

### MikroTik

REST v7 is tried first. If REST is disabled the driver falls back to the binary API on port 8728 or 8729. Ping and config export use SSH because RouterOS has no equivalent REST endpoint. `netdev_command` accepts paths such as `/ip/firewall/filter/print`; add `"params":"{\"protocol\":\"api\"}"` to force the binary API.

### ZTE ZXHN H199A

These gateways expose a Lua web UI, and the exact `pid` numbers change with ISP firmware. The defaults in `devices.example.json` are a starting point. Discover the real paths with browser developer tools, then override them in the `endpoints` map. The login form field names can be overridden with `extra.username_field` and `extra.password_field`. Reboot is disabled until `endpoints.reboot` is set explicitly.

If a page is not normalized yet, call `netdev_http` with the discovered path and parse the returned HTML or JSON.

### Huawei WiFi BE3

The driver uses the classic Huawei session flow: fetch `SesTokInfo`, derive the password with `password_type` 4, log in, then attach the CSRF token and session ID to every request. Some firmware builds require type 2 or plaintext; the driver retries those automatically, or you can pin one with `extra.password_type`.

The WiFi BE3 with WUKUN firmware (for example `WUKUN-BE32-41`) uses a different, newer JSON API. The driver `huawei_json` reads `/api/system/deviceinfo` without a login; that response carries the model, serial, uptime and fresh `csrf_param` and `csrf_token` values. Setup detects this variant automatically, so selecting `Huawei` works for both API generations. Only system info is implemented for `huawei_json` today because the read endpoints for clients and interfaces are not public on that firmware. The classic XML driver still owns the wider capability set.

### SSH switches

Profiles are `cisco` and `huawei_vrp`. Add another dialect with `extra.profile_commands` containing `system`, `interfaces`, `routes`, `neighbors`, `config` and `ping` command templates. The ping template supports `<target>` and `<count>` placeholders.

## Security

- Inventory files are gitignored. Use environment variables for passwords and keys.
- `netdev_reboot` requires `confirm=true`.
- HTTP mode is optional, binds only when `-listen` is passed, and supports `NETDEV_HTTP_TOKEN`.
- Device web UI certificates are self-signed; `insecure_tls` is available per device. SSH host keys are not verified by default.
- This server is for devices you own or are authorized to administer.

## Tests

```bash
go test ./netdev-mcp/...
```

The suite uses `httptest` servers and a fake RouterOS API socket, so no physical hardware is needed.
