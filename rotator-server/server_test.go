package server

import (
	controller "antenna-rotator-server/rotator-controller"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return New(Config{Rotator: controller.NewSimulationController(), Version: "test"})
}

func dispatch(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	srv := newTestServer(t)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(method, target, nil))
	return rr
}

func TestHealthz(t *testing.T) {
	for _, path := range []string{"/healthz", "/api/v1/healthz"} {
		rr := dispatch(t, "GET", path)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: expected 200; got %d", path, rr.Code)
		}
		var h healthBody
		if err := json.Unmarshal(rr.Body.Bytes(), &h); err != nil {
			t.Fatalf("%s: body not JSON: %v (%q)", path, err, rr.Body.String())
		}
		if h.Status != "ok" || h.Mode != "simulation" || h.Protocol != "simulation" || !h.Connected || h.Version != "test" {
			t.Fatalf("%s: got %+v, want status=ok mode=simulation protocol=simulation connected=true version=test", path, h)
		}
	}
}

func TestRootRedirectsToSwagger(t *testing.T) {
	rr := dispatch(t, "GET", "/")
	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302; got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/swagger/" {
		t.Fatalf("expected redirect to /swagger/; got %q", loc)
	}
}

func TestUnknownPathIs404(t *testing.T) {
	rr := dispatch(t, "GET", "/nope")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404; got %d", rr.Code)
	}
}

func TestSwaggerUIServed(t *testing.T) {
	rr := dispatch(t, "GET", "/swagger/")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected text/html; got %q", ct)
	}
	if !strings.Contains(rr.Body.String(), "swagger-ui") {
		t.Fatalf("swagger HTML did not contain expected marker; body starts with: %.120s", rr.Body.String())
	}
}

func TestOpenAPISpecServed(t *testing.T) {
	rr := dispatch(t, "GET", "/openapi.yaml")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "openapi:") || !strings.Contains(body, "/set-heading") {
		t.Fatalf("openapi.yaml does not look like the expected spec; body starts with: %.120s", body)
	}
}

func TestSetHeadingThenGet(t *testing.T) {
	srv := newTestServer(t)

	for _, prefix := range []string{"", "/api/v1"} {
		post := httptest.NewRecorder()
		srv.Handler().ServeHTTP(post, httptest.NewRequest("POST", prefix+"/set-heading?heading=270", nil))
		if post.Code != http.StatusOK {
			t.Fatalf("%s/set-heading: expected 200; got %d (%s)", prefix, post.Code, post.Body.String())
		}
		var setResp headingBody
		if err := json.Unmarshal(post.Body.Bytes(), &setResp); err != nil {
			t.Fatalf("set-heading body not JSON: %v (%q)", err, post.Body.String())
		}
		if setResp.Heading != 270 || setResp.Status != "set" {
			t.Fatalf("set-heading: got %+v, want {Heading:270 Status:set}", setResp)
		}

		get := httptest.NewRecorder()
		srv.Handler().ServeHTTP(get, httptest.NewRequest("GET", prefix+"/get-heading", nil))
		if get.Code != http.StatusOK {
			t.Fatalf("%s/get-heading: expected 200; got %d (%s)", prefix, get.Code, get.Body.String())
		}
		var getResp headingBody
		if err := json.Unmarshal(get.Body.Bytes(), &getResp); err != nil {
			t.Fatalf("get-heading body not JSON: %v (%q)", err, get.Body.String())
		}
		if getResp.Heading != 270 {
			t.Fatalf("get-heading: got heading=%d, want 270", getResp.Heading)
		}
	}
}

func TestSetHeadingJSONBody(t *testing.T) {
	srv := newTestServer(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/set-heading", strings.NewReader(`{"heading": 45}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d (%s)", rr.Code, rr.Body.String())
	}
	var resp headingBody
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if resp.Heading != 45 {
		t.Fatalf("got heading=%d, want 45", resp.Heading)
	}

	// Body without the heading field is a 400.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/set-heading", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing heading field; got %d", rr.Code)
	}
}

func TestSetHeadingValidation(t *testing.T) {
	cases := []struct {
		name   string
		target string
		want   int
	}{
		{"missing heading", "/set-heading", http.StatusBadRequest},
		{"out of range", "/set-heading?heading=400", http.StatusBadRequest},
		{"non-numeric", "/set-heading?heading=abc", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := dispatch(t, "POST", tc.target)
			if rr.Code != tc.want {
				t.Fatalf("got %d (%s), want %d", rr.Code, rr.Body.String(), tc.want)
			}
			var e errorBody
			if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil || e.Error == "" {
				t.Fatalf("expected JSON error body; got %q", rr.Body.String())
			}
		})
	}
}

func TestOffsetGetAndSet(t *testing.T) {
	srv := newTestServer(t)

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/offset", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /offset: expected 200; got %d", rr.Code)
	}
	var got offsetBody
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("offset body not JSON: %v", err)
	}
	if got.Offset != 0 {
		t.Fatalf("default offset: got %d, want 0", got.Offset)
	}

	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/offset?offset=15", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /offset: expected 200; got %d (%s)", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("offset body not JSON: %v", err)
	}
	if got.Offset != 15 || got.Status != "set" {
		t.Fatalf("POST /offset: got %+v, want {Offset:15 Status:set}", got)
	}

	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/offset", nil))
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("offset body not JSON: %v", err)
	}
	if got.Offset != 15 {
		t.Fatalf("GET /offset after set: got %d, want 15", got.Offset)
	}

	// The offset affects heading calibration end-to-end: a rotator whose
	// zero point is 15 degrees off should still report the requested
	// real-world heading back.
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/set-heading?heading=100", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("set-heading: expected 200; got %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/get-heading", nil))
	var h headingBody
	if err := json.Unmarshal(rr.Body.Bytes(), &h); err != nil {
		t.Fatalf("get-heading body not JSON: %v", err)
	}
	if h.Heading != 100 {
		t.Fatalf("get-heading with offset applied: got %d, want 100", h.Heading)
	}
}

func TestOffsetJSONBodyNormalisesAndValidates(t *testing.T) {
	srv := newTestServer(t)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/offset", strings.NewReader(`{"offset": -5}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d (%s)", rr.Code, rr.Body.String())
	}
	var got offsetBody
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("offset body not JSON: %v", err)
	}
	if got.Offset != 355 { // -5 normalised modulo 360
		t.Fatalf("got offset=%d, want 355 (normalised -5)", got.Offset)
	}

	// Missing offset is a 400.
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/offset", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing offset; got %d", rr.Code)
	}
}

func TestStop(t *testing.T) {
	rr := dispatch(t, "POST", "/stop")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d (%s)", rr.Code, rr.Body.String())
	}
	var resp stopBody
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("stop body not JSON: %v", err)
	}
	if resp.Status != "stopped" {
		t.Fatalf("got status=%q, want stopped", resp.Status)
	}
}

func TestListPorts(t *testing.T) {
	rr := dispatch(t, "GET", "/list-ports")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d (%s)", rr.Code, rr.Body.String())
	}
	var resp portsBody
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("list-ports body not JSON: %v", err)
	}
	if resp.Ports == nil {
		t.Fatal("ports field should be a (possibly empty) array, not null")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	rr := dispatch(t, "GET", "/set-heading")
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405; got %d", rr.Code)
	}
}

// brokenRotator simulates a rotator whose serial link is down.
type brokenRotator struct{}

func (brokenRotator) SetHeading(context.Context, int) error { return fmt.Errorf("serial write failed") }
func (brokenRotator) GetHeading(context.Context) (int, error) {
	return 0, fmt.Errorf("serial read failed")
}
func (brokenRotator) Stop(context.Context) error { return fmt.Errorf("serial write failed") }
func (brokenRotator) Mode() string               { return "hardware" }
func (brokenRotator) Protocol() string           { return "prosistel" }
func (brokenRotator) Connected() bool            { return false }
func (brokenRotator) Offset() int                { return 0 }
func (brokenRotator) SetOffset(int)              {}

func TestSerialFailuresReturn502(t *testing.T) {
	srv := New(Config{Rotator: brokenRotator{}})

	cases := []struct{ method, target string }{
		{"POST", "/set-heading?heading=90"},
		{"GET", "/get-heading"},
		{"POST", "/stop"},
	}
	for _, tc := range cases {
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequest(tc.method, tc.target, nil))
		if rr.Code != http.StatusBadGateway {
			t.Errorf("%s %s: got %d, want 502", tc.method, tc.target, rr.Code)
		}
	}

	// A validation error is still the client's fault even when the link is
	// down — but brokenRotator never validates, so use the real controller:
	// out-of-range headings must map to 400, not 502.
	rr := httptest.NewRecorder()
	real := New(Config{Rotator: controller.NewSimulationController()})
	real.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/set-heading?heading=400", nil))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("out-of-range heading: got %d, want 400", rr.Code)
	}

	// healthz still answers and reports the degraded link.
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("healthz: got %d, want 200", rr.Code)
	}
	var h healthBody
	if err := json.Unmarshal(rr.Body.Bytes(), &h); err != nil {
		t.Fatalf("healthz body not JSON: %v", err)
	}
	if h.Mode != "hardware" || h.Connected {
		t.Fatalf("healthz: got %+v, want mode=hardware connected=false", h)
	}
}

func TestReadyz(t *testing.T) {
	for _, path := range []string{"/readyz", "/api/v1/readyz"} {
		rr := dispatch(t, "GET", path)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: expected 200; got %d (%s)", path, rr.Code, rr.Body.String())
		}
		var r readyBody
		if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
			t.Fatalf("%s: body not JSON: %v (%q)", path, err, rr.Body.String())
		}
		if r.Status != "ready" || r.Heading == nil || *r.Heading != 0 || r.Error != "" {
			t.Fatalf("%s: got %+v, want status=ready heading=0", path, r)
		}
	}
}

func TestReadyzReportsBrokenLink(t *testing.T) {
	srv := New(Config{Rotator: brokenRotator{}})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503; got %d (%s)", rr.Code, rr.Body.String())
	}
	var r readyBody
	if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if r.Status != "unavailable" || r.Heading != nil || r.Error == "" {
		t.Fatalf("got %+v, want status=unavailable with an error and no heading", r)
	}
}

func TestOffsetIsSaved(t *testing.T) {
	saved := -1
	srv := New(Config{
		Rotator:    controller.NewSimulationController(),
		SaveOffset: func(offset int) error { saved = offset; return nil },
	})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/offset?offset=-5", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d (%s)", rr.Code, rr.Body.String())
	}
	if saved != 355 {
		t.Fatalf("SaveOffset got %d, want 355 (the normalised value)", saved)
	}
}

func TestOffsetSaveFailureIs500(t *testing.T) {
	srv := New(Config{
		Rotator:    controller.NewSimulationController(),
		SaveOffset: func(int) error { return fmt.Errorf("read-only file system") },
	})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/offset?offset=15", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500; got %d (%s)", rr.Code, rr.Body.String())
	}
	var e errorBody
	if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil || !strings.Contains(e.Error, "not saved") {
		t.Fatalf("expected a 'not saved' error body; got %q", rr.Body.String())
	}
}

func TestReadsLoggedAtDebugActionsAtInfo(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv := newTestServer(t)
	for _, req := range []*http.Request{
		httptest.NewRequest("GET", "/api/v1/get-heading", nil),
		httptest.NewRequest("GET", "/api/v1/readyz", nil),
		httptest.NewRequest("POST", "/api/v1/set-heading?heading=90", nil),
	} {
		srv.Handler().ServeHTTP(httptest.NewRecorder(), req)
	}

	logs := buf.String()
	if strings.Contains(logs, "get-heading") || strings.Contains(logs, "readyz") {
		t.Errorf("reads should be logged at debug, hidden at info; got:\n%s", logs)
	}
	if !strings.Contains(logs, "set-heading") {
		t.Errorf("actions should be logged at info; got:\n%s", logs)
	}
}

// busyRotator never gets the serial port: every call waits until its
// context gives up.
type busyRotator struct{ brokenRotator }

func (busyRotator) SetHeading(ctx context.Context, _ int) error {
	<-ctx.Done()
	return ctx.Err()
}
func (busyRotator) GetHeading(ctx context.Context) (int, error) {
	<-ctx.Done()
	return 0, ctx.Err()
}
func (busyRotator) Stop(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestRequestsGiveUpWhenClientLeaves(t *testing.T) {
	srv := New(Config{Rotator: busyRotator{}})
	for _, tc := range []struct{ method, target string }{
		{"POST", "/set-heading?heading=90"},
		{"GET", "/get-heading"},
		{"POST", "/stop"},
	} {
		ctx, cancel := context.WithCancel(t.Context())
		cancel() // the client has disconnected
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequestWithContext(ctx, tc.method, tc.target, nil))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: got %d, want 503", tc.method, tc.target, rr.Code)
		}
	}
}

func TestOverlappingOffsetUpdatesSaveTheOffsetInUse(t *testing.T) {
	rotator := controller.NewSimulationController()
	var mu sync.Mutex
	saved := -1
	srv := New(Config{
		Rotator: rotator,
		SaveOffset: func(offset int) error {
			time.Sleep(time.Millisecond) // widen the window a race would need
			mu.Lock()
			saved = offset
			mu.Unlock()
			return nil
		},
	})

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			rr := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rr, httptest.NewRequest("POST", fmt.Sprintf("/offset?offset=%d", i), nil))
		})
	}
	wg.Wait()
	if saved != rotator.Offset() {
		t.Fatalf("saved offset %d, but the offset in use is %d", saved, rotator.Offset())
	}
}
