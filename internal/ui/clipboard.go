package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"golang.org/x/term"
)

var (
	stdoutIsTerminal = func() bool {
		return term.IsTerminal(int(os.Stdout.Fd()))
	}

	writeOSC52Clipboard = func(text string) error {
		payload := base64.StdEncoding.EncodeToString([]byte(text))
		_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\a", payload)
		return err
	}

	lookPath = exec.LookPath

	runClipboardCommand = func(name string, args []string, text string) error {
		cmd := exec.Command(name, args...)
		cmd.Stdin = bytes.NewBufferString(text)
		return cmd.Run()
	}
)

func parseCopyIndex(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("Usage: /copy [n] where n is a positive assistant message number")
	}
	return n, nil
}

func (m *Model) copyAssistantMessage(selection int) (string, string, error) {
	assistantMsgs := assistantMessages(m.messages)
	if len(assistantMsgs) == 0 {
		return "", "", fmt.Errorf("No assistant responses available to copy.")
	}

	idx := len(assistantMsgs) - 1
	target := "last assistant response"
	if selection > 0 {
		if selection > len(assistantMsgs) {
			return "", "", fmt.Errorf("Assistant message %d does not exist. %d available.", selection, len(assistantMsgs))
		}
		idx = selection - 1
		target = fmt.Sprintf("assistant response %d", selection)
	}

	method, err := copyToClipboard(assistantMsgs[idx].Content)
	if err != nil {
		return "", "", err
	}

	return target, method, nil
}

func assistantMessages(messages []ChatMessage) []ChatMessage {
	assistant := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Type == MsgAgent && msg.Content != "" {
			assistant = append(assistant, msg)
		}
	}
	return assistant
}

func copyToClipboard(text string) (string, error) {
	if stdoutIsTerminal() {
		if err := writeOSC52Clipboard(text); err == nil {
			return "OSC 52", nil
		}
	}

	if _, err := lookPath("pbcopy"); err == nil {
		if err := runClipboardCommand("pbcopy", nil, text); err == nil {
			return "pbcopy", nil
		}
	}

	if _, err := lookPath("xclip"); err == nil {
		if err := runClipboardCommand("xclip", []string{"-selection", "clipboard"}, text); err == nil {
			return "xclip", nil
		}
	}

	return "", fmt.Errorf("Clipboard copy failed. Need a terminal that accepts OSC 52 or a working pbcopy/xclip.")
}
