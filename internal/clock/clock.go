// clock.go provides a testable time abstraction layer, decoupling business logic from a direct dependency on system time.
//
// This file contains:
//   - Clock interface: abstracts time.Now() calls, improving code testability
//   - RealClock: production implementation, returns real system time
//   - FakeClock: test implementation, supports manually advancing time to simulate timeouts and scheduled scenarios
//
// Use cases:
//   - Business code obtains time through the Clock interface instead of calling time.Now() directly
//   - Unit tests inject FakeClock to precisely control time progression and verify timeout logic
//   - Scheduled-task schedulers use the Clock interface to support fast verification in test environments
package clock

import "time"

// The Clock interface abstracts time.Now() calls to improve code testability.
// Implementations provide time-access capability; business code calls via the interface rather than depending on the time package directly.
//
// Production: use RealClock to return real system time.
// Testing: use FakeClock to return a manually controllable fixed time, convenient for simulating time-progression scenarios.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
}

// RealClock is the production implementation of the Clock interface, returning real system time.
type RealClock struct{}

// Now returns the current system time.
func (RealClock) Now() time.Time { return time.Now() }

// FakeClock is the test implementation of the Clock interface, returning a fixed time that can be manually advanced.
// It is used in unit tests to simulate time-related behavior, such as timeouts and scheduled-task triggers.
type FakeClock struct {
	fixed time.Time
}

// NewFakeClock creates a FakeClock instance set to the specified time.
//
// Parameters:
//   - t: the initial time of the fake clock
//
// Returns:
//   - *FakeClock: the initialized fake clock instance
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{fixed: t}
}

// Now returns the currently set time of the fake clock; it does not change with the system clock.
func (f *FakeClock) Now() time.Time { return f.fixed }

// Advance advances the fake clock forward by the specified duration, simulating time progression.
//
// Parameters:
//   - d: the duration to advance; supports negative values (to move time backward)
func (f *FakeClock) Advance(d time.Duration) {
	f.fixed = f.fixed.Add(d)
}
