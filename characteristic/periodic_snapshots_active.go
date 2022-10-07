package characteristic

import "github.com/brutella/hap/characteristic"

const TypePeriodicSnapshotsActive = "225"

type PeriodicSnapshotsActive struct {
	*characteristic.Bool
}

func NewPeriodicSnapshotsActive() *PeriodicSnapshotsActive {
	c := characteristic.NewBool(TypePeriodicSnapshotsActive)
	c.Format = characteristic.FormatBool
	c.Permissions = []string{characteristic.PermissionRead, characteristic.PermissionWrite, characteristic.PermissionEvents}

	c.SetValue(false)

	return &PeriodicSnapshotsActive{c}
}
