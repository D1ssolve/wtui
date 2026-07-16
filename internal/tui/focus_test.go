package tui

import "testing"

func TestFocusPanel_Next(t *testing.T) {
	cases := []struct {
		label string
		input FocusPanel
		want  FocusPanel
	}{
		{"tasks→services", FocusTasks, FocusServices},
		{"services→releases", FocusServices, FocusReleases},
		{"releases→output", FocusReleases, FocusOutput},
		{"output→tasks", FocusOutput, FocusTasks},
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
		{"tasks→output", FocusTasks, FocusOutput},
		{"services→tasks", FocusServices, FocusTasks},
		{"releases→services", FocusReleases, FocusServices},
		{"output→releases", FocusOutput, FocusReleases},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			if got := tc.input.Prev(); got != tc.want {
				t.Errorf("Prev() = %v, want %v", got, tc.want)
			}
		})
	}
}
