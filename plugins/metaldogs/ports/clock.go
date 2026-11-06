package ports

import "time"

// Clock provides the current time for application use cases.
type Clock interface {
	Now() time.Time
}

// SystemClock uses time.Now (UTC).
type SystemClock struct{}

// Now returns time.Now().UTC().
func (SystemClock) Now() time.Time { return time.Now().UTC() }
