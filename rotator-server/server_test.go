package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func dispatch(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	srv := CreateServer()
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	rr := httptest.NewRecorder()
	srv.HttpServer.Handler.ServeHTTP(rr, req)
	return rr
}

func TestHealthz(t *testing.T) {
	rr := dispatch(t, "GET", "/healthz")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d", rr.Code)
	}
	if body := rr.Body.String(); body != "OK" {
		t.Fatalf("expected body 'OK'; got %q", body)
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
	srv := CreateServer()

	post := httptest.NewRecorder()
	srv.HttpServer.Handler.ServeHTTP(post, httptest.NewRequest("POST", "/set-heading?heading=270", nil))
	if post.Code != http.StatusOK {
		t.Fatalf("set-heading: expected 200; got %d (%s)", post.Code, post.Body.String())
	}
	var setResp headingBody
	if err := json.Unmarshal(post.Body.Bytes(), &setResp); err != nil {
		t.Fatalf("set-heading body not JSON: %v (%q)", err, post.Body.String())
	}
	if setResp.Heading != 270 || setResp.Status != "set" {
		t.Fatalf("set-heading: got %+v, want {Heading:270 Status:set}", setResp)
	}

	get := httptest.NewRecorder()
	srv.HttpServer.Handler.ServeHTTP(get, httptest.NewRequest("GET", "/get-heading", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get-heading: expected 200; got %d (%s)", get.Code, get.Body.String())
	}
	var getResp headingBody
	if err := json.Unmarshal(get.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("get-heading body not JSON: %v (%q)", err, get.Body.String())
	}
	if getResp.Heading != 270 {
		t.Fatalf("get-heading: got heading=%d, want 270", getResp.Heading)
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
