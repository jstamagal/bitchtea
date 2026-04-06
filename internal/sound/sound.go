package sound

// Play plays a notification sound
func Play(soundType string) {
	switch soundType {
	case "done":
		// Terminal bell
		print("\a")
	case "success":
		// Single bell
		print("\a")
	case "error":
		// Three quick beeps
		print("\a\a\a")
	default:
		print("\a")
	}
}

// Beep sends a terminal bell character
func Beep() {
	print("\a")
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
