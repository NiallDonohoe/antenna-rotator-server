package controller

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

const (
	defaultBaudRate = 9600
	readTimeout     = 2 * time.Second
)

// ErrInvalidHeading marks a caller error (heading out of range). Callers can
// use errors.Is to distinguish it from serial I/O failures.
var ErrInvalidHeading = errors.New("invalid heading")

// Options configures a hardware controller.
type Options struct {
	Protocol string // a ProtocolSpec name or alias; defaults to the first registry entry
	BaudRate int    // defaults to 9600
}

// RotatorController drives a serial-attached rotator, or an in-memory
// simulated rotator when created with NewSimulationController.
//
// All methods are safe for concurrent use: a mutex serialises access to the
// port so overlapping HTTP requests cannot interleave commands or steal each
// other's responses.
//
// If the port dies mid-operation (e.g. USB unplugged), the controller drops
// it and transparently reopens it on the next operation.
type RotatorController struct {
	mu        sync.Mutex
	simulated bool
	portName  string
	port      serial.Port // nil when disconnected (hardware mode) or simulated
	heading   int          // simulation mode only; otherwise the last-read raw value is not cached
	offset    int          // calibration offset: real-world heading = raw rotator heading + offset
	proto     ProtocolSpec // zero value in simulation mode

	// open is swappable so tests can inject a fake port.
	open func(name string) (serial.Port, error)
}

func ListAvailablePorts() ([]string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, fmt.Errorf("could not list serial ports: %v", err)
	}
	return ports, nil
}

// NewSimulationController returns a controller with no underlying serial
// port. SetHeading/GetHeading/Stop operate against an in-memory heading,
// which is useful for local development, CI, and the Swagger UI demo.
func NewSimulationController() *RotatorController {
	return &RotatorController{simulated: true}
}

// NewRotatorControllerWithPort opens the named serial port immediately so a
// misconfigured or missing device fails at startup rather than on first use.
func NewRotatorControllerWithPort(portName string, opts Options) (*RotatorController, error) {
	proto, err := protocolByName(opts.Protocol)
	if err != nil {
		return nil, err
	}
	baud := opts.BaudRate
	if baud == 0 {
		baud = defaultBaudRate
	}
	rc := &RotatorController{
		portName: portName,
		proto:    proto,
		open: func(name string) (serial.Port, error) {
			return openSerialPort(name, baud)
		},
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if err := rc.connectLocked(); err != nil {
		return nil, err
	}
	return rc, nil
}

func openSerialPort(portName string, baud int) (serial.Port, error) {
	mode := &serial.Mode{
		BaudRate: baud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	p, err := serial.Open(portName, mode)
	if err != nil {
		return nil, err
	}
	if err := p.SetReadTimeout(readTimeout); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("could not set read timeout: %v", err)
	}
	return p, nil
}

// Mode reports "simulation" or "hardware".
func (rc *RotatorController) Mode() string {
	if rc.simulated {
		return "simulation"
	}
	return "hardware"
}

// Protocol reports the wire protocol in use (e.g. "prosistel",
// "yaesu-gs232"), or "simulation" for a simulated controller.
func (rc *RotatorController) Protocol() string {
	if rc.simulated {
		return "simulation"
	}
	return rc.proto.Name
}

// Connected reports whether the serial port is currently open. Simulation
// controllers always report true.
func (rc *RotatorController) Connected() bool {
	if rc.simulated {
		return true
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.port != nil
}

// SetHeading rotates to the given real-world heading (0–359 degrees). The
// calibration offset (see SetOffset) is subtracted before the raw command is
// sent, so the rotator ends up pointing at the requested compass heading
// even if its own zero point is mechanically misaligned.
func (rc *RotatorController) SetHeading(deg int) error {
	if deg < 0 || deg > 359 {
		return fmt.Errorf("%w: %d out of range, must be 0–359", ErrInvalidHeading, deg)
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	raw := mod360(deg - rc.offset)
	if rc.simulated {
		rc.heading = raw
		return nil
	}
	if _, err := rc.transactLocked(rc.proto.setHeadingCmd(raw), false); err != nil {
		return fmt.Errorf("failed to send set-heading command: %v", err)
	}
	return nil
}

// GetHeading queries the controller for the current azimuth and applies the
// calibration offset to report a real-world heading.
func (rc *RotatorController) GetHeading() (int, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.simulated {
		return mod360(rc.heading + rc.offset), nil
	}
	resp, err := rc.transactLocked(rc.proto.GetHeadingCmd, true)
	if err != nil {
		return 0, fmt.Errorf("failed to read heading: %v", err)
	}
	raw, err := rc.proto.parseHeading(resp)
	if err != nil {
		return 0, err
	}
	return mod360(raw + rc.offset), nil
}

// Offset reports the calibration offset currently applied.
func (rc *RotatorController) Offset() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.offset
}

// SetOffset sets the calibration offset applied to all subsequent
// SetHeading/GetHeading calls: real-world heading = raw rotator heading +
// offset. Any integer is accepted and normalised modulo 360, so it can be
// tuned live (e.g. via the HTTP API) while comparing the antenna's actual
// physical heading to what the rotator reports.
func (rc *RotatorController) SetOffset(offset int) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.offset = mod360(offset)
}

// mod360 normalises deg into the range [0, 360).
func mod360(deg int) int {
	deg %= 360
	if deg < 0 {
		deg += 360
	}
	return deg
}

// Stop halts any in-progress rotation.
func (rc *RotatorController) Stop() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.simulated {
		return nil
	}
	if _, err := rc.transactLocked(rc.proto.StopCmd, false); err != nil {
		return fmt.Errorf("failed to send stop command: %v", err)
	}
	return nil
}

func (rc *RotatorController) Close() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.dropPortLocked()
}

// connectLocked opens the configured port. Callers must hold rc.mu.
func (rc *RotatorController) connectLocked() error {
	p, err := rc.open(rc.portName)
	if err != nil {
		return fmt.Errorf("could not open serial port %s: %v", rc.portName, err)
	}
	rc.port = p
	return nil
}

func (rc *RotatorController) dropPortLocked() {
	if rc.port != nil {
		_ = rc.port.Close()
		rc.port = nil
	}
}

// transactLocked sends one command and, when expectReply is set, reads one
// CR/LF-terminated response line. On an I/O failure it drops the port,
// reopens it and retries once, so a replugged USB adapter recovers without a
// server restart. Callers must hold rc.mu.
func (rc *RotatorController) transactLocked(cmd string, expectReply bool) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if rc.port == nil {
			if err := rc.connectLocked(); err != nil {
				if lastErr != nil {
					return "", fmt.Errorf("%v (reconnect failed: %v)", lastErr, err)
				}
				return "", err
			}
		}
		resp, err := rc.tryTransactLocked(cmd, expectReply)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		rc.dropPortLocked()
	}
	return "", lastErr
}

func (rc *RotatorController) tryTransactLocked(cmd string, expectReply bool) (string, error) {
	// Discard any stale bytes (e.g. a late response to a command that timed
	// out) so we never parse a previous command's reply.
	if err := rc.port.ResetInputBuffer(); err != nil {
		return "", fmt.Errorf("could not flush input buffer: %v", err)
	}
	if _, err := rc.port.Write([]byte(cmd)); err != nil {
		return "", fmt.Errorf("serial write failed: %v", err)
	}
	if !expectReply {
		return "", nil
	}
	return readLine(rc.port)
}

// readLine reads from the port one byte at a time until CR/LF or the port
// read timeout fires with no bytes (indicating no more data).
func readLine(p serial.Port) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := p.Read(buf)
		if n > 0 {
			if buf[0] == '\r' || buf[0] == '\n' {
				break
			}
			sb.WriteByte(buf[0])
		}
		if err != nil {
			return "", err
		}
		// n == 0, err == nil means the read timeout fired with no data.
		if n == 0 {
			if sb.Len() == 0 {
				return "", fmt.Errorf("timeout: no response from rotator")
			}
			break
		}
	}
	return sb.String(), nil
}
