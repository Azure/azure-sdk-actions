package main

import (
	"fmt"
	"time"
)

// toPercent converts a fraction (e.g. 0.01) to a percentage string with no
// decimal places (e.g. "1%").
func toPercent(value float64) string {
	return fmt.Sprintf("%.0f%%", value*100)
}

// getDuration returns the absolute duration between two times.
func getDuration(from time.Time, to time.Time) time.Duration {
	d := to.Sub(from)
	if d < 0 {
		return -d
	}
	return d
}

// formatDuration formats a duration as hh:mm:ss (always zero-padded).
func formatDuration(d time.Duration) string {
	totalSeconds := int64(d / time.Second)
	hours := totalSeconds / 3600
	totalSeconds %= 3600
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}
