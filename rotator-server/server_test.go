package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	srv := CreateServer()
	req, err := http.NewRequest("GET", "/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	srv.HttpServer.Handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("expected status 200; got %d", status)
	}

	if body := rr.Body.String(); body != "OK" {
		t.Fatalf("expected body 'OK'; got %q", body)
	}
}
