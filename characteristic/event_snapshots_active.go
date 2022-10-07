package characteristic

import "github.com/brutella/hap/characteristic"

const TypeEventSnapshotsActive = "223"

type EventSnapshotsActive struct {
	*characteristic.Bool
}

func NewEventSnapshotsActive() *EventSnapshotsActive {
	c := characteristic.NewBool(TypeEventSnapshotsActive)
	c.Format = characteristic.FormatBool
	c.Permissions = []string{characteristic.PermissionRead, characteristic.PermissionWrite, characteristic.PermissionEvents}

	c.SetValue(false)

	return &EventSnapshotsActive{c}
}
