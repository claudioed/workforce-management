// Package clock provides ports.Clock implementations.
package clock

import "time"

// System is a ports.Clock backed by the wall clock.
type System struct{}

// Now returns the current time.
func (System) Now() time.Time { return time.Now() }
