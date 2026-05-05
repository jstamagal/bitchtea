package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeMP3Process struct {
	done    chan error
	paused  int
	resumed int
	stopped int
}

func newFakeMP3Process() *fakeMP3Process {
	return &fakeMP3Process{done: make(chan error, 1)}
}

func (p *fakeMP3Process) Pause() error {
	p.paused++
	return nil
}

func (p *fakeMP3Process) Resume() error {
	p.resumed++
	return nil
}

func (p *fakeMP3Process) Stop() error {
	p.stopped++
	return nil
}

func (p *fakeMP3Process) Done() <-chan error {
	return p.done
}

func stubMP3Globals(t *testing.T, dir string) func() {
	t.Helper()

	origDir := mp3LibraryDir
	origDuration := mp3DurationReader
	origStarter := mp3ProcessStarter

	mp3LibraryDir = func() string { return dir }
	mp3DurationReader = func(path string) (time.Duration, error) {
		switch filepath.Base(path) {
		case "alpha.mp3":
			return 3*time.Minute + 5*time.Second, nil
		case "beta.mp3":
			return 4*time.Minute + 10*time.Second, nil
		default:
			return 90 * time.Second, nil
		}
	}

	return func() {
		mp3LibraryDir = origDir
		mp3DurationReader = origDuration
		mp3ProcessStarter = origStarter
	}
}

func writeFakeMP3(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("fake mp3"), 0644); err != nil {
		t.Fatalf("write fake mp3: %v", err)
	}
}

func TestMP3CommandStartsPlaybackAndShowsPanel(t *testing.T) {
	dir := t.TempDir()
	writeFakeMP3(t, dir, "beta.mp3")
	writeFakeMP3(t, dir, "alpha.mp3")

	restore := stubMP3Globals(t, dir)
	defer restore()

	var started []string
	mp3ProcessStarter = func(path string) (mp3Process, error) {
		started = append(started, filepath.Base(path))
		return newFakeMP3Process(), nil
	}

	m := testModel(t)
	result, cmd := m.handleCommand("/mp3")
	model := result.(Model)
	msg := lastMsg(result)

	if cmd == nil {
		t.Fatal("expected playback command")
	}
	if !model.mp3.visible {
		t.Fatal("expected MP3 panel to be visible")
	}
	if model.mp3.currentTrack().Name != "alpha" {
		t.Fatalf("expected sorted first track alpha, got %q", model.mp3.currentTrack().Name)
	}
	if len(started) != 1 || started[0] != "alpha.mp3" {
		t.Fatalf("expected alpha.mp3 to start first, got %v", started)
	}
	if !strings.Contains(msg.Content, "Now playing: alpha") {
		t.Fatalf("unexpected status message: %q", msg.Content)
	}
}

func TestMP3KeybindingsControlPlayback(t *testing.T) {
	dir := t.TempDir()
	writeFakeMP3(t, dir, "alpha.mp3")
	writeFakeMP3(t, dir, "beta.mp3")

	restore := stubMP3Globals(t, dir)
	defer restore()

	var processes []*fakeMP3Process
	mp3ProcessStarter = func(path string) (mp3Process, error) {
		proc := newFakeMP3Process()
		processes = append(processes, proc)
		return proc, nil
	}

	m := testModel(t)
	result, _ := m.handleCommand("/mp3")
	model := result.(Model)
	model.input.SetValue("")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeySpace})
	pausedModel := updated.(Model)
	if !pausedModel.mp3.paused {
		t.Fatal("expected playback to pause")
	}
	if processes[0].paused != 1 {
		t.Fatalf("expected pause signal, got %d", processes[0].paused)
	}

	updated, _ = pausedModel.Update(tea.KeyMsg{Type: tea.KeyRight})
	nextModel := updated.(Model)
	if nextModel.mp3.currentTrack().Name != "beta" {
		t.Fatalf("expected beta after next, got %q", nextModel.mp3.currentTrack().Name)
	}
	if len(processes) != 2 {
		t.Fatalf("expected a new process for next track, got %d", len(processes))
	}
	if processes[0].stopped != 1 {
		t.Fatalf("expected previous process to stop, got %d", processes[0].stopped)
	}
}

func TestMP3StatusTextIncludesTrackProgress(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 30, 0, time.UTC)
	ctrl := &mp3Controller{
		now:       func() time.Time { return now },
		tracks:    []mp3Track{{Path: "alpha.mp3", Name: "alpha", Duration: 3 * time.Minute}},
		current:   0,
		playing:   true,
		startedAt: now.Add(-90 * time.Second),
	}

	status := ctrl.statusText()
	if !strings.Contains(status, "alpha") {
		t.Fatalf("expected track name in status, got %q", status)
	}
	if !strings.Contains(status, "[") || !strings.Contains(status, "/") {
		t.Fatalf("expected progress details in status, got %q", status)
	}
	if !strings.Contains(status, "♫") {
		t.Fatalf("expected music marker in status, got %q", status)
	}
}

func TestMP3HelpCommandDocumentsControls(t *testing.T) {
	m := testModel(t)
	result, _ := m.handleCommand("/help")
	msg := lastMsg(result)
	if !strings.Contains(msg.Content, "/mp3 [cmd]") {
		t.Fatalf("expected /mp3 in help output, got %q", msg.Content)
	}
}
