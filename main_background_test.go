package main

import "testing"

// The FlexTerm themes paint their flat background before the first frame.
func TestWindowBackgroundColourFlexTerm(t *testing.T) {
	for theme, want := range map[string][3]uint8{
		"flexterm":       {10, 17, 32},
		"flexterm-light": {244, 246, 251},
		"deep-blue":      {16, 23, 40},
	} {
		c := windowBackgroundColour(theme)
		if [3]uint8{c.Red, c.Green, c.Blue} != want || c.Alpha != 255 {
			t.Errorf("%s: %+v", theme, c)
		}
	}
}
