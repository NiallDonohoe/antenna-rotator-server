# Antenna Rotator Server

[![CI](https://github.com/NiallDonohoe/antenna-rotator-server/actions/workflows/ci.yml/badge.svg)](https://github.com/NiallDonohoe/antenna-rotator-server/actions/workflows/ci.yml)

Go-based server that allows remote control of antenna rotators.

## Why rotate antennas?
Rotating a directional antenna (for example a Yagi or beam) is essential in amateur radio to point the antenna's main lobe toward the station or satellite you want to work. Proper antenna orientation increases received and transmitted signal strength, reduces interference from unwanted directions, and is critical for activities like DXing, contesting and satellite tracking (azimuth-only).

## How this project helps — simple, low-cost remote control
Commercial, remotely controlled rotator systems are often expensive and tightly coupled to vendor hardware and GUIs. This project provides a lightweight, open solution that separates the control software from the hardware. By running a Go-based server (`rotator-server`) and using the `rotator-controller` package to talk to simple serial-based rotator controllers, you can:

- Control rotators over the network from any device or script
- Integrate with satellite tracking or contesting automation without proprietary systems
- Use inexpensive or home-built controllers instead of complex, proprietary remote boxes

## Advantages
- **Low cost:** works with simple serial controllers and avoids vendor lock-in
- **Open & extensible:** integrate with homebrew hardware, existing apps, or scripts
- **Networked control:** expose a standard API for remote control and automation
- **Automation-friendly:** ideal for azimuth-only satellite tracking, scheduled direction changes, or contests
- **Robust serial handling:** serialized port access, automatic reconnect after USB glitches, stale-byte flushing
- **Built-in Swagger UI:** point a browser at the server and drive the API by hand

> ⚠️ The API has **no authentication**. Only expose it on a trusted LAN, or put it behind a reverse proxy that adds TLS + auth if you need Internet access.

---

## Quick start

### Run from source
```bash
make build
ROTATOR_PORT=/dev/ttyUSB0 ./bin/antenna-rotator-server   # Linux/macOS
```

### Windows

Run the native Windows binary directly against a COM port — this is the recommended way to run on Windows, since Docker Desktop's Linux VM can't see COM ports without extra USB-passthrough tooling (`usbipd-win`).

Cross-compile from Linux/macOS:
```bash
make build-windows
```
or build straight from Go on the Windows machine itself:
```powershell
go build -o antenna-rotator-server.exe .
```

Then run it:
```powershell
$env:ROTATOR_PORT = "COM3"
.\antenna-rotator-server.exe
```
No hardware attached? Run against the simulated rotator instead:
```powershell
$env:SIMULATION = "true"
.\antenna-rotator-server.exe
```

The server listens on port `8080` by default. Open <http://localhost:8080/swagger/> to use the Swagger UI.

No hardware attached? Run against the in-memory simulated rotator:
```bash
SIMULATION=true ./bin/antenna-rotator-server    # or: make run-sim
```

The server **refuses to start** if neither `ROTATOR_PORT` nor `SIMULATION=true` is set — a production server must never silently pretend to drive hardware. The startup error lists the serial ports it detected (also available at runtime via `GET /list-ports`).

### Configuration

All configuration is via environment variables:

| Variable           | Default     | Description                                                        |
|--------------------|-------------|--------------------------------------------------------------------|
| `ROTATOR_PORT`     | —           | Serial port of the rotator (e.g. `/dev/ttyUSB0`, `COM3`). Required unless simulating. |
| `ROTATOR_PROTOCOL` | `prosistel` | Rotator protocol: `prosistel` or `yaesu` (GS-232A/B).               |
| `ROTATOR_BAUD`     | `9600`      | Serial baud rate (older GS-232A interfaces often use 1200–4800).    |
| `SIMULATION`       | `false`     | `true` runs an in-memory rotator, no hardware needed.               |
| `LISTEN_ADDR`      | `:8080`     | HTTP listen address.                                                |
| `LOG_LEVEL`        | `info`      | `debug`, `info`, `warn`, or `error`.                                |
| `LOG_FORMAT`       | `text`      | `json` for structured JSON logs.                                    |

The server shuts down gracefully on `SIGINT`/`SIGTERM`, draining in-flight requests (10s budget) and closing the serial port.

### Run with Docker
```bash
make docker-build
docker run --rm -p 8080:8080 \
  --device=/dev/ttyUSB0 \
  -e ROTATOR_PORT=/dev/ttyUSB0 \
  antenna-rotator-server:latest
```
For simulation mode use `-e SIMULATION=true` and drop the `--device` flag.

Multi-arch images (amd64 + arm64, e.g. for a Raspberry Pi) build with:
```bash
make docker-buildx
```

---

## Swagger UI

The server embeds a Swagger UI page and the OpenAPI 3 spec; no external setup required.

| Path             | Description                          |
|------------------|--------------------------------------|
| `/`              | Redirects to `/swagger/`             |
| `/swagger/`      | Interactive Swagger UI               |
| `/openapi.yaml`  | Raw OpenAPI 3 spec                   |

Use the **Try it out** buttons in Swagger UI to set headings, read the current heading, stop rotation, and inspect available serial ports — all without writing any client code.

> The Swagger UI page loads its JS/CSS from `unpkg.com`. If you need a fully offline build, vendor the `swagger-ui-dist` files into `rotator-server/static/` and update the `<script>` / `<link>` tags in `swagger.html`.

---

## HTTP API

Canonical paths live under `/api/v1`; the unprefixed paths remain as aliases. All endpoints return JSON. Errors come back as `{"error": "..."}` — `400` for invalid input, `502` when serial communication with the rotator fails.

| Method | Path                  | Description                                          |
|--------|-----------------------|------------------------------------------------------|
| POST   | `/api/v1/set-heading` | Set the heading (`?heading=0..359` or JSON body)     |
| GET    | `/api/v1/get-heading` | Read the current heading                             |
| POST   | `/api/v1/stop`        | Stop in-progress rotation                            |
| GET    | `/api/v1/list-ports`  | List serial ports detected on the host               |
| GET    | `/api/v1/healthz`     | Liveness probe with mode/connection/version info     |

### Examples
```bash
curl -X POST 'http://localhost:8080/api/v1/set-heading?heading=180'
# {"heading":180,"status":"set"}

curl -X POST http://localhost:8080/api/v1/set-heading \
  -H 'Content-Type: application/json' -d '{"heading":180}'
# {"heading":180,"status":"set"}

curl http://localhost:8080/api/v1/get-heading
# {"heading":180}

curl -X POST http://localhost:8080/api/v1/stop
# {"status":"stopped"}

curl http://localhost:8080/api/v1/list-ports
# {"ports":["COM3","COM4"]}

curl http://localhost:8080/api/v1/healthz
# {"status":"ok","mode":"hardware","connected":true,"version":"1.2.0"}
```

`healthz` always returns `200` while the process is alive; monitors should alert on `"mode":"simulation"` or `"connected":false` if they expect real hardware.

---

## Hardware support

The `rotator-controller` package speaks two serial protocols (both 8N1), selected with `ROTATOR_PROTOCOL`:

### Prosistel (`ROTATOR_PROTOCOL=prosistel`, the default)

| Action          | Command sent     | Response       |
|-----------------|------------------|----------------|
| Set azimuth     | `AP1XXX\r`       | —              |
| Read azimuth    | `AI1\r`          | `+AXXX\r`      |
| Stop rotation   | `AX1\r`          | —              |

### Yaesu GS-232A/B (`ROTATOR_PROTOCOL=yaesu`)

Covers Yaesu G-450/G-650/G-800/G-1000/G-2800-series rotators attached via a GS-232 computer interface (or a compatible one such as the ERC or K3NG controllers in GS-232 mode).

| Action          | Command sent     | Response                              |
|-----------------|------------------|---------------------------------------|
| Set azimuth     | `MXXX\r`         | —                                     |
| Read azimuth    | `C\r`            | `+0XXX` (GS-232A) or `AZ=XXX` (GS-232B) |
| Stop rotation   | `S\r`            | —                                     |

Both response variants are recognised automatically, including the azimuth+elevation forms (`+0XXX+0YYY`, `AZ=XXX EL=YYY`) from az/el units — the elevation part is ignored. If your interface runs at a slow baud rate, set `ROTATOR_BAUD` accordingly.

`XXX` is always the zero-padded 3-digit azimuth in degrees. `GET /healthz` reports the active protocol.

### Adding another rotator

Protocols are defined as data, not code: each one is a `ProtocolSpec` entry in the registry in [`rotator-controller/protocols.go`](rotator-controller/protocols.go) — the command templates plus the markers that precede the azimuth in the response. The serial machinery (locking, reconnect, buffer flushing) is shared. To support a new make, add one entry to that table and a few cases to `protocols_test.go`.

Serial access is fully serialized (concurrent HTTP requests can't interleave commands), the input buffer is flushed before every command, and a dead port (e.g. unplugged USB adapter) is automatically reopened on the next request.

---

## Development

```bash
make build         # build the binary into ./bin (version-stamped from git)
make build-windows # cross-compile a Windows .exe into ./bin
make test          # go test -race ./...
make vet       # go vet ./...
make lint      # golangci-lint run
make run-sim   # build and run in simulation mode
make clean     # remove build artifacts
```

CI (GitHub Actions) runs vet, the race-enabled test suite, golangci-lint, and a multi-arch Docker build on every push and pull request.

## Project layout
```
.
├── main.go                  # entry point: config, logging, graceful shutdown
├── rotator-server/          # HTTP server, Swagger UI, OpenAPI spec
│   └── static/              # embedded swagger.html + openapi.yaml
├── rotator-controller/      # serial-port rotator driver (Prosistel)
├── .github/workflows/       # CI: vet, race tests, lint, docker build
├── Dockerfile               # multi-stage, multi-arch scratch image
└── Makefile                 # build/test/docker helpers
```
