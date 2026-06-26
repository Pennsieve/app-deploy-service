package main

import "testing"

func TestParseBuildStorageGiB(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int32
	}{
		{"empty", "", 0},
		{"valid", "100", 100},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"non-numeric", "abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseBuildStorageGiB(tt.in); got != tt.want {
				t.Errorf("parseBuildStorageGiB(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
