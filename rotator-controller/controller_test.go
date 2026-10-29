package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"
)

func TestSimulationMode(t *testing.T) {
	rc := NewSimulationController()

	if rc.Mode() != "simulation" {
		t.Errorf("Mode(): got %q, want %q", rc.Mode(), "simulation")
	}
	if rc.Protocol() != "simulation" {
		t.Errorf("Protocol(): got %q, want %q", rc.Protocol(), "simulation")
	}
	if !rc.Connected() {
		t.Error("Connected(): simulation controller should report true")
	}

	// Default heading before any set
	h, err := rc.GetHeading(t.Context())
	if err != nil {
		t.Fatalf("GetHeading error: %v", err)
	}
	if h != 0 {
		t.Errorf("default heading: got %d, want 0", h)
	}

	if err := rc.SetHeading(t.Context(), 270); err != nil {
		t.Fatalf("SetHeading error: %v", err)
	}
	h, err = rc.GetHeading(t.Context())
	if err != nil {
		t.Fatalf("GetHeading error: %v", err)
	}
	if h != 270 {
		t.Errorf("after SetHeading(270): got %d, want 270", h)
	}

	for _, deg := range []int{-1, 360, 1000} {
		err := rc.SetHeading(t.Context(), deg)
		if err == nil {
			t.Errorf("SetHeading(%d): expected error for out-of-range heading", deg)
		}
	}
}

func TestHeadingOffsetRoundTrips(t *testing.T) {
	rc := NewSimulationController()
	rc.SetOffset(10)
	if got := rc.Offset(); got != 10 {
		t.Errorf("Offset(): got %d, want 10", got)
	}

	if err := rc.SetHeading(t.Context(), 100); err != nil {
		t.Fatalf("SetHeading: %v", err)
	}
	h, err := rc.GetHeading(t.Context())
	if err != nil {
		t.Fatalf("GetHeading: %v", err)
	}
	if h != 100 {
		t.Errorf("GetHeading with offset applied: got %d, want 100 (real-world heading should round-trip)", h)
	}
}

func TestSetOffsetNormalisesModulo360(t *testing.T) {
	rc := NewSimulationController()
	rc.SetOffset(-10)
	if got := rc.Offset(); got != 350 {
		t.Errorf("Offset() after SetOffset(-10): got %d, want 350", got)
	}
	rc.SetOffset(370)
	if got := rc.Offset(); got != 10 {
		t.Errorf("Offset() after SetOffset(370): got %d, want 10", got)
	}
}

func TestSimulationConcurrentAccess(t *testing.T) {
	rc := NewSimulationController()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(deg int) {
			defer wg.Done()
			if err := rc.SetHeading(t.Context(), deg%360); err != nil {
				t.Errorf("SetHeading: %v", err)
			}
		}(i * 30)
		go func() {
			defer wg.Done()
			if _, err := rc.GetHeading(t.Context()); err != nil {
				t.Errorf("GetHeading: %v", err)
			}
		}()
	}
	wg.Wait()
}

// fakePort is a scripted in-memory serial.Port. Embedding the interface
// keeps it compiling if go.bug.st/serial grows methods we don't use.
type fakePort struct {
	serial.Port

	mu        sync.Mutex
	written   []byte
	responses map[string][]byte // full command → reply queued when written
	pending   []byte            // bytes available to Read
	failNext  int               // fail this many Writes before succeeding
	closed    bool
}

func newFakePort() *fakePort {
	return &fakePort{responses: map[string][]byte{}}
}

func (f *fakePort) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext > 0 {
		f.failNext--
		return 0, fmt.Errorf("fake write failure")
	}
	f.written = append(f.written, p...)
	if reply, ok := f.responses[string(p)]; ok {
		f.pending = append(f.pending, reply...)
	}
	return len(p), nil
}

func (f *fakePort) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) == 0 {
		return 0, nil // mimics the serial read timeout firing with no data
	}
	n := copy(p, f.pending)
	f.pending = f.pending[n:]
	return n, nil
}

func (f *fakePort) ResetInputBuffer() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = nil
	return nil
}

func (f *fakePort) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakePort) writtenString() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.written)
}

// mustProtocol resolves a registry protocol or panics; for test wiring only.
func mustProtocol(name string) ProtocolSpec {
	p, err := protocolByName(name)
	if err != nil {
		panic(err)
	}
	return p
}

// newFakeController wires a controller for the named protocol to the given
// fake port; the port is opened lazily on the first operation.
func newFakeController(protocolName string, fp serial.Port) *RotatorController {
	return newHardwareController("FAKE", mustProtocol(protocolName), func(string) (serial.Port, error) { return fp, nil })
}

func TestSetHeadingWritesCommand(t *testing.T) {
	fp := newFakePort()
	fp.responses["\x02AG90\r"] = []byte("\x02A,?,045,R\r")
	rc := newFakeController("prosistel", fp)
	if err := rc.SetHeading(t.Context(), 90); err != nil {
		t.Fatalf("SetHeading: %v", err)
	}
	if got := fp.writtenString(); got != "\x02AG90\r" {
		t.Errorf("wrote %q, want %q", got, "\x02AG90\r")
	}
	if err := rc.SetHeading(t.Context(), 400); err == nil {
		t.Error("SetHeading(400): expected validation error")
	}
}

func TestSetHeadingSubtractsOffsetFromRawCommand(t *testing.T) {
	fp := newFakePort()
	fp.responses["\x02AG355\r"] = []byte("\x02A,?,045,R\r")
	rc := newFakeController("prosistel", fp)
	rc.SetOffset(10)
	// Desired real-world heading 5; raw command sent to the rotator should
	// be mod360(5-10) = 355, wrapping around zero.
	if err := rc.SetHeading(t.Context(), 5); err != nil {
		t.Fatalf("SetHeading: %v", err)
	}
	if got := fp.writtenString(); got != "\x02AG355\r" {
		t.Errorf("wrote %q, want %q", got, "\x02AG355\r")
	}
}

func TestGetHeadingAddsOffsetToRawReading(t *testing.T) {
	fp := newFakePort()
	fp.responses["\x02A?\r"] = []byte("\x02A,?,350,R\r")
	rc := newFakeController("prosistel", fp)
	rc.SetOffset(10)
	h, err := rc.GetHeading(t.Context())
	if err != nil {
		t.Fatalf("GetHeading: %v", err)
	}
	if h != 0 { // mod360(350+10) = 0
		t.Errorf("got %d, want 0", h)
	}
}

func TestGetHeadingParsesResponse(t *testing.T) {
	fp := newFakePort()
	fp.responses["\x02A?\r"] = []byte("\x02A,?,123,R\r")
	rc := newFakeController("prosistel", fp)
	h, err := rc.GetHeading(t.Context())
	if err != nil {
		t.Fatalf("GetHeading: %v", err)
	}
	if h != 123 {
		t.Errorf("got %d, want 123", h)
	}
}

func TestGetHeadingFlushesStaleBytes(t *testing.T) {
	fp := newFakePort()
	fp.pending = []byte("\x02A,?,007,R\r") // leftover reply from an earlier command
	fp.responses["\x02A?\r"] = []byte("\x02A,?,123,R\r")
	rc := newFakeController("prosistel", fp)
	h, err := rc.GetHeading(t.Context())
	if err != nil {
		t.Fatalf("GetHeading: %v", err)
	}
	if h != 123 {
		t.Errorf("got %d, want 123 (stale bytes should have been flushed)", h)
	}
}

func TestGetHeadingTimeout(t *testing.T) {
	fp := newFakePort() // no scripted response → read timeout
	rc := newFakeController("prosistel", fp)
	_, err := rc.GetHeading(t.Context())
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestStopSendsSoftStop(t *testing.T) {
	fp := newFakePort()
	fp.responses["\x02AG997\r"] = []byte("\x02A,?,045,R\r")
	rc := newFakeController("prosistel", fp)
	if err := rc.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// 997 ramps the motor down; 999 would stop it dead.
	if got := fp.writtenString(); got != "\x02AG997\r" {
		t.Errorf("wrote %q, want %q", got, "\x02AG997\r")
	}
}

func TestReconnectAfterPortFailure(t *testing.T) {
	dead := newFakePort()
	dead.failNext = 1000 // port has gone away; every write fails
	replacement := newFakePort()
	replacement.responses["\x02A?\r"] = []byte("\x02A,?,045,R\r")

	opened := 0
	rc := newHardwareController("FAKE", mustProtocol("prosistel"), func(string) (serial.Port, error) {
		opened++
		if opened == 1 {
			return dead, nil
		}
		return replacement, nil
	})

	h, err := rc.GetHeading(t.Context())
	if err != nil {
		t.Fatalf("GetHeading should succeed after reconnect: %v", err)
	}
	if h != 45 {
		t.Errorf("got %d, want 45", h)
	}
	if !dead.closed {
		t.Error("dead port should have been closed on failure")
	}
	if opened != 2 {
		t.Errorf("expected 2 open attempts, got %d", opened)
	}
	if !rc.Connected() {
		t.Error("controller should report connected after successful reconnect")
	}
}

func TestErrorWhenReconnectAlsoFails(t *testing.T) {
	dead := newFakePort()
	dead.failNext = 1000

	opened := 0
	rc := newHardwareController("FAKE", mustProtocol("prosistel"), func(string) (serial.Port, error) {
		opened++
		if opened == 1 {
			return dead, nil
		}
		return nil, fmt.Errorf("device gone")
	})

	if _, err := rc.GetHeading(t.Context()); err == nil {
		t.Fatal("expected error when port and reconnect both fail")
	}
	if rc.Connected() {
		t.Error("controller should report disconnected after failed reconnect")
	}
}

func TestNewRotatorControllerRejectsUnknownProtocol(t *testing.T) {
	if _, err := NewRotatorController("FAKE", Options{Protocol: "nope"}); err == nil {
		t.Fatal("expected error for unsupported protocol")
	}
}

func TestStartsDisconnectedAndConnectsWhenPortAppears(t *testing.T) {
	fp := newFakePort()
	fp.responses["\x02A?\r"] = []byte("\x02A,?,090,R\r")

	present := false
	rc := newHardwareController("FAKE", mustProtocol("prosistel"), func(string) (serial.Port, error) {
		if !present {
			return nil, fmt.Errorf("no such device")
		}
		return fp, nil
	})

	if err := rc.Connect(); err == nil {
		t.Fatal("Connect: expected error while the port is missing")
	}
	if rc.Connected() {
		t.Error("controller should report disconnected while the port is missing")
	}
	if _, err := rc.GetHeading(t.Context()); err == nil {
		t.Fatal("GetHeading: expected error while the port is missing")
	}

	present = true // USB adapter finishes enumerating
	h, err := rc.GetHeading(t.Context())
	if err != nil {
		t.Fatalf("GetHeading should succeed once the port appears: %v", err)
	}
	if h != 90 {
		t.Errorf("got %d, want 90", h)
	}
	if !rc.Connected() {
		t.Error("controller should report connected once the port appears")
	}
	if err := rc.Connect(); err != nil {
		t.Errorf("Connect on an open port should be a no-op: %v", err)
	}
}

func TestSilentControllerIsNotRetried(t *testing.T) {
	fp := newFakePort() // port works, but nothing ever answers
	opened := 0
	rc := newHardwareController("FAKE", mustProtocol("prosistel"), func(string) (serial.Port, error) {
		opened++
		return fp, nil
	})

	for i := 0; i < 2; i++ {
		if _, err := rc.GetHeading(t.Context()); !errors.Is(err, errNoResponse) {
			t.Fatalf("GetHeading #%d: got %v, want errNoResponse", i+1, err)
		}
	}
	if got := strings.Count(fp.writtenString(), "\x02A?\r"); got != 2 {
		t.Errorf("sent the query %d times for 2 requests, want 2 (a timeout must not be retried)", got)
	}
	if opened != 1 {
		t.Errorf("opened the port %d times, want 1 (a timeout must not reopen it)", opened)
	}
	if !rc.Connected() {
		t.Error("controller should still report connected: the port itself is fine")
	}
}

func TestConnectedDoesNotWaitForLock(t *testing.T) {
	rc := newFakeController("prosistel", newFakePort())
	if err := rc.Connect(); err != nil {
		t.Fatal(err)
	}
	rc.acquire() // a slow serial exchange is in progress
	defer rc.release()

	done := make(chan bool)
	go func() { done <- rc.Connected() }()
	select {
	case ok := <-done:
		if !ok {
			t.Error("Connected: got false, want true")
		}
	case <-time.After(time.Second):
		t.Fatal("Connected blocked while the lock was held")
	}
}

func TestWaitingForPortGivesUpWhenContextEnds(t *testing.T) {
	fp := newFakePort()
	rc := newFakeController("prosistel", fp)
	rc.acquire() // a slow serial exchange is in progress
	defer rc.release()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := rc.SetHeading(ctx, 90); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("SetHeading: got %v, want context.DeadlineExceeded", err)
	}
	if err := rc.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Stop: got %v, want context.DeadlineExceeded", err)
	}
	if _, err := rc.GetHeading(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("GetHeading: got %v, want context.DeadlineExceeded", err)
	}
	if got := fp.writtenString(); got != "" {
		t.Errorf("wrote %q after giving up, want nothing", got)
	}
}

func TestOverlappingReadsShareOneExchange(t *testing.T) {
	fp := newFakePort()
	fp.responses["\x02A?\r"] = []byte("\x02A,?,123,R\r")
	rc := newFakeController("prosistel", fp)

	rc.acquire() // hold the port so the reads overlap
	first := rc.sharedRead()
	second := rc.sharedRead()
	if first != second {
		t.Fatal("a read that overlaps one in progress should share it")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // this caller gives up; the shared read must still complete
	if _, err := rc.GetHeading(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetHeading with a cancelled context: got %v, want context.Canceled", err)
	}
	rc.release()

	<-first.done
	if first.err != nil || first.raw != 123 {
		t.Fatalf("shared read: got %d, %v; want 123", first.raw, first.err)
	}
	if got := strings.Count(fp.writtenString(), "\x02A?\r"); got != 1 {
		t.Errorf("sent the query %d times for overlapping reads, want 1", got)
	}

	if _, err := rc.GetHeading(t.Context()); err != nil {
		t.Fatalf("GetHeading after the shared read: %v", err)
	}
	if got := strings.Count(fp.writtenString(), "\x02A?\r"); got != 2 {
		t.Errorf("a read after the shared one finished should query again; sent %d queries, want 2", got)
	}
}

func TestRejectedCommandIsReported(t *testing.T) {
	fp := newFakePort()
	fp.responses["\x02AG90\r"] = []byte("\x02A,?,E,00003\r")
	rc := newFakeController("prosistel", fp)
	if err := rc.SetHeading(t.Context(), 90); !errors.Is(err, errRejected) {
		t.Errorf("SetHeading: got %v, want errRejected", err)
	}
}

func TestUnacknowledgedCommandIsReported(t *testing.T) {
	fp := newFakePort() // the controller never answers
	rc := newFakeController("prosistel", fp)
	if err := rc.SetHeading(t.Context(), 90); !errors.Is(err, errNoResponse) {
		t.Errorf("SetHeading: got %v, want errNoResponse", err)
	}
	if err := rc.Stop(t.Context()); !errors.Is(err, errNoResponse) {
		t.Errorf("Stop: got %v, want errNoResponse", err)
	}
}

// noisePort returns the same byte forever, like a line at the wrong baud
// rate.
type noisePort struct{ fakePort }

func (*noisePort) Read(p []byte) (int, error) {
	p[0] = 'x'
	return 1, nil
}

func TestEndlessReplyIsRejected(t *testing.T) {
	np := &noisePort{}
	opened := 0
	rc := newHardwareController("FAKE", mustProtocol("prosistel"), func(string) (serial.Port, error) {
		opened++
		return np, nil
	})
	if _, err := rc.GetHeading(t.Context()); !errors.Is(err, errBadReply) {
		t.Fatalf("GetHeading: got %v, want errBadReply", err)
	}
	if opened != 1 {
		t.Errorf("opened the port %d times, want 1 (garbage must not trigger a reconnect)", opened)
	}
}

func TestSlowReplyIsRejected(t *testing.T) {
	fp := newFakePort()
	fp.pending = []byte("\x02A,?,123,R\r")
	if _, err := readLine(fp, time.Now().Add(-time.Second)); !errors.Is(err, errBadReply) {
		t.Errorf("readLine past its deadline: got %v, want errBadReply", err)
	}
}

func TestClosedControllerDoesNotReopenPort(t *testing.T) {
	opened := 0
	rc := newHardwareController("FAKE", mustProtocol("prosistel"), func(string) (serial.Port, error) {
		opened++
		return newFakePort(), nil
	})
	rc.Close()
	if _, err := rc.GetHeading(t.Context()); !errors.Is(err, errClosed) {
		t.Errorf("GetHeading after Close: got %v, want errClosed", err)
	}
	if opened != 0 {
		t.Errorf("opened the port %d times after Close, want 0", opened)
	}
}
