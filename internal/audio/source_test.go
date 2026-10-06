package audio

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d        time.Duration
		expected string
	}{
		{0, "Live / Unknown"},
		{-1 * time.Second, "Live / Unknown"},
		{45 * time.Second, "00:45"},
		{3*time.Minute + 25*time.Second, "03:25"},
		{1*time.Hour + 2*time.Minute + 3*time.Second, "01:02:03"},
	}

	for _, tc := range cases {
		tr := &Track{Duration: tc.d}
		got := tr.FormatDuration()
		if got != tc.expected {
			t.Errorf("FormatDuration(%v) = %s, expected %s", tc.d, got, tc.expected)
		}
	}
}

func TestExtractVideoID(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://youtu.be/dQw4w9WgXcQ?si=123", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"dQw4w9WgXcQ", "dQw4w9WgXcQ"},
	}

	for _, tc := range cases {
		got := extractVideoID(tc.input)
		if got != tc.expected {
			t.Errorf("extractVideoID(%s) = %s, expected %s", tc.input, got, tc.expected)
		}
	}
}
