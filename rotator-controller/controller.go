package controller

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
)

const (
	baudRate    = 9600
	readTimeout = 2 * time.Second
)

// Prosistel serial protocol (8N1, 9600 baud):
//   Set azimuth : AP1XXX\r  (XXX = zero-padded 3-digit degrees, e.g. "AP1090\r" for 90°)
//   Get azimuth : AI1\r     → response "+AXXX\r"  (XXX = current degrees)
//   Stop        : AX1\r

type RotatorController struct {
	port    serial.Port
	heading string // simulation mode only
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
	return &RotatorController{}
}

func NewRotatorController() (*RotatorController, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, fmt.Errorf("could not list serial ports: %v", err)
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("no serial ports found")
	}
	return NewRotatorControllerWithPort(ports[0])
}

func NewRotatorControllerWithPort(portName string) (*RotatorController, error) {
	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	p, err := serial.Open(portName, mode)
	if err != nil {
		return nil, fmt.Errorf("could not open serial port %s: %v", portName, err)
	}
	if err := p.SetReadTimeout(readTimeout); err != nil {
		p.Close()
		return nil, fmt.Errorf("could not set read timeout on %s: %v", portName, err)
	}
	return &RotatorController{port: p}, nil
}

// SetHeading rotates to the given heading (0–359 degrees).
func (rc *RotatorController) SetHeading(heading string) error {
	deg, err := parseAndValidateDegrees(heading)
	if err != nil {
		return err
	}
	if rc.port == nil {
		rc.heading = strconv.Itoa(deg)
		return nil
	}
	cmd := fmt.Sprintf("AP1%03d\r", deg)
	if _, err := rc.port.Write([]byte(cmd)); err != nil {
		return fmt.Errorf("failed to send set-heading command: %v", err)
	}
	return nil
}

// GetHeading queries the controller for the current azimuth.
func (rc *RotatorController) GetHeading() (string, error) {
	if rc.port == nil {
		if rc.heading == "" {
			return "0", nil
		}
		return rc.heading, nil
	}
	if _, err := rc.port.Write([]byte("AI1\r")); err != nil {
		return "", fmt.Errorf("failed to send get-heading query: %v", err)
	}
	resp, err := readLine(rc.port)
	if err != nil {
		return "", fmt.Errorf("failed to read heading response: %v", err)
	}
	return parseProsistelResponse(resp)
}

// Stop halts any in-progress rotation.
func (rc *RotatorController) Stop() error {
	if rc.port == nil {
		return nil
	}
	if _, err := rc.port.Write([]byte("AX1\r")); err != nil {
		return fmt.Errorf("failed to send stop command: %v", err)
	}
	return nil
}

func (rc *RotatorController) Close() {
	if rc.port != nil {
		rc.port.Close()
	}
}

// readLine reads from the port one byte at a time until CR/LF or the port
// read timeout fires with no bytes (indicating no more data).
func readLine(p serial.Port) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := p.Read(buf)
		if n > 0 {
			if buf[0] == '\r' || buf[0] == '\n' {
				break
			}
			sb.WriteByte(buf[0])
		}
		if err != nil {
			return "", err
		}
		// n == 0, err == nil means the read timeout fired with no data.
		if n == 0 {
			if sb.Len() == 0 {
				return "", fmt.Errorf("timeout: no response from rotator")
			}
			break
		}
	}
	return sb.String(), nil
}

// parseProsistelResponse parses the AI1 response.
// Expected format: "+AXXX" where XXX is the 3-digit azimuth in degrees.
func parseProsistelResponse(resp string) (string, error) {
	s := strings.TrimSpace(resp)
	s = strings.TrimPrefix(s, "+A")
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return "", fmt.Errorf("empty response from rotator")
	}
	deg, err := strconv.Atoi(s)
	if err != nil {
		return "", fmt.Errorf("non-numeric response %q from rotator", resp)
	}
	return strconv.Itoa(deg), nil
}

func parseAndValidateDegrees(heading string) (int, error) {
	deg, err := strconv.Atoi(strings.TrimSpace(heading))
	if err != nil {
		return 0, fmt.Errorf("invalid heading %q: must be an integer", heading)
	}
	if deg < 0 || deg > 359 {
		return 0, fmt.Errorf("heading %d out of range: must be 0–359", deg)
	}
	return deg, nil
}
