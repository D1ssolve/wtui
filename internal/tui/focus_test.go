package tui

import "testing"

func TestFocusPanel_Next(t *testing.T) {
	cases := []struct {
		label string
		input FocusPanel
		want  FocusPanel
	}{
		{"tasks→services", FocusTasks, FocusServices},
		{"services→output", FocusServices, FocusOutput},
		{"output→releases", FocusOutput, FocusReleases},
		{"releases→tasks", FocusReleases, FocusTasks},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			if got := tc.input.Next(); got != tc.want {
				t.Errorf("Next() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFocusPanel_Prev(t *testing.T) {
	cases := []struct {
		label string
		input FocusPanel
		want  FocusPanel
	}{
		{"tasks→releases", FocusTasks, FocusReleases},
		{"services→tasks", FocusServices, FocusTasks},
		{"output→services", FocusOutput, FocusServices},
		{"releases→output", FocusReleases, FocusOutput},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			if got := tc.input.Prev(); got != tc.want {
				t.Errorf("Prev() = %v, want %v", got, tc.want)
			}
		})
	}
}
