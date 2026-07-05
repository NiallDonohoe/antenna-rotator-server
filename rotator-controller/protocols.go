package controller

import (
	"fmt"
	"strconv"
	"strings"
)

// ProtocolSpec describes a rotator wire protocol as data: the command bytes
// to send and how to find the azimuth in the response. Supporting a new
// rotator means adding one entry to the protocols table below — no code.
type ProtocolSpec struct {
	Name          string   // canonical identifier, reported by /healthz
	Aliases       []string // additional names accepted in ROTATOR_PROTOCOL
	SetHeadingFmt string   // fmt.Sprintf template receiving the azimuth in degrees
	GetHeadingCmd string
	StopCmd       string

	// AzimuthMarkers are tried in order against the get-heading response;
	// the azimuth digits are read from just after the first marker found
	// (or from the start of the response if none match).
	AzimuthMarkers []string
}

// protocols is the registry of supported rotator protocols.
var protocols = []ProtocolSpec{
	{
		// Prosistel "D" protocol. The "1" addresses controller #1
		// (azimuth) on the Prosistel bus.
		//   Set azimuth : AP1XXX\r → —
		//   Get azimuth : AI1\r    → "+AXXX\r"
		//   Stop        : AX1\r    → —
		Name:           "prosistel",
		SetHeadingFmt:  "AP1%03d\r",
		GetHeadingCmd:  "AI1\r",
		StopCmd:        "AX1\r",
		AzimuthMarkers: []string{"+A", "+"},
	},
	{
		// Yaesu GS-232A/B command set, used by the G-450/G-650/G-800/
		// G-1000/G-2800 series via a GS-232 interface.
		//   Set azimuth : MXXX\r → —
		//   Get azimuth : C\r    → "+0XXX" (GS-232A) or "AZ=XXX" (GS-232B)
		//   Stop        : S\r    → —
		// Az/el units answer with "+0XXX+0YYY" or "AZ=XXX  EL=YYY"; the
		// leading-digit-run parse ignores the elevation part.
		Name:           "yaesu-gs232",
		Aliases:        []string{"yaesu", "gs232", "gs-232"},
		SetHeadingFmt:  "M%03d\r",
		GetHeadingCmd:  "C\r",
		StopCmd:        "S\r",
		AzimuthMarkers: []string{"AZ=", "+"},
	},
}

// protocolByName resolves a ROTATOR_PROTOCOL value (case-insensitive,
// canonical name or alias) against the registry. An empty name selects the
// first registry entry, the default protocol.
func protocolByName(name string) (ProtocolSpec, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return protocols[0], nil
	}
	for _, p := range protocols {
		if n == p.Name {
			return p, nil
		}
		for _, alias := range p.Aliases {
			if n == alias {
				return p, nil
			}
		}
	}
	return ProtocolSpec{}, fmt.Errorf("unsupported rotator protocol %q (supported: %s)",
		name, strings.Join(SupportedProtocols(), ", "))
}

// SupportedProtocols lists the canonical names of every registered protocol.
func SupportedProtocols() []string {
	names := make([]string, len(protocols))
	for i, p := range protocols {
		names[i] = p.Name
	}
	return names
}

func (p ProtocolSpec) setHeadingCmd(deg int) string {
	return fmt.Sprintf(p.SetHeadingFmt, deg)
}

// parseHeading extracts the azimuth from a get-heading response: skip past
// the first azimuth marker present, then read the leading run of digits.
func (p ProtocolSpec) parseHeading(resp string) (int, error) {
	s := strings.TrimSpace(resp)
	if s == "" {
		return 0, fmt.Errorf("empty response from rotator")
	}
	for _, m := range p.AzimuthMarkers {
		if i := strings.Index(s, m); i >= 0 {
			s = s[i+len(m):]
			break
		}
	}
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, fmt.Errorf("unrecognised response %q from rotator", resp)
	}
	deg, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0, fmt.Errorf("non-numeric response %q from rotator", resp)
	}
	return deg, nil
}
