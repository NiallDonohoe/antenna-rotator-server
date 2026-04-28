# Antenna Rotator Server
Go-based server that allows remote control of antenna rotators.

## Why rotate antennas?
Rotating a directional antenna (for example a Yagi or beam) is essential in amateur radio to point the antenna's main lobe toward the station or satellite you want to work. Proper antenna orientation increases received and transmitted signal strength, reduces interference from unwanted directions, and is critical for activities like DXing, contesting and satellite tracking (azimuth-only).

## How this project helps — simple, low-cost remote control
Commercial, remotely controlled rotator systems are often expensive and tightly coupled to vendor hardware and GUIs. This project provides a lightweight, open solution that separates the control software from the hardware. By running a Go-based server (`rotator-server`) and using the `rotator-controller` package to talk to simple serial-based rotator controllers, you can:

- Control rotators over the network (LAN or Internet) from any device or script
- Integrate with satellite tracking or contesting automation without proprietary systems
- Use inexpensive or home-built controllers instead of complex, proprietary remote boxes

This removes the need to buy fully integrated, costly remote rotator systems and makes remote/automated control accessible and maintainable.

## Advantages
- **Low cost:** works with simple serial controllers and avoids vendor lock-in
- **Open & extensible:** integrate with homebrew hardware, existing apps, or scripts
- **Networked control:** expose a standard API for remote control and automation
- **Automation-friendly:** ideal for azimuth-only satellite tracking, scheduled direction changes, or contests
- **Easier maintenance:** update software instead of replacing proprietary hardware
- **Built-in Swagger UI:** point a browser at the server and drive the API by hand

---

## Quick start

### Run from source
```bash
go build -o bin/antenna-rotator-server
./bin/antenna-rotator-server
```

The server listens on port `8080`. Open <http://localhost:8080/swagger/> to use the Swagger UI.

If a serial port is attached, set it explicitly:
```bash
ROTATOR_PORT=COM3 ./bin/antenna-rotator-server          # Windows
ROTATOR_PORT=/dev/ttyUSB0 ./bin/antenna-rotator-server  # Linux/macOS
```

If no serial port is found, the server falls back to **simulation mode** so the API stays usable for testing and demos.

### Run with Docker
```bash
make docker-build
docker run --rm -p 8080:8080 \
  --device=/dev/ttyUSB0 \
  -e ROTATOR_PORT=/dev/ttyUSB0 \
  antenna-rotator-server:latest
```
Drop the `--device` and `ROTATOR_PORT` flags to run in simulation mode.

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

All non-health endpoints return JSON. Errors come back as `{"error": "..."}`.

| Method | Path           | Description                                      |
|--------|----------------|--------------------------------------------------|
| POST   | `/set-heading` | Set the antenna heading (`?heading=0..359`)      |
| GET    | `/get-heading` | Read the current heading                         |
| POST   | `/stop`        | Stop in-progress rotation                        |
| GET    | `/list-ports`  | List serial ports detected on the host           |
| GET    | `/healthz`     | Liveness probe (returns `OK` as `text/plain`)    |

### Examples
```bash
curl -X POST 'http://localhost:8080/set-heading?heading=180'
# {"heading":180,"status":"set"}

curl http://localhost:8080/get-heading
# {"heading":180}

curl -X POST http://localhost:8080/stop
# {"status":"stopped"}

curl http://localhost:8080/list-ports
# {"ports":["COM3","COM4"]}
```

---

## Hardware support

The `rotator-controller` package speaks the **Prosistel** serial protocol (8N1, 9600 baud):

| Action          | Command sent     | Response       |
|-----------------|------------------|----------------|
| Set azimuth     | `AP1XXX\r`       | —              |
| Read azimuth    | `AI1\r`          | `+AXXX\r`      |
| Stop rotation   | `AX1\r`          | —              |

Where `XXX` is the zero-padded 3-digit azimuth in degrees.

---

## Development

```bash
make build     # build the binary into ./bin
make test      # run go test ./...
make clean     # remove build artifacts
```

## Project layout
```
.
├── main.go                  # entry point
├── rotator-server/          # HTTP server, Swagger UI, OpenAPI spec
│   └── static/              # embedded swagger.html + openapi.yaml
├── rotator-controller/      # serial-port rotator driver (Prosistel)
├── Dockerfile               # multi-stage scratch image
└── Makefile                 # build/test/docker helpers
```
