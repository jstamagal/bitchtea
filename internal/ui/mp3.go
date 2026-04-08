package ui

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hajimehoshi/go-mp3"
)

const (
	mp3PanelWidth        = 34
	mp3StatusBarWidth    = 38
	mp3VisualizerBars    = 8
	mp3ProgressBarWidth  = 10
	mp3DefaultTrackWidth = 24
)

var mp3LibraryDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".bitchtea", "mp3")
	}
	return filepath.Join(home, ".bitchtea", "mp3")
}

var mp3DurationReader = readMP3Duration
var mp3ProcessStarter = startMP3Process

type mp3TickMsg time.Time

type mp3DoneMsg struct {
	generation int
	err        error
}

type mp3Track struct {
	Path     string
	Name     string
	Duration time.Duration
}

type mp3Process interface {
	Pause() error
	Resume() error
	Stop() error
	Done() <-chan error
}

type mp3Controller struct {
	libraryDir     func() string
	durationReader func(string) (time.Duration, error)
	startProcess   func(string) (mp3Process, error)
	now            func() time.Time

	tracks     []mp3Track
	current    int
	visible    bool
	playing    bool
	paused     bool
	startedAt  time.Time
	offset     time.Duration
	process    mp3Process
	generation int
}

func newMP3Controller() *mp3Controller {
	return &mp3Controller{
		libraryDir:     mp3LibraryDir,
		durationReader: mp3DurationReader,
		startProcess:   mp3ProcessStarter,
		now:            time.Now,
	}
}

func (p *mp3Controller) hasTracks() bool {
	return len(p.tracks) > 0
}

func (p *mp3Controller) scan() error {
	dir := p.libraryDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			p.tracks = nil
			p.current = 0
			return nil
		}
		return fmt.Errorf("scan %s: %w", dir, err)
	}

	var tracks []mp3Track
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.ToLower(filepath.Ext(name)) != ".mp3" {
			continue
		}
		path := filepath.Join(dir, name)
		duration, err := p.durationReader(path)
		if err != nil {
			duration = 0
		}
		tracks = append(tracks, mp3Track{
			Path:     path,
			Name:     strings.TrimSuffix(name, filepath.Ext(name)),
			Duration: duration,
		})
	}

	sort.Slice(tracks, func(i, j int) bool {
		return strings.ToLower(tracks[i].Name) < strings.ToLower(tracks[j].Name)
	})

	currentPath := ""
	if p.current >= 0 && p.current < len(p.tracks) {
		currentPath = p.tracks[p.current].Path
	}
	p.tracks = tracks
	p.current = 0
	for i, track := range p.tracks {
		if track.Path == currentPath {
			p.current = i
			break
		}
	}
	return nil
}

func (p *mp3Controller) rescan() string {
	if err := p.scan(); err != nil {
		return fmt.Sprintf("MP3 scan failed: %v", err)
	}
	if !p.hasTracks() {
		p.stop()
		return fmt.Sprintf("No MP3s found in %s", p.libraryDir())
	}
	return fmt.Sprintf("Loaded %d track(s) from %s", len(p.tracks), p.libraryDir())
}

func (p *mp3Controller) toggle() (string, tea.Cmd) {
	p.visible = !p.visible
	if !p.visible {
		return "MP3 panel hidden.", nil
	}
	msg := p.rescan()
	if !p.hasTracks() {
		return msg, nil
	}
	if p.process != nil {
		return fmt.Sprintf("MP3 panel ready. %s", p.currentTrack().Name), nil
	}
	playMsg, cmd := p.playIndex(p.current)
	if playMsg == "" {
		playMsg = "MP3 panel ready."
	}
	return playMsg, cmd
}

func (p *mp3Controller) stop() {
	p.generation++
	p.playing = false
	p.paused = false
	p.offset = 0
	if p.process != nil {
		_ = p.process.Stop()
	}
	p.process = nil
}

func (p *mp3Controller) currentTrack() mp3Track {
	if !p.hasTracks() {
		return mp3Track{}
	}
	if p.current < 0 || p.current >= len(p.tracks) {
		p.current = 0
	}
	return p.tracks[p.current]
}

func (p *mp3Controller) playIndex(idx int) (string, tea.Cmd) {
	if !p.hasTracks() {
		return fmt.Sprintf("No MP3s found in %s", p.libraryDir()), nil
	}
	if idx < 0 {
		idx = len(p.tracks) - 1
	}
	if idx >= len(p.tracks) {
		idx = 0
	}

	p.stop()
	proc, err := p.startProcess(p.tracks[idx].Path)
	if err != nil {
		return fmt.Sprintf("MP3 playback failed: %v", err), nil
	}

	p.current = idx
	p.process = proc
	p.playing = true
	p.paused = false
	p.offset = 0
	p.startedAt = p.now()
	p.generation++
	gen := p.generation

	return fmt.Sprintf("Now playing: %s", p.currentTrack().Name), func() tea.Msg {
		return mp3DoneMsg{generation: gen, err: <-proc.Done()}
	}
}

func (p *mp3Controller) next() (string, tea.Cmd) {
	if !p.hasTracks() {
		return fmt.Sprintf("No MP3s found in %s", p.libraryDir()), nil
	}
	return p.playIndex(p.current + 1)
}

func (p *mp3Controller) prev() (string, tea.Cmd) {
	if !p.hasTracks() {
		return fmt.Sprintf("No MP3s found in %s", p.libraryDir()), nil
	}
	return p.playIndex(p.current - 1)
}

func (p *mp3Controller) togglePause() string {
	if p.process == nil {
		return "No MP3 track is playing."
	}
	if p.paused {
		if err := p.process.Resume(); err != nil {
			return fmt.Sprintf("Resume failed: %v", err)
		}
		p.paused = false
		p.startedAt = p.now()
		return fmt.Sprintf("Resumed: %s", p.currentTrack().Name)
	}
	p.offset = p.elapsed()
	if err := p.process.Pause(); err != nil {
		return fmt.Sprintf("Pause failed: %v", err)
	}
	p.paused = true
	return fmt.Sprintf("Paused: %s", p.currentTrack().Name)
}

func (p *mp3Controller) elapsed() time.Duration {
	if !p.playing {
		return 0
	}
	if p.paused {
		return p.offset
	}
	return p.offset + p.now().Sub(p.startedAt)
}

func (p *mp3Controller) statusText() string {
	if !p.hasTracks() {
		return "♫ idle"
	}

	track := p.currentTrack()
	icon := "▶"
	if p.paused {
		icon = "▌▌"
	}
	if !p.playing {
		icon = "■"
	}

	trackName := truncateRunes(track.Name, mp3DefaultTrackWidth)
	progress := renderMP3ProgressBar(track.Duration, p.elapsed(), mp3ProgressBarWidth)
	timing := fmt.Sprintf("%s/%s", formatDuration(p.elapsed()), formatDuration(track.Duration))
	viz := renderMP3Visualizer(track.Path, p.elapsed(), mp3VisualizerBars)

	return fmt.Sprintf("♫ %s %s %s %s %s", trackName, icon, progress, timing, viz)
}

func (p *mp3Controller) handleDone(msg mp3DoneMsg) (string, tea.Cmd) {
	if msg.generation != p.generation {
		return "", nil
	}

	finished := p.currentTrack()
	p.process = nil
	p.playing = false
	p.paused = false
	p.offset = finished.Duration

	if msg.err != nil {
		return fmt.Sprintf("MP3 playback ended: %v", msg.err), nil
	}
	if len(p.tracks) <= 1 {
		return fmt.Sprintf("Finished: %s", finished.Name), nil
	}

	playMsg, cmd := p.next()
	if playMsg == "" {
		playMsg = fmt.Sprintf("Finished: %s", finished.Name)
	}
	return playMsg, cmd
}

func (p *mp3Controller) renderPanel(height int) string {
	if !p.visible || height < 4 {
		return ""
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Theme.Yellow).
		Width(mp3PanelWidth-2).
		Padding(0, 1)

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(Theme.Yellow)
	activeStyle := lipgloss.NewStyle().Foreground(Theme.Green)
	mutedStyle := lipgloss.NewStyle().Foreground(Theme.Gray)

	var lines []string
	lines = append(lines, headerStyle.Render("MP3 Player"))
	lines = append(lines, strings.Repeat("─", mp3PanelWidth-4))
	lines = append(lines, fmt.Sprintf("Dir: %s", truncateRunes(p.libraryDir(), mp3PanelWidth-8)))
	lines = append(lines, "Controls: space pause, ←/j prev, →/k next")
	lines = append(lines, "")

	if !p.hasTracks() {
		lines = append(lines, mutedStyle.Render("Drop .mp3 files into the library dir."))
		content := strings.Join(lines, "\n")
		return panelStyle.Render(content)
	}

	lines = append(lines, headerStyle.Render("Now Playing"))
	lines = append(lines, strings.Repeat("─", mp3PanelWidth-4))
	lines = append(lines, truncateRunes(p.statusText(), mp3PanelWidth-6))
	lines = append(lines, "")
	lines = append(lines, headerStyle.Render("Playlist"))
	lines = append(lines, strings.Repeat("─", mp3PanelWidth-4))

	maxTracks := height - len(lines) - 3
	if maxTracks < 1 {
		maxTracks = 1
	}

	start := 0
	if p.current >= maxTracks {
		start = p.current - maxTracks + 1
	}
	end := start + maxTracks
	if end > len(p.tracks) {
		end = len(p.tracks)
	}

	for i := start; i < end; i++ {
		line := fmt.Sprintf("  %d. %s", i+1, truncateRunes(p.tracks[i].Name, mp3PanelWidth-10))
		if i == p.current {
			line = activeStyle.Render("▶ " + strings.TrimPrefix(line, "  "))
		}
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	return panelStyle.Render(content)
}

func readMP3Duration(path string) (time.Duration, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	decoder, err := mp3.NewDecoder(f)
	if err != nil {
		return 0, err
	}
	if decoder.SampleRate() == 0 {
		return 0, nil
	}

	samples := decoder.Length() / 4
	seconds := float64(samples) / float64(decoder.SampleRate())
	return time.Duration(seconds * float64(time.Second)), nil
}

type shellMP3Process struct {
	cmd  *exec.Cmd
	done chan error
}

func startMP3Process(path string) (mp3Process, error) {
	type playerSpec struct {
		name string
		args []string
	}

	specs := []playerSpec{
		{name: "mpv", args: []string{"--no-video", "--really-quiet", "--no-terminal", path}},
		{name: "ffplay", args: []string{"-nodisp", "-autoexit", "-loglevel", "error", path}},
		{name: "mpg123", args: []string{"-q", path}},
	}

	for _, spec := range specs {
		if _, err := exec.LookPath(spec.name); err != nil {
			continue
		}
		cmd := exec.Command(spec.name, spec.args...)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		proc := &shellMP3Process{
			cmd:  cmd,
			done: make(chan error, 1),
		}
		go func() {
			err := cmd.Wait()
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) && exitErr.ExitCode() < 0 {
					err = nil
				}
			}
			proc.done <- err
		}()
		return proc, nil
	}

	return nil, errors.New("no supported audio player found (tried mpv, ffplay, mpg123)")
}

func (p *shellMP3Process) Pause() error {
	return p.signal(syscall.SIGSTOP)
}

func (p *shellMP3Process) Resume() error {
	return p.signal(syscall.SIGCONT)
}

func (p *shellMP3Process) Stop() error {
	return p.signal(syscall.SIGKILL)
}

func (p *shellMP3Process) Done() <-chan error {
	return p.done
}

func (p *shellMP3Process) signal(sig syscall.Signal) error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Signal(sig)
}

func renderMP3ProgressBar(total, elapsed time.Duration, width int) string {
	if width < 4 {
		width = 4
	}
	if total <= 0 {
		return "[" + strings.Repeat("·", width) + "]"
	}
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > total {
		elapsed = total
	}
	filled := int((float64(elapsed) / float64(total)) * float64(width))
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("·", width-filled) + "]"
}

func renderMP3Visualizer(seed string, elapsed time.Duration, bars int) string {
	if bars < 1 {
		bars = 1
	}
	levels := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	base := int(h.Sum32())
	step := int(elapsed/time.Second) + 1

	var out []rune
	for i := 0; i < bars; i++ {
		level := (base + (i+1)*(step+3) + i*i) % len(levels)
		out = append(out, levels[level])
	}
	return string(out)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "?:??"
	}
	totalSeconds := int(d.Round(time.Second) / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	if limit == 1 {
		return string(runes[:1])
	}
	return string(runes[:limit-1]) + "…"
}

func mp3TickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return mp3TickMsg(t)
	})
}
