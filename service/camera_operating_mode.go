package service

import (
	"github.com/brutella/hap/service"
	characteristic2 "github.com/w4/hkbi/characteristic"
)

const TypeCameraOperatingMode = "21A"

type CameraOperatingMode struct {
	*service.S

	EventSnapshotsActive    *characteristic2.EventSnapshotsActive
	HomeKitCameraActive     *characteristic2.HomeKitCameraActive
	PeriodicSnapshotsActive *characteristic2.PeriodicSnapshotsActive
}

func NewCameraOperatingMode() *CameraOperatingMode {
	s := CameraOperatingMode{}
	s.S = service.New(TypeCameraOperatingMode)

	s.EventSnapshotsActive = characteristic2.NewEventSnapshotsActive()
	s.AddC(s.EventSnapshotsActive.C)

	s.HomeKitCameraActive = characteristic2.NewHomeKitCameraActive()
	s.AddC(s.HomeKitCameraActive.C)

	s.PeriodicSnapshotsActive = characteristic2.NewPeriodicSnapshotsActive()
	s.AddC(s.PeriodicSnapshotsActive.C)

	return &s
}
