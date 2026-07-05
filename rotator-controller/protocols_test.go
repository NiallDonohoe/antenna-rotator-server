package controller

import (
	"testing"
)

func TestProtocolByName(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"", "prosistel", false}, // default = first registry entry
		{"prosistel", "prosistel", false},
		{"Prosistel", "prosistel", false},
		{"yaesu", "yaesu-gs232", false},
		{"GS232", "yaesu-gs232", false},
		{"gs-232", "yaesu-gs232", false},
		{"yaesu-gs232", "yaesu-gs232", false},
		{"rotawhat", "", true},
	}
	for _, tc := range cases {
		p, err := protocolByName(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("protocolByName(%q): expected error, got %v", tc.input, p)
			}
			continue
		}
		if err != nil {
			t.Errorf("protocolByName(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if p.Name != tc.want {
			t.Errorf("protocolByName(%q): got %q, want %q", tc.input, p.Name, tc.want)
		}
	}
}

func TestSupportedProtocols(t *testing.T) {
	names := SupportedProtocols()
	if len(names) != len(protocols) {
		t.Fatalf("got %d names, want %d", len(names), len(protocols))
	}
	if names[0] != "prosistel" {
		t.Errorf("default (first) protocol: got %q, want %q", names[0], "prosistel")
	}
}

func TestProtocolSpecsComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range protocols {
		if p.Name == "" || p.SetHeadingFmt == "" || p.GetHeadingCmd == "" || p.StopCmd == "" {
			t.Errorf("protocol %+v has empty required fields", p)
		}
		for _, n := range append([]string{p.Name}, p.Aliases...) {
			if seen[n] {
				t.Errorf("duplicate protocol name/alias %q", n)
			}
			seen[n] = true
		}
	}
}

func TestSetHeadingCmd(t *testing.T) {
	cases := []struct {
		protocol string
		deg      int
		want     string
	}{
		{"prosistel", 90, "AP1090\r"},
		{"prosistel", 0, "AP1000\r"},
		{"prosistel", 359, "AP1359\r"},
		{"yaesu", 90, "M090\r"},
		{"yaesu", 5, "M005\r"},
	}
	for _, tc := range cases {
		got := mustProtocol(tc.protocol).setHeadingCmd(tc.deg)
		if got != tc.want {
			t.Errorf("%s setHeadingCmd(%d): got %q, want %q", tc.protocol, tc.deg, got, tc.want)
		}
	}
}

func TestParseHeading(t *testing.T) {
	cases := []struct {
		protocol string
		input    string
		want     int
		wantErr  bool
	}{
		// Prosistel: "+AXXX"
		{"prosistel", "+A090", 90, false},
		{"prosistel", "+A000", 0, false},
		{"prosistel", "+A359", 359, false},
		{"prosistel", "090", 90, false},
		{"prosistel", "+090", 90, false},
		{"prosistel", "  +A180  ", 180, false},
		{"prosistel", "", 0, true},
		{"prosistel", "+Axyz", 0, true},

		// Yaesu GS-232A: "+0XXX"
		{"yaesu", "+0090", 90, false},
		{"yaesu", "+0000", 0, false},
		{"yaesu", "+0359", 359, false},
		{"yaesu", "+0123+0045", 123, false}, // az/el unit: elevation ignored
		// Yaesu GS-232B: "AZ=XXX"
		{"yaesu", "AZ=123", 123, false},
		{"yaesu", "AZ=005", 5, false},
		{"yaesu", "AZ=123  EL=045", 123, false}, // az/el unit: elevation ignored
		{"yaesu", "  AZ=270  ", 270, false},
		{"yaesu", "", 0, true},
		{"yaesu", "?>", 0, true}, // GS-232 error response
		{"yaesu", "AZ=", 0, true},
	}
	for _, tc := range cases {
		got, err := mustProtocol(tc.protocol).parseHeading(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s parseHeading(%q): expected error, got %d", tc.protocol, tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s parseHeading(%q): unexpected error: %v", tc.protocol, tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s parseHeading(%q): got %d, want %d", tc.protocol, tc.input, got, tc.want)
		}
	}
}

func TestYaesuOverFakePort(t *testing.T) {
	fp := newFakePort()
	fp.responses["C\r"] = []byte("AZ=123\r")
	rc := newFakeController("yaesu", fp)

	if err := rc.SetHeading(90); err != nil {
		t.Fatalf("SetHeading: %v", err)
	}
	h, err := rc.GetHeading()
	if err != nil {
		t.Fatalf("GetHeading: %v", err)
	}
	if h != 123 {
		t.Errorf("GetHeading: got %d, want 123", h)
	}
	if err := rc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := fp.writtenString(); got != "M090\rC\rS\r" {
		t.Errorf("wrote %q, want %q", got, "M090\rC\rS\r")
	}
	if rc.Protocol() != "yaesu-gs232" {
		t.Errorf("Protocol(): got %q, want %q", rc.Protocol(), "yaesu-gs232")
	}
}
