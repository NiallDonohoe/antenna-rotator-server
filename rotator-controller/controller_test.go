package controller

import (
	"fmt"
	"strings"
	"sync"
	"testing"

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
	h, err := rc.GetHeading()
	if err != nil {
		t.Fatalf("GetHeading error: %v", err)
	}
	if h != 0 {
		t.Errorf("default heading: got %d, want 0", h)
	}

	if err := rc.SetHeading(270); err != nil {
		t.Fatalf("SetHeading error: %v", err)
	}
	h, err = rc.GetHeading()
	if err != nil {
		t.Fatalf("GetHeading error: %v", err)
	}
	if h != 270 {
		t.Errorf("after SetHeading(270): got %d, want 270", h)
	}

	for _, deg := range []int{-1, 360, 1000} {
		err := rc.SetHeading(deg)
		if err == nil {
			t.Errorf("SetHeading(%d): expected error for out-of-range heading", deg)
		}
	}
}

func TestSimulationConcurrentAccess(t *testing.T) {
	rc := NewSimulationController()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(deg int) {
			defer wg.Done()
			if err := rc.SetHeading(deg % 360); err != nil {
				t.Errorf("SetHeading: %v", err)
			}
		}(i * 30)
		go func() {
			defer wg.Done()
			if _, err := rc.GetHeading(); err != nil {
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
	return &RotatorController{
		portName: "FAKE",
		proto:    mustProtocol(protocolName),
		open:     func(string) (serial.Port, error) { return fp, nil },
	}
}

func TestSetHeadingWritesCommand(t *testing.T) {
	fp := newFakePort()
	rc := newFakeController("prosistel", fp)
	if err := rc.SetHeading(90); err != nil {
		t.Fatalf("SetHeading: %v", err)
	}
	if got := fp.writtenString(); got != "AP1090\r" {
		t.Errorf("wrote %q, want %q", got, "AP1090\r")
	}
	if err := rc.SetHeading(400); err == nil {
		t.Error("SetHeading(400): expected validation error")
	}
}

func TestGetHeadingParsesResponse(t *testing.T) {
	fp := newFakePort()
	fp.responses["AI1\r"] = []byte("+A123\r")
	rc := newFakeController("prosistel", fp)
	h, err := rc.GetHeading()
	if err != nil {
		t.Fatalf("GetHeading: %v", err)
	}
	if h != 123 {
		t.Errorf("got %d, want 123", h)
	}
}

func TestGetHeadingFlushesStaleBytes(t *testing.T) {
	fp := newFakePort()
	fp.pending = []byte("+A007\r") // leftover reply from an earlier command
	fp.responses["AI1\r"] = []byte("+A123\r")
	rc := newFakeController("prosistel", fp)
	h, err := rc.GetHeading()
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
	_, err := rc.GetHeading()
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestStopWritesCommand(t *testing.T) {
	fp := newFakePort()
	rc := newFakeController("prosistel", fp)
	if err := rc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := fp.writtenString(); got != "AX1\r" {
		t.Errorf("wrote %q, want %q", got, "AX1\r")
	}
}

func TestReconnectAfterPortFailure(t *testing.T) {
	dead := newFakePort()
	dead.failNext = 1000 // port has gone away; every write fails
	replacement := newFakePort()
	replacement.responses["AI1\r"] = []byte("+A045\r")

	opened := 0
	rc := &RotatorController{
		portName: "FAKE",
		proto:    mustProtocol("prosistel"),
		open: func(string) (serial.Port, error) {
			opened++
			if opened == 1 {
				return dead, nil
			}
			return replacement, nil
		},
	}

	h, err := rc.GetHeading()
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
	rc := &RotatorController{
		portName: "FAKE",
		proto:    mustProtocol("prosistel"),
		open: func(string) (serial.Port, error) {
			opened++
			if opened == 1 {
				return dead, nil
			}
			return nil, fmt.Errorf("device gone")
		},
	}

	if _, err := rc.GetHeading(); err == nil {
		t.Fatal("expected error when port and reconnect both fail")
	}
	if rc.Connected() {
		t.Error("controller should report disconnected after failed reconnect")
	}
}
