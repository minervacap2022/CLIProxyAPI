package warmup

import (
	"testing"
	"time"
)

func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		in      string
		wantH   int
		wantM   int
		wantErr bool
	}{
		{in: "00:00", wantH: 0, wantM: 0},
		{in: "08:45", wantH: 8, wantM: 45},
		{in: "23:59", wantH: 23, wantM: 59},
		{in: " 09:15 ", wantH: 9, wantM: 15},
		{in: "24:00", wantErr: true},
		{in: "12:60", wantErr: true},
		{in: "nope", wantErr: true},
		{in: "12", wantErr: true},
		{in: "-1:00", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseTimeOfDay(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.hour != tc.wantH || got.minute != tc.wantM {
				t.Errorf("got %02d:%02d, want %02d:%02d", got.hour, got.minute, tc.wantH, tc.wantM)
			}
		})
	}
}

func TestTimeOfDay_NextFrom(t *testing.T) {
	loc := time.UTC

	// Reference: 2026-04-20 07:00 UTC
	ref := time.Date(2026, 4, 20, 7, 0, 0, 0, loc)

	// Future same-day.
	if got := (timeOfDay{hour: 8, minute: 45}).nextFrom(ref); !got.Equal(time.Date(2026, 4, 20, 8, 45, 0, 0, loc)) {
		t.Errorf("got %s, want 2026-04-20 08:45 UTC", got)
	}

	// Past same-day -> rolls to tomorrow.
	if got := (timeOfDay{hour: 6, minute: 0}).nextFrom(ref); !got.Equal(time.Date(2026, 4, 21, 6, 0, 0, 0, loc)) {
		t.Errorf("got %s, want 2026-04-21 06:00 UTC", got)
	}

	// Exactly equal -> also rolls to tomorrow (strict after).
	if got := (timeOfDay{hour: 7, minute: 0}).nextFrom(ref); !got.Equal(time.Date(2026, 4, 21, 7, 0, 0, 0, loc)) {
		t.Errorf("got %s, want 2026-04-21 07:00 UTC", got)
	}
}

// Covers DST "spring forward": in America/New_York on 2026-03-08, 02:00 local
// jumps to 03:00. A naive Add(24h) would shift the intended 09:00 fire time
// to 10:00 local on the following day. Using time.Date(year, month, day+1,...)
// keeps the wall-clock time stable across the transition.
func TestTimeOfDay_NextFrom_DSTSpringForward(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("timezone data unavailable: %v", err)
	}
	// Ref: 2026-03-07 10:00 local NY (Saturday, day before DST begins).
	ref := time.Date(2026, 3, 7, 10, 0, 0, 0, ny)
	got := (timeOfDay{hour: 9, minute: 0}).nextFrom(ref)
	want := time.Date(2026, 3, 8, 9, 0, 0, 0, ny)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s — DST transition should not shift wall-clock fire time", got, want)
	}
	// Sanity: between 10:00 EST and 09:00 EDT the wall-clock gap is 23h but
	// DST spring-forward consumes one real hour, so the monotonic gap is 22h.
	// Add(24h) would incorrectly give 23h (i.e. fire at 10:00 EDT instead of
	// the desired 09:00 EDT).
	if got.Sub(ref) != 22*time.Hour {
		t.Errorf("expected 22h elapsed across DST spring-forward, got %s", got.Sub(ref))
	}
}
