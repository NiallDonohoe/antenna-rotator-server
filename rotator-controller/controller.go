package controller

import (
	"fmt"

	"github.com/google/gousb"
)

type RotatorController struct {
	ctx    *gousb.Context
	device *gousb.Device
}

func NewRotatorController(vid, pid int) (*RotatorController, error) {
	ctx := gousb.NewContext()
	dev, err := ctx.OpenDeviceWithVIDPID(gousb.ID(vid), gousb.ID(pid))
	if err != nil {
		ctx.Close()
		return nil, fmt.Errorf("could not open USB device: %v", err)
	}
	return &RotatorController{ctx: ctx, device: dev}, nil
}

func (rc *RotatorController) SetHeading(heading string) error {
	// Convert heading to bytes and send to device
	// This is a placeholder; actual implementation depends on your device protocol
	data := []byte(heading)
	_, err := rc.device.Control(0x40, 0x01, 0, 0, data)
	if err != nil {
		return fmt.Errorf("failed to send heading: %v", err)
	}
	return nil
}

func (rc *RotatorController) Close() {
	rc.device.Close()
	rc.ctx.Close()
}
