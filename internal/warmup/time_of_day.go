package warmup

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// timeOfDay represents a wall-clock time independent of date or zone.
// The zone is supplied separately at evaluation time (see nextFrom).
type timeOfDay struct {
	hour   int
	minute int
}

// parseTimeOfDay accepts "HH:MM" in 24-hour format.
func parseTimeOfDay(s string) (timeOfDay, error) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return timeOfDay{}, fmt.Errorf("expected HH:MM, got %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return timeOfDay{}, fmt.Errorf("invalid hour %q", parts[0])
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return timeOfDay{}, fmt.Errorf("invalid minute %q", parts[1])
	}
	return timeOfDay{hour: h, minute: m}, nil
}

// String formats as "HH:MM".
func (t timeOfDay) String() string {
	return fmt.Sprintf("%02d:%02d", t.hour, t.minute)
}

// nextFrom returns the next time instant matching this time-of-day in the
// reference time's location. If the target has already passed (or equals ref),
// returns the same time the following calendar day.
//
// Uses time.Date year/month/day+1 instead of Add(24h) so DST transitions do
// not silently shift the fire time by one hour.
func (t timeOfDay) nextFrom(ref time.Time) time.Time {
	loc := ref.Location()
	candidate := time.Date(ref.Year(), ref.Month(), ref.Day(), t.hour, t.minute, 0, 0, loc)
	if !candidate.After(ref) {
		candidate = time.Date(ref.Year(), ref.Month(), ref.Day()+1, t.hour, t.minute, 0, 0, loc)
	}
	return candidate
}
