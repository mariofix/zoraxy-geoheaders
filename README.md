# Zoraxy GeoHeaders Plugin (`zoraxy-geoheaders`)

A high-performance [Zoraxy](https://github.com/tobychui/zoraxy) Router Plugin that inspects incoming requests and injects the `X-Country` header (and optional `X-Continent` header) for any proxy endpoint tagged with `geo-match`.

---

## 🌟 Key Features

- **Tag-Based Interception**: Automatically matches endpoints tagged with `geo-match` in Zoraxy.
- **Header Injection**: Injects `X-Country: <ISO_CODE>` (customizable header name) and optional `X-Continent: <CONTINENT>` into upstream requests.
- **Built-in Fast GeoIP Engine**: Memory-efficient binary search over sorted IPv4 and IPv6 CIDR/range tables.
- **Zero External Dependencies**: Built entirely with Go standard library components for zero CVE vulnerability surface and maximum portability.
- **Special Zone & Private IP Handling**: Accurately classifies RFC1918 private subnets, loopback (`127.0.0.1`, `::1`), link-local (`169.254.0.0/16`, `fe80::/10`), and Carrier-Grade NAT (`100.64.0.0/10`) as `LOCAL`.
- **Custom CIDR Overrides**: Map specific IP ranges or internal subnets to custom codes (e.g., `10.0.0.0/8` -> `CORP_VPN`, `203.0.113.0/24` -> `TEST`).
- **Embedded Web Management UI**: Built-in dashboard for live traffic inspection, configuration adjustments, and real-time IP lookup simulation.
- **REST Management API**: Full programmatic API for stats, health status, and live configuration updates.

---

## 🚀 How It Works in Zoraxy

1. **Introspection (`-introspect`)**:
   Zoraxy queries the plugin via `./zoraxy-geoheaders -introspect` to register its capabilities (`Type: 0` Router Plugin, `TargetTag: geo-match`, `DynamicRouter: true`).

2. **Configuration (`-configure`)**:
   When started by Zoraxy, the plugin receives its allocated port, web root path, and runtime data folder.

3. **Dynamic Sniff (`/d_sniff/`)**:
   When a request arrives at a tagged proxy rule (`geo-match`), Zoraxy sends a lightweight sniff request payload to the plugin. If enabled, the plugin responds `200 OK` (`zoraxy_plugin.SniffResultAccept`).

4. **Dynamic Capture & Enrichment (`/d_capture/`)**:
   Zoraxy forwards the captured request to the plugin's ingress. The plugin:
   - Resolves the client IP (`CF-Connecting-IP`, `X-Forwarded-For`, `X-Real-IP`, or `RemoteAddr`).
   - Looks up the country ISO code in the GeoIP database or custom overrides.
   - Injects the `X-Country` header.
   - Forwards the enriched request to the upstream backend or Zoraxy host router.

---

## 🛠️ Installation & Usage in Zoraxy

### 1. Build from Source
```bash
make build
```
This produces the standalone binary `zoraxy-geoheaders`.

### 2. Add to Zoraxy
Copy the `zoraxy-geoheaders` binary and `.introspect` file into Zoraxy's `system/plugins/` directory:
```bash
mkdir -p system/plugins/geoheaders/
cp zoraxy-geoheaders .introspect system/plugins/geoheaders/
```

### 3. Tag Zoraxy Endpoints
In the Zoraxy Web UI (Proxy Rules / Subrouting):
- Add the tag `geo-match` to any domain, subdomain, or path rule you want enriched with `X-Country`.

---

## ⚙️ Configuration Options

Configuration is persisted in `config.json` in the runtime directory:

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | `bool` | `true` | Master switch to enable or bypass header injection |
| `header_name` | `string` | `"X-Country"` | Name of the injected HTTP header |
| `fallback_country` | `string` | `"XX"` | Country code used when an IP cannot be resolved |
| `local_country` | `string` | `"LOCAL"` | Code used for loopback, private, and LAN addresses |
| `inject_continent` | `bool` | `false` | When `true`, also injects `X-Continent` header |
| `continent_header_name` | `string` | `"X-Continent"` | Name of continent header |
| `tag_filter` | `string` | `"geo-match"` | Target tag used to match endpoints in Zoraxy |
| `default_upstream` | `string` | `""` | Fallback backend URL for standalone proxying |
| `custom_overrides` | `object` | `{}` | Custom CIDR to country code mappings |
| `log_limit` | `int` | `200` | Max number of recent requests kept in memory |

---

## 📡 REST API Endpoints

- `GET /api/status`: Plugin health, uptime, total processed requests, and loaded IP ranges.
- `GET /api/config`: Retrieve current configuration.
- `POST /api/config`: Update configuration parameters on the fly.
- `GET /api/stats`: Retrieve request counters, country distribution, and recent request log buffer.
- `POST /api/stats/reset`: Clear traffic logs and counters.
- `GET /api/lookup?ip=<ip_address>`: Perform test GeoIP lookup for any IPv4 or IPv6 address.
- `POST /api/test`: Simulate header injection for a request body `{"ip": "...", "host": "..."}`.

---

## 🧪 Testing

Run the full test suite with coverage:
```bash
go test -v ./...
```

