// Package utils
package utils

import (
	"math"
	"strconv"
)

func HumanSI(v float64) string {
	abs := math.Abs(v)
	var value float64
	var suffix string

	switch {
	case abs >= 1_000_000_000:
		value = v / 1_000_000_000
		suffix = "B"
	case abs >= 1_000_000:
		value = v / 1_000_000
		suffix = "M"
	case abs >= 1_000:
		value = v / 1_000
		suffix = "K"
	default:
		// No suffix: return plain float without trailing zeros
		return strconv.FormatFloat(v, 'f', -1, 64)
	}

	// Format without trailing zeros and return uppercase suffix
	return strconv.FormatFloat(value, 'f', -1, 64) + suffix
}
