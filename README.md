# Antenna Rotator Server

[![CI](https://github.com/NiallDonohoe/antenna-rotator-server/actions/workflows/ci.yml/badge.svg)](https://github.com/NiallDonohoe/antenna-rotator-server/actions/workflows/ci.yml)
[![Buy Me A Coffee](https://img.shields.io/badge/Buy%20Me%20A%20Coffee-donate-yellow.svg?logo=buy-me-a-coffee)](https://buymeacoffee.com/nialldonohg)

Go-based server that allows remote control of antenna rotators.

If you find this project helpful or useful, consider [buying me a coffee](https://buymeacoffee.com/nialldonohg) ☕.

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
- **Robust serial handling:** serialized port access, automatic reconnect after USB glitches, stale-byte flushing, acknowledged commands, and requests that give up instead of piling up behind a silent controller
- **Built for unattended stations:** starts even if the USB adapter isn't there yet, keeps its calibration across restarts, and ships with a systemd unit
- **Built-in Swagger UI:** point a browser at the server and drive the API by hand

> ⚠️ The API has **no authentication**. Only expose it on a trusted LAN, or put it behind a reverse proxy that adds TLS + auth if you need Internet access.

---

## Quick start

### Run from source
```bash
go build -o antenna-rotator-server .
ROTATOR_PORT=/dev/ttyUSB0 ./antenna-rotator-server   # Linux/macOS
```

### Windows

Run the native Windows binary directly against a COM port.

Cross-compile from Linux/macOS:
```bash
GOOS=windows GOARCH=amd64 go build -o antenna-rotator-server.exe .
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
SIMULATION=true ./antenna-rotator-server    # or: SIMULATION=true go run .
```

The server **refuses to start** if neither `ROTATOR_PORT` nor `SIMULATION=true` is set — a production server must never silently pretend to drive hardware. The startup error lists the serial ports it detected (also available at runtime via `GET /list-ports`).

If `ROTATOR_PORT` is set but the port can't be opened yet (say, the USB adapter is still enumerating after a power cut), the server logs a warning and **starts anyway**. Every request retries the port, so it picks the adapter up as soon as it appears; until then rotator calls return `502` and `GET /readyz` returns `503`.

### Configuration

All configuration is via environment variables:

| Variable           | Default     | Description                                                        |
|--------------------|-------------|--------------------------------------------------------------------|
| `ROTATOR_PORT`     | —           | Serial port of the rotator (e.g. `/dev/ttyUSB0`, `COM3`). Required unless simulating. |
| `ROTATOR_PROTOCOL` | `prosistel` | Rotator protocol: `prosistel` or `yaesu` (GS-232A/B).               |
| `ROTATOR_BAUD`     | `9600`      | Serial baud rate (older GS-232A interfaces often use 1200–4800).    |
| `ROTATOR_HEADING_OFFSET` | `0`   | Calibration offset in degrees, added to raw rotator readings (see [Calibration offset](#calibration-offset)). |
| `ROTATOR_OFFSET_FILE` | —        | File where `POST /offset` saves the offset so it survives restarts. A saved value takes precedence over `ROTATOR_HEADING_OFFSET`. Unset means offsets set via the API are lost on restart. |
| `SIMULATION`       | `false`     | `true` runs an in-memory rotator, no hardware needed.               |
| `LISTEN_ADDR`      | `:8080`     | HTTP listen address.                                                |
| `LOG_LEVEL`        | `info`      | `debug`, `info`, `warn`, or `error`. Read requests (`GET`) such as Node-RED polling are logged at `debug`, so they only show up there. |
| `LOG_FORMAT`       | `text`      | `json` for structured JSON logs.                                    |

The server shuts down gracefully on `SIGINT`/`SIGTERM`, draining in-flight requests (10s budget) and closing the serial port.

### Run as a service (Raspberry Pi / Linux)

For an unattended station, build on the Pi and install the bundled systemd unit. You need Go 1.26 or newer from <https://go.dev/dl/>; the `golang` package in Debian/Raspberry Pi OS is too old.
```bash
go build -o antenna-rotator-server .
sudo install -m 0755 antenna-rotator-server /usr/local/bin/
sudo install -m 0644 deploy/antenna-rotator-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo install -m 0644 deploy/antenna-rotator-server.env /etc/   # first install only: this is your config
```

Point `ROTATOR_PORT` at the adapter's stable name under `/dev/serial/by-id/`. `/dev/ttyUSB0` and friends are numbered in the order adapters appear, so they can swap between reboots:
```bash
ls -l /dev/serial/by-id/
# usb-FTDI_FT232R_USB_UART_A10KXYZ1-if00-port0 -> ../../ttyUSB0
sudo nano /etc/antenna-rotator-server.env   # set ROTATOR_PORT and ROTATOR_PROTOCOL
sudo systemctl enable --now antenna-rotator-server
journalctl -u antenna-rotator-server -f     # follow the logs
```

The unit ([`deploy/antenna-rotator-server.service`](deploy/antenna-rotator-server.service)):
- runs as an unprivileged throwaway user in the `dialout` group, so it can open serial ports but nothing else
- is sandboxed: read-only filesystem apart from its state directory, no capabilities, and a restricted set of system calls (check with `systemd-analyze security antenna-rotator-server`)
- restarts automatically if the process ever exits
- saves the calibration offset to `/var/lib/antenna-rotator-server/offset`, so it survives reboots

To upgrade:
```bash
git pull
go build -o antenna-rotator-server .
sudo install -m 0755 antenna-rotator-server /usr/local/bin/
sudo systemctl restart antenna-rotator-server
```
If an upgrade changes `deploy/antenna-rotator-server.service`, reinstall it and run `sudo systemctl daemon-reload` before restarting. Don't recopy the env file, or you'll overwrite your settings.

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

Canonical paths live under `/api/v1`; the unprefixed paths remain as aliases. All endpoints return JSON. Errors come back as `{"error": "..."}`:
- `400`: invalid input
- `502`: serial communication with the rotator failed, or the controller rejected the command
- `503`: the request gave up waiting for the serial port (after 10 seconds, or as soon as the client disconnects)

| Method | Path                  | Description                                          |
|--------|-----------------------|------------------------------------------------------|
| POST   | `/api/v1/set-heading` | Set the heading (`?heading=0..359` or JSON body)     |
| GET    | `/api/v1/get-heading` | Read the current heading                             |
| POST   | `/api/v1/stop`        | Stop in-progress rotation                            |
| GET    | `/api/v1/offset`      | Read the current calibration offset                  |
| POST   | `/api/v1/offset`      | Set the calibration offset (`?offset=N` or JSON body) |
| GET    | `/api/v1/list-ports`  | List serial ports detected on the host               |
| GET    | `/api/v1/healthz`     | Liveness probe with mode/connection/version info     |
| GET    | `/api/v1/readyz`      | Readiness probe: reads the heading, `503` if the rotator can't be reached |

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

curl -X POST 'http://localhost:8080/api/v1/offset?offset=15'
# {"offset":15,"status":"set"}

curl http://localhost:8080/api/v1/offset
# {"offset":15}

curl http://localhost:8080/api/v1/list-ports
# {"ports":["COM3","COM4"]}

curl http://localhost:8080/api/v1/healthz
# {"status":"ok","mode":"hardware","connected":true,"version":"1.2.0"}

curl http://localhost:8080/api/v1/readyz
# {"status":"ready","heading":180}
```

`healthz` always returns `200` while the process is alive, and `connected` only means the serial port is open. To monitor a remote station, poll `readyz` instead: it actually reads the heading, so it returns `503` with an `error` whenever the rotator isn't answering, whether the adapter is unplugged, the controller is powered off, or the cable is loose. Polling it also reconnects a dropped port.

### Calibration offset

If the rotator is mechanically mounted so its own zero point doesn't line up with true heading (a common real-world install error), set `ROTATOR_HEADING_OFFSET` or `POST /api/v1/offset` so requested headings point where you actually expect: `real-world heading = raw rotator heading + offset (mod 360)`. It's applied to every `set-heading`/`get-heading` call, in both hardware and simulation mode, and can be tuned live via the API while comparing the antenna's actual physical heading to what it reports — no server restart or hardware command required, since it's a pure software translation layer.

Set `ROTATOR_OFFSET_FILE` (the systemd unit does this for you) and every `POST /offset` is saved to that file and reloaded on startup, so calibration survives reboots and power cuts. Once a value has been saved it takes precedence over `ROTATOR_HEADING_OFFSET`; delete the file to fall back to the env var. If the file can't be written, `POST /offset` still applies the offset but returns `500` so you know it won't persist.

---

## Hardware support

The `rotator-controller` package speaks two serial protocols (both 8N1), selected with `ROTATOR_PROTOCOL`:

### Prosistel (`ROTATOR_PROTOCOL=prosistel`, the default)

Prosistel "D" protocol, matching [Hamlib's prosistel backend](https://github.com/Hamlib/Hamlib/tree/master/rotators/prosistel) — the reference implementation used across ham radio rotator software. Every command is framed with a leading STX (`\x02`) and addresses the azimuth axis by the literal letter `A` (there is no numeric bus address).

| Action          | Command sent       | Response                |
|-----------------|--------------------|--------------------------|
| Set azimuth     | `\x02AG<deg>\r`    | `\x02A,?,<deg>,R\r`      |
| Read azimuth    | `\x02A?\r`         | `\x02A,?,<deg>,R\r`      |
| Stop rotation   | `\x02AG997\r`      | `\x02A,?,<deg>,R\r`      |

The controller answers every command. The server waits for that reply after `set-heading` and `stop` too, so a command the controller rejects (`\x02A,?,E,<code>\r`) or never answers comes back as a `502` instead of a false success.

**Soft start and stop.** `POST /stop` sends `G997`, Prosistel's soft stop, rather than `G999`, which always stops dead. The ramping itself is done by the control box, and only if **PWM is enabled in its setup menu**. That same setting gives every `set-heading` its soft start. With PWM off, `G997` stops dead as well, and nothing in the protocol lets the server check the setting, so confirm it on the control box.

> ⚠️ Combi-Track controllers scale every angle by 10 (their soft stop is `G9777`). This protocol entry doesn't, so with a Combi-Track the stop command `G997` would be read as "go to 99.7°". Don't use it with one.

### Yaesu GS-232A/B (`ROTATOR_PROTOCOL=yaesu`)

Covers Yaesu G-450/G-650/G-800/G-1000/G-2800-series rotators attached via a GS-232 computer interface (or a compatible one such as the ERC or K3NG controllers in GS-232 mode).

| Action          | Command sent     | Response                              |
|-----------------|------------------|---------------------------------------|
| Set azimuth     | `MXXX\r`         | —                                     |
| Read azimuth    | `C\r`            | `+0XXX` (GS-232A) or `AZ=XXX` (GS-232B) |
| Stop rotation   | `S\r`            | —                                     |

Both response variants are recognised automatically, including the azimuth+elevation forms (`+0XXX+0YYY`, `AZ=XXX EL=YYY`) from az/el units — the elevation part is ignored. If your interface runs at a slow baud rate, set `ROTATOR_BAUD` accordingly.

`XXX` is always the zero-padded 3-digit azimuth in degrees. `GET /healthz` reports the active protocol.

GS-232 has no soft stop: `S` cuts the motor at once, so any ramping has to come from the control box itself.

### Adding another rotator

Protocols are defined as data, not code: each one is a `ProtocolSpec` entry in the registry in [`rotator-controller/protocols.go`](rotator-controller/protocols.go) — the command templates plus the markers that precede the azimuth in the response. The serial machinery (locking, reconnect, buffer flushing) is shared. To support a new make, add one entry to that table and a few cases to `protocols_test.go`.

Serial access is fully serialized (concurrent HTTP requests can't interleave commands), the input buffer is flushed before every command, and a dead port (e.g. unplugged USB adapter) is automatically reopened on the next request. Overlapping heading reads share one serial exchange, and a request waiting for the port gives up when its client disconnects or after 10 seconds, so heavy polling of a silent controller can't build a queue that delays a stop. Replies longer than 64 bytes or slower than 5 seconds are rejected as garbage (usually a baud rate mismatch).

---

## Development

```bash
go build -o antenna-rotator-server .   # build; the version comes from git automatically
go test -race ./...                    # tests with the race detector
go vet ./...
golangci-lint run                      # lint (CI uses v2.12.2)
SIMULATION=true go run .               # run against the simulated rotator
```

`/healthz` reports the version Go records at build time: the tag (e.g. `v1.0.0`) when you build a tagged commit, otherwise the commit, plus `+dirty` if you have uncommitted changes. `go run` reports `dev`.

CI (GitHub Actions) runs vet, the race-enabled test suite, and golangci-lint on every push and pull request.

## Project layout
```
.
├── main.go                  # entry point: config, logging, graceful shutdown
├── offsetfile.go            # saves/loads the calibration offset
├── rotator-server/          # HTTP server, Swagger UI, OpenAPI spec
│   └── static/              # embedded swagger.html + openapi.yaml
├── rotator-controller/      # serial-port rotator driver (Prosistel, Yaesu GS-232)
├── deploy/                  # systemd unit + example env file
└── .github/workflows/       # CI: vet, race tests, lint
```
