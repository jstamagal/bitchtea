package sound

import (
	"bytes"
	"testing"
)

func TestPlayWritesExpectedBellPattern(t *testing.T) {
	testCases := []struct {
		name      string
		soundType string
		want      string
	}{
		{name: "bell", soundType: "bell", want: "\a"},
		{name: "done", soundType: "done", want: "\a"},
		{name: "success", soundType: "success", want: "\a"},
		{name: "error", soundType: "error", want: "\a\a\a"},
		{name: "default", soundType: "unknown", want: "\a"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			origOutput := Output
			Output = &buf
			defer func() { Output = origOutput }()

			Play(tc.soundType)

			if got := buf.String(); got != tc.want {
				t.Fatalf("Play(%q) = %q, want %q", tc.soundType, got, tc.want)
			}
		})
	}
}

func TestHelpersWriteToOutput(t *testing.T) {
	testCases := []struct {
		name string
		call func()
		want string
	}{
		{name: "beep", call: Beep, want: "\a"},
		{name: "success", call: Success, want: "\a"},
		{name: "error", call: Error, want: "\a\a\a"},
		{name: "done", call: Done, want: "\a"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			origOutput := Output
			Output = &buf
			defer func() { Output = origOutput }()

			tc.call()

			if got := buf.String(); got != tc.want {
				t.Fatalf("%s wrote %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
