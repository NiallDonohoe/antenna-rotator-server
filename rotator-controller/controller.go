package controller

import (
	"fmt"

	"go.bug.st/serial"
)

type RotatorController struct {
	port    serial.Port
	heading string
}

func NewRotatorController() (*RotatorController, error) {
	// Fallback: auto-detect first available port
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, fmt.Errorf("could not list serial ports: %v", err)
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("no serial ports found")
	}
	// Select the first available port by default
	portName := ports[0]
	mode := &serial.Mode{BaudRate: 9600}
	p, err := serial.Open(portName, mode)
	if err != nil {
		return nil, fmt.Errorf("could not open serial port %s: %v", portName, err)
	}
	return &RotatorController{port: p}, nil
}

func (rc *RotatorController) SetHeading(heading string) error {
	if rc.port != nil {
		data := []byte(heading)
		_, err := rc.port.Write(data)
		if err != nil {
			return fmt.Errorf("failed to send heading: %v", err)
		}
		return nil
	}
	// Simulation mode: store heading locally
	rc.heading = heading
	return nil
}

func (rc *RotatorController) GetHeading() (string, error) {
	if rc.port != nil {
		// Placeholder; implement reading from the device if supported
		return "180", nil
	}
	// Return simulated heading (default to "180")
	if rc.heading == "" {
		return "180", nil
	}
	return rc.heading, nil
}

func (rc *RotatorController) Close() {
	if rc.port != nil {
		rc.port.Close()
	}
}
