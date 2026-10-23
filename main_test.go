package main

import "testing"

func TestSwaggerURL(t *testing.T) {
	cases := map[string]string{
		":8080":              "http://localhost:8080/swagger/",
		"0.0.0.0:8080":       "http://localhost:8080/swagger/",
		"[::]:8080":          "http://localhost:8080/swagger/",
		"127.0.0.1:18080":    "http://127.0.0.1:18080/swagger/",
		"100.64.1.2:8080":    "http://100.64.1.2:8080/swagger/",
		"[fd7a::1]:8080":     "http://[fd7a::1]:8080/swagger/",
		"rotator.local:8080": "http://rotator.local:8080/swagger/",
	}
	for addr, want := range cases {
		if got := swaggerURL(addr); got != want {
			t.Errorf("swaggerURL(%q) = %q, want %q", addr, got, want)
		}
	}
}
