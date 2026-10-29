package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.bug.st/serial"
)

const (
	defaultBaudRate = 9600
	readTimeout     = 2 * time.Second

	// A reply is one short line; anything longer, or slower, is line noise
	// or a baud rate mismatch, and must not hold the port indefinitely.
	maxReplyLen   = 64
	replyDeadline = 5 * time.Second
)

// ErrInvalidHeading marks a caller error (heading out of range). Callers can
// use errors.Is to distinguish it from serial I/O failures.
var ErrInvalidHeading = errors.New("invalid heading")

// errNoResponse means the port is fine but the controller didn't answer
// (powered off, cable loose). Reopening the port can't fix that.
var errNoResponse = errors.New("timeout: no response from rotator")

// errBadReply means the controller sent something that isn't a reply line
// (wrong baud rate, line noise). Reopening the port can't fix that either.
var errBadReply = errors.New("garbled reply from rotator")

var errClosed = errors.New("rotator controller is closed")

// Options configures a hardware controller.
type Options struct {
	Protocol string // a ProtocolSpec name or alias; defaults to the first registry entry
	BaudRate int    // defaults to 9600
}

// RotatorController drives a serial-attached rotator, or an in-memory
// simulated rotator when created with NewSimulationController.
//
// All methods are safe for concurrent use: a lock serialises access to the
// port so overlapping HTTP requests cannot interleave commands or steal each
// other's responses. Waiting for the lock ends when the caller's context
// does, so requests whose clients have gone away never pile up behind a
// slow or silent controller, and concurrent heading reads share a single
// serial exchange. Together these keep a stop from queueing behind a
// backlog of reads.
//
// If the port dies mid-operation (e.g. USB unplugged), the controller drops
// it and transparently reopens it on the next operation.
type RotatorController struct {
	// lock is a one-slot semaphore guarding port, heading and closed. It is
	// a channel rather than a sync.Mutex so waiting can be cancelled.
	lock      chan struct{}
	simulated bool
	portName  string
	port      serial.Port  // nil when disconnected (hardware mode) or simulated
	connected atomic.Bool  // mirrors port != nil so Connected never waits on lock
	closed    bool         // set by Close; stops a late operation reopening the port
	heading   int          // simulation mode only; otherwise the last-read raw value is not cached
	offset    atomic.Int64 // calibration offset: real-world heading = raw rotator heading + offset
	proto     ProtocolSpec // zero value in simulation mode

	readMu sync.Mutex
	read   *headingRead // the heading read in progress, if any; guarded by readMu

	// open is swappable so tests can inject a fake port.
	open func(name string) (serial.Port, error)
}

// headingRead is one serial heading read, shared by every GetHeading call
// that arrives while it is in progress.
type headingRead struct {
	done chan struct{} // closed once raw and err are set
	raw  int
	err  error
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
	return &RotatorController{lock: make(chan struct{}, 1), simulated: true}
}

// NewRotatorController validates the options and returns a hardware
// controller for the named serial port. It does not open the port: call
// Connect to try it up front, or let the first operation open it.
func NewRotatorController(portName string, opts Options) (*RotatorController, error) {
	proto, err := protocolByName(opts.Protocol)
	if err != nil {
		return nil, err
	}
	baud := opts.BaudRate
	if baud == 0 {
		baud = defaultBaudRate
	}
	return newHardwareController(portName, proto, func(name string) (serial.Port, error) {
		return openSerialPort(name, baud)
	}), nil
}

func newHardwareController(portName string, proto ProtocolSpec, open func(string) (serial.Port, error)) *RotatorController {
	return &RotatorController{
		lock:     make(chan struct{}, 1),
		portName: portName,
		proto:    proto,
		open:     open,
	}
}

// Connect opens the serial port if it is not already open. A failure is not
// fatal: every operation reopens the port on demand, so a USB adapter that
// enumerates after the server starts (e.g. after a power cut) is picked up
// on the next request.
func (rc *RotatorController) Connect() error {
	if rc.simulated {
		return nil
	}
	rc.acquire()
	defer rc.release()
	if rc.port != nil {
		return nil
	}
	return rc.connectLocked()
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

// acquire takes the lock, waiting as long as it takes.
func (rc *RotatorController) acquire() {
	rc.lock <- struct{}{}
}

// acquireCtx takes the lock, or gives up with the context's error once ctx
// is done.
func (rc *RotatorController) acquireCtx(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case rc.lock <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (rc *RotatorController) release() {
	<-rc.lock
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
// controllers always report true. It doesn't take the lock, so health checks
// answer immediately even while a slow serial exchange is in progress.
func (rc *RotatorController) Connected() bool {
	if rc.simulated {
		return true
	}
	return rc.connected.Load()
}

// SetHeading rotates to the given real-world heading (0–359 degrees). The
// calibration offset (see SetOffset) is subtracted before the raw command is
// sent, so the rotator ends up pointing at the requested compass heading
// even if its own zero point is mechanically misaligned.
func (rc *RotatorController) SetHeading(ctx context.Context, deg int) error {
	if deg < 0 || deg > 359 {
		return fmt.Errorf("%w: %d out of range, must be 0–359", ErrInvalidHeading, deg)
	}
	if err := rc.acquireCtx(ctx); err != nil {
		return err
	}
	defer rc.release()
	raw := mod360(deg - rc.Offset())
	if rc.simulated {
		rc.heading = raw
		return nil
	}
	if err := rc.commandLocked(rc.proto.setHeadingCmd(raw)); err != nil {
		return fmt.Errorf("failed to send set-heading command: %w", err)
	}
	return nil
}

// GetHeading queries the controller for the current azimuth and applies the
// calibration offset to report a real-world heading. Calls that overlap
// share one serial exchange, so frequent polling can't build a queue.
func (rc *RotatorController) GetHeading(ctx context.Context) (int, error) {
	if rc.simulated {
		if err := rc.acquireCtx(ctx); err != nil {
			return 0, err
		}
		raw := rc.heading
		rc.release()
		return mod360(raw + rc.Offset()), nil
	}
	r := rc.sharedRead()
	select {
	case <-r.done:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	if r.err != nil {
		return 0, r.err
	}
	return mod360(r.raw + rc.Offset()), nil
}

// sharedRead returns the heading read in progress, starting one if there
// is none. The read runs on its own goroutine so it completes for the other
// callers sharing it even if the caller that started it gives up.
func (rc *RotatorController) sharedRead() *headingRead {
	rc.readMu.Lock()
	defer rc.readMu.Unlock()
	if rc.read != nil {
		return rc.read
	}
	r := &headingRead{done: make(chan struct{})}
	rc.read = r
	go func() {
		r.raw, r.err = rc.readHeading()
		rc.readMu.Lock()
		rc.read = nil
		rc.readMu.Unlock()
		close(r.done)
	}()
	return r
}

// readHeading performs one serial heading read and returns the raw value.
func (rc *RotatorController) readHeading() (int, error) {
	rc.acquire()
	defer rc.release()
	resp, err := rc.transactLocked(rc.proto.GetHeadingCmd, true)
	if err != nil {
		return 0, fmt.Errorf("failed to read heading: %w", err)
	}
	return rc.proto.parseHeading(resp)
}

// Offset reports the calibration offset currently applied.
func (rc *RotatorController) Offset() int {
	return int(rc.offset.Load())
}

// SetOffset sets the calibration offset applied to all subsequent
// SetHeading/GetHeading calls: real-world heading = raw rotator heading +
// offset. Any integer is accepted and normalised modulo 360, so it can be
// tuned live (e.g. via the HTTP API) while comparing the antenna's actual
// physical heading to what the rotator reports.
func (rc *RotatorController) SetOffset(offset int) {
	rc.offset.Store(int64(mod360(offset)))
}

// mod360 normalises deg into the range [0, 360).
func mod360(deg int) int {
	deg %= 360
	if deg < 0 {
		deg += 360
	}
	return deg
}

// Stop halts any in-progress rotation, using the protocol's soft stop where
// it has one (see the protocol registry).
func (rc *RotatorController) Stop(ctx context.Context) error {
	if err := rc.acquireCtx(ctx); err != nil {
		return err
	}
	defer rc.release()
	if rc.simulated {
		return nil
	}
	if err := rc.commandLocked(rc.proto.StopCmd); err != nil {
		return fmt.Errorf("failed to send stop command: %w", err)
	}
	return nil
}

// Close closes the serial port. Operations after Close fail rather than
// reopening it.
func (rc *RotatorController) Close() {
	rc.acquire()
	defer rc.release()
	rc.closed = true
	rc.dropPortLocked()
}

// connectLocked opens the configured port. Callers must hold the lock.
func (rc *RotatorController) connectLocked() error {
	if rc.closed {
		return errClosed
	}
	p, err := rc.open(rc.portName)
	if err != nil {
		return fmt.Errorf("could not open serial port %s: %v", rc.portName, err)
	}
	rc.port = p
	rc.connected.Store(true)
	return nil
}

func (rc *RotatorController) dropPortLocked() {
	if rc.port != nil {
		_ = rc.port.Close()
		rc.port = nil
	}
	rc.connected.Store(false)
}

// commandLocked sends a set-heading or stop command. For protocols that
// acknowledge commands it waits for the reply and checks it, so a command
// the controller rejects or never answers is reported as a failure.
// Callers must hold the lock.
func (rc *RotatorController) commandLocked(cmd string) error {
	resp, err := rc.transactLocked(cmd, rc.proto.Acknowledges)
	if err != nil {
		return err
	}
	return rc.proto.checkReply(resp)
}

// transactLocked sends one command and, when expectReply is set, reads one
// CR/LF-terminated response line. On an I/O failure it drops the port,
// reopens it and retries once, so a replugged USB adapter recovers without a
// server restart. A controller that doesn't answer, or answers with
// garbage, is reported straight away: the port is healthy, so retrying
// would only double the wait (an unplugged adapter shows up as an I/O
// error, not a silent timeout). Callers must hold the lock.
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
		if errors.Is(err, errNoResponse) || errors.Is(err, errBadReply) {
			return "", err
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
	return readLine(rc.port, time.Now().Add(replyDeadline))
}

// readLine reads from the port one byte at a time until CR/LF or the port
// read timeout fires with no bytes (indicating no more data). A line longer
// than maxReplyLen, or still arriving at deadline, is rejected as garbage.
func readLine(p serial.Port, deadline time.Time) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := p.Read(buf)
		if n > 0 {
			if buf[0] == '\r' || buf[0] == '\n' {
				break
			}
			if sb.Len() >= maxReplyLen {
				return "", fmt.Errorf("%w: no line end within %d bytes (check the baud rate)", errBadReply, maxReplyLen)
			}
			sb.WriteByte(buf[0])
		}
		if err != nil {
			return "", err
		}
		// n == 0, err == nil means the read timeout fired with no data.
		if n == 0 {
			if sb.Len() == 0 {
				return "", errNoResponse
			}
			break
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%w: reply still incomplete after %v", errBadReply, replyDeadline)
		}
	}
	return sb.String(), nil
}
