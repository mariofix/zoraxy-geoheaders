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

### 2. Manual Installation
Copy the `zoraxy-geoheaders` binary and `.introspect` file into Zoraxy's `system/plugins/` directory:
```bash
mkdir -p system/plugins/geoheaders/
cp zoraxy-geoheaders .introspect system/plugins/geoheaders/
```

### 3. Running with Docker

If you run Zoraxy inside a Docker container, follow these steps to install and configure `zoraxy-geoheaders`:

#### Option A: Docker Compose (Recommended)

1. Obtain the plugin binary for your architecture (e.g. `zoraxy-geoheaders_linux_amd64` or `zoraxy-geoheaders_linux_arm64`) and the `.introspect` file from the [Releases](https://github.com/mariofix/zoraxy-geoheaders/releases) page (or build from source).
2. Place `zoraxy-geoheaders` and `.introspect` in a local plugin folder on your host machine, e.g. `./plugins/geoheaders/`.
3. Ensure the binary has execution permissions:
   ```bash
   chmod +x ./plugins/geoheaders/zoraxy-geoheaders
   ```
4. Mount the plugin directory into Zoraxy's container in `docker-compose.yml`:
   ```yaml
   version: '3.8'
   services:
     zoraxy:
       image: tobychui/zoraxy:latest
       container_name: zoraxy
       ports:
         - "8000:8000"
         - "80:80"
         - "443:443"
       volumes:
         - ./zoraxy-data:/opt/zoraxy/config
         # Mount plugin directory into Zoraxy:
         - ./plugins/geoheaders:/opt/zoraxy/system/plugins/geoheaders
       restart: unless-stopped
   ```
5. Start or restart the container:
   ```bash
   docker compose up -d
   ```

#### Option B: Copy into a Running Docker Container

If Zoraxy is already running in a container named `zoraxy`:

1. Create the plugin directory inside the container:
   ```bash
   docker exec zoraxy mkdir -p /opt/zoraxy/system/plugins/geoheaders
   ```
2. Copy the binary and `.introspect` file into the container:
   ```bash
   docker cp zoraxy-geoheaders zoraxy:/opt/zoraxy/system/plugins/geoheaders/zoraxy-geoheaders
   docker cp .introspect zoraxy:/opt/zoraxy/system/plugins/geoheaders/.introspect
   ```
3. Grant executable permissions and restart Zoraxy:
   ```bash
   docker exec zoraxy chmod +x /opt/zoraxy/system/plugins/geoheaders/zoraxy-geoheaders
   docker restart zoraxy
   ```

### 4. Tag & Configure Zoraxy Endpoints
In the Zoraxy Web UI (Proxy Rules / Subrouting):
1. **Verify Plugin Detection**:
   - Navigate to **Plugins** (or **Router Plugins**). Verify `GeoHeaders` appears in the plugin list.
2. **Tag Proxy Rules**:
   - Add the tag `geo-match` to any domain, subdomain, or path rule you want enriched with `X-Country` (and optional `X-Continent`).
3. **Open GeoHeaders Dashboard**:
   - Click the plugin's Web UI button in Zoraxy to access live traffic stats, custom CIDR overrides, and GeoIP testing tools.

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

