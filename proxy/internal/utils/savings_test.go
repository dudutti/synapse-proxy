package utils

import (
	"testing"
)

func TestCalculateSavings(t *testing.T) {
	tests := []struct {
		provider string
		orig     int
		opt      int
		expected float64
	}{
		{"openai", 1000, 500, 0.0005},
		{"anthropic", 2000000, 1000000, 1.0},
		{"google", 500, 1000, 0.0}, // No savings if optimized is greater
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := CalculateSavings(tt.provider, tt.orig, tt.opt)
			if got != tt.expected {
				t.Errorf("CalculateSavings(%q, %d, %d) = %v; want %v", tt.provider, tt.orig, tt.opt, got, tt.expected)
			}
		})
	}
}
