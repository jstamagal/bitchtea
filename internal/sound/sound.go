package sound

import (
	"io"
	"os"
)

// Output receives terminal bell bytes. Tests override it to capture output.
var Output io.Writer = os.Stdout

// Play plays a notification sound
func Play(soundType string) {
	switch soundType {
	case "bell":
		writeBell(1)
	case "done":
		writeBell(1)
	case "success":
		writeBell(1)
	case "error":
		writeBell(3)
	default:
		writeBell(1)
	}
}

// Beep sends a terminal bell character
func Beep() {
	writeBell(1)
}

// Success plays a success sound
func Success() {
	Play("success")
}

// Error plays an error sound
func Error() {
	Play("error")
}

// Done plays a completion sound
func Done() {
	Play("done")
}

func writeBell(count int) {
	for range count {
		_, _ = io.WriteString(Output, "\a")
	}
}
