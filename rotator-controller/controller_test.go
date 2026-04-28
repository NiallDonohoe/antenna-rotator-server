package controller

import (
	"testing"
)

func TestParseAndValidateDegrees(t *testing.T) {
	tests := []struct {
		input   string
		wantDeg int
		wantErr bool
	}{
		{"0", 0, false},
		{"90", 90, false},
		{"359", 359, false},
		{"  180  ", 180, false},
		{"-1", 0, true},
		{"360", 0, true},
		{"abc", 0, true},
		{"", 0, true},
	}
	for _, tc := range tests {
		deg, err := parseAndValidateDegrees(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseAndValidateDegrees(%q): expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseAndValidateDegrees(%q): unexpected error: %v", tc.input, err)
			}
			if deg != tc.wantDeg {
				t.Errorf("parseAndValidateDegrees(%q): got %d, want %d", tc.input, deg, tc.wantDeg)
			}
		}
	}
}

func TestParseProsistelResponse(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"+A090", "90", false},
		{"+A000", "0", false},
		{"+A359", "359", false},
		{"090", "90", false},
		{"+090", "90", false},
		{"  +A180  ", "180", false},
		{"", "", true},
		{"+Axyz", "", true},
	}
	for _, tc := range tests {
		got, err := parseProsistelResponse(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseProsistelResponse(%q): expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseProsistelResponse(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("parseProsistelResponse(%q): got %q, want %q", tc.input, got, tc.want)
			}
		}
	}
}

func TestSimulationMode(t *testing.T) {
	rc := &RotatorController{} // port == nil → simulation mode

	// Default heading before any set
	h, err := rc.GetHeading()
	if err != nil {
		t.Fatalf("GetHeading error: %v", err)
	}
	if h != "0" {
		t.Errorf("default heading: got %q, want %q", h, "0")
	}

	if err := rc.SetHeading("270"); err != nil {
		t.Fatalf("SetHeading error: %v", err)
	}
	h, err = rc.GetHeading()
	if err != nil {
		t.Fatalf("GetHeading error: %v", err)
	}
	if h != "270" {
		t.Errorf("after SetHeading(270): got %q, want %q", h, "270")
	}

	if err := rc.SetHeading("400"); err == nil {
		t.Error("SetHeading(400): expected error for out-of-range heading")
	}
}
