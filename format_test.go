package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestToPercent(t *testing.T) {
	assert := assert.New(t)
	assert.Equal("0%", toPercent(0))
	assert.Equal("1%", toPercent(0.01))
	assert.Equal("50%", toPercent(0.5))
	assert.Equal("100%", toPercent(1))
	assert.Equal("150%", toPercent(1.5))
}

func TestFormatDuration(t *testing.T) {
	assert := assert.New(t)
	assert.Equal("00:00:00", formatDuration(0))
	assert.Equal("00:19:31", formatDuration(19*time.Minute+31*time.Second))
	assert.Equal("01:00:00", formatDuration(time.Hour))
	assert.Equal("10:09:08", formatDuration(10*time.Hour+9*time.Minute+8*time.Second))
}

func TestGetDuration(t *testing.T) {
	assert := assert.New(t)
	from := time.Unix(1000, 0)
	to := time.Unix(1060, 0)
	assert.Equal(time.Minute, getDuration(from, to))
	// Always non-negative regardless of argument order.
	assert.Equal(time.Minute, getDuration(to, from))
}
