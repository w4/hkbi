package characteristic

import "github.com/brutella/hap/characteristic"

const TypeHomeKitCameraActive = "21B"

type HomeKitCameraActive struct {
	*characteristic.Bool
}

func NewHomeKitCameraActive() *HomeKitCameraActive {
	c := characteristic.NewBool(TypeHomeKitCameraActive)
	c.Format = characteristic.FormatBool
	c.Permissions = []string{characteristic.PermissionRead, characteristic.PermissionWrite, characteristic.PermissionEvents}

	c.SetValue(false)

	return &HomeKitCameraActive{c}
}
