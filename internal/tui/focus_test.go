package tui

import "testing"

func TestFocusPanel_Next(t *testing.T) {
	cases := []struct {
		label string
		input FocusPanel
		want  FocusPanel
	}{
		{"tasks→services", FocusTasks, FocusServices},
		{"services→tasks", FocusServices, FocusTasks},
		{"output→tasks (safe default)", FocusOutput, FocusTasks},
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
		{"services→tasks", FocusServices, FocusTasks},
		{"tasks→services", FocusTasks, FocusServices},
		{"output→services (safe default)", FocusOutput, FocusServices},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			if got := tc.input.Prev(); got != tc.want {
				t.Errorf("Prev() = %v, want %v", got, tc.want)
			}
		})
	}
}
