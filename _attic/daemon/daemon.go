package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jstamagal/bitchtea/internal/llm"
)

const (
	defaultHeartbeatInterval = 25 * time.Minute
	defaultJanitorInterval   = 30 * time.Minute

	// compactModel is the REQUIRED high-quality model for HOT.md compaction.
	// NEVER set this to a cheap or local model — see project memory notes.
	compactModel    = "claude-opus-4-6"
	compactProvider = "anthropic"
	compactBaseURL  = "https://api.anthropic.com/v1"

	hotCompactThreshold = 8 * 1024 // 8 KB
)

// clientParams groups arguments for llm.NewClient.
type clientParams struct {
	APIKey, BaseURL, Model, Provider string
}

// Config holds daemon runtime parameters. Populate via DefaultConfig.
type Config struct {
	// DataDir is the root bitchtea data directory (~/.local/share/bitchtea).
	DataDir string

	// HeartbeatModel is the cheap/local model used for periodic pings.
	// Compaction always uses claude-opus-4-6 regardless of this value.
	HeartbeatModel string

	Logger *log.Logger
}

// DefaultConfig builds Config from environment variables.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		DataDir:        filepath.Join(home, ".local", "share", "bitchtea"),
		HeartbeatModel: envCoalesce("BITCHTEA_HEARTBEAT_MODEL", "BITCHTEA_MODEL", "gpt-4o-mini"),
		Logger:         log.New(os.Stderr, "[btdaemon] ", log.LstdFlags),
	}
}

// Daemon runs two background jobs: heartbeat (cheap model ping) and janitor
// (prune HOT.md tool blocks + compact with Opus 4.6).
type Daemon struct {
	dataDir   string
	heartbeat clientParams
	compact   clientParams
	logger    *log.Logger
	stop      chan struct{}
	done      chan struct{}
}

// New creates a Daemon from a Config.
func New(cfg Config) *Daemon {
	heartbeatAPIKey := envCoalesce("OPENAI_API_KEY", "ANTHROPIC_API_KEY", "")
	heartbeatBaseURL := envCoalesce("OPENAI_BASE_URL", "https://api.openai.com/v1")
	heartbeatProvider := os.Getenv("BITCHTEA_PROVIDER")
	if heartbeatProvider == "" {
		if os.Getenv("ANTHROPIC_API_KEY") != "" && os.Getenv("OPENAI_API_KEY") == "" {
			heartbeatProvider = "anthropic"
			heartbeatAPIKey = os.Getenv("ANTHROPIC_API_KEY")
			heartbeatBaseURL = envCoalesce("ANTHROPIC_BASE_URL", "https://api.anthropic.com/v1")
		} else {
			heartbeatProvider = "openai"
		}
	}

	logger := cfg.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "[btdaemon] ", log.LstdFlags)
	}

	return &Daemon{
		dataDir: cfg.DataDir,
		heartbeat: clientParams{
			APIKey:   heartbeatAPIKey,
			BaseURL:  heartbeatBaseURL,
			Model:    cfg.HeartbeatModel,
			Provider: heartbeatProvider,
		},
		compact: clientParams{
			APIKey:   os.Getenv("ANTHROPIC_API_KEY"),
			BaseURL:  envCoalesce("ANTHROPIC_BASE_URL", compactBaseURL),
			Model:    compactModel,
			Provider: compactProvider,
		},
		logger: logger,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

// Start runs the daemon loop and blocks until ctx is canceled or Stop is called.
func (d *Daemon) Start(ctx context.Context) error {
	d.logger.Printf("starting — heartbeat every %v, janitor every %v",
		defaultHeartbeatInterval, defaultJanitorInterval)
	d.logger.Printf("heartbeat model=%s  compact model=%s (provider=%s)",
		d.heartbeat.Model, compactModel, compactProvider)

	hbTicker := time.NewTicker(defaultHeartbeatInterval)
	janTicker := time.NewTicker(defaultJanitorInterval)
	defer hbTicker.Stop()
	defer janTicker.Stop()
	defer close(d.done)

	// Janitor pass at startup so memory is clean immediately.
	d.runJanitor(ctx)

	for {
		select {
		case <-ctx.Done():
			d.logger.Printf("shutting down: %v", ctx.Err())
			return ctx.Err()
		case <-d.stop:
			d.logger.Printf("stop signal received")
			return nil
		case <-hbTicker.C:
			d.runHeartbeat(ctx)
		case <-janTicker.C:
			d.runJanitor(ctx)
		}
	}
}

// Stop signals a clean shutdown and waits for Start to return.
func (d *Daemon) Stop() {
	select {
	case <-d.stop:
	default:
		close(d.stop)
	}
	<-d.done
}

// runHeartbeat pings the cheap model to confirm API connectivity.
func (d *Daemon) runHeartbeat(ctx context.Context) {
	d.logger.Printf("heartbeat start (model=%s)", d.heartbeat.Model)

	client := llm.NewClient(d.heartbeat.APIKey, d.heartbeat.BaseURL, d.heartbeat.Model, d.heartbeat.Provider)
	evCh := make(chan llm.StreamEvent, 16)
	go client.StreamChat(ctx, []llm.Message{{Role: "user", Content: "heartbeat"}}, nil, evCh)

	replyLen := 0
	for ev := range evCh {
		if ev.Type == "error" {
			d.logger.Printf("heartbeat error: %v", ev.Error)
			return
		}
		if ev.Type == "text" {
			replyLen += len(ev.Text)
		}
	}
	d.logger.Printf("heartbeat ok (reply bytes=%d)", replyLen)
}

// runJanitor prunes HOT.md files and compacts oversized ones with Opus 4.6.
func (d *Daemon) runJanitor(ctx context.Context) {
	d.logger.Printf("janitor start")
	memDir := filepath.Join(d.dataDir, "memory")
	if err := runJanitorInDir(ctx, memDir, d.compact, d.logger); err != nil {
		d.logger.Printf("janitor error: %v", err)
		return
	}
	d.logger.Printf("janitor done")
}

// --------------------------------------------------------------------------
// Systemd install / uninstall
// --------------------------------------------------------------------------

const unitTpl = `[Unit]
Description=bitchtea background daemon — heartbeat and memory janitor
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=60
EnvironmentFile=-%s/.config/bitchtea/daemon.env
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
`

const daemonEnvSkeleton = `# bitchtea daemon environment
# Edit this file to set API keys for the background daemon.
# Loaded automatically by the systemd user service.

ANTHROPIC_API_KEY=
OPENAI_API_KEY=
OPENAI_BASE_URL=
BITCHTEA_HEARTBEAT_MODEL=
# BITCHTEA_COMPACT_MODEL is locked to claude-opus-4-6 — do not override with a cheap model
`

// Install writes the systemd user unit for bitchtea-daemon and enables it.
// If systemd is not available (Docker, non-Linux) it prints manual instructions.
func Install(execPath string) error {
	if runtime.GOOS != "linux" {
		printFallback(execPath)
		return nil
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		printFallback(execPath)
		return nil
	}

	home, _ := os.UserHomeDir()

	// Ensure ~/.config/systemd/user/ exists.
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}

	// Write unit file.
	unitContent := fmt.Sprintf(unitTpl, execPath, home)
	unitPath := filepath.Join(unitDir, "bitchtea-daemon.service")
	if err := os.WriteFile(unitPath, []byte(unitContent), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	fmt.Printf("unit file: %s\n", unitPath)

	// Write skeleton daemon.env if absent.
	envDir := filepath.Join(home, ".config", "bitchtea")
	if err := os.MkdirAll(envDir, 0700); err != nil {
		return fmt.Errorf("create bitchtea config dir: %w", err)
	}
	envPath := filepath.Join(envDir, "daemon.env")
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if err := os.WriteFile(envPath, []byte(daemonEnvSkeleton), 0600); err != nil {
			return fmt.Errorf("write daemon.env skeleton: %w", err)
		}
		fmt.Printf("env file: %s (add your API keys)\n", envPath)
	}

	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", "bitchtea-daemon.service"},
	} {
		cmd := exec.Command("systemctl", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
		}
	}
	fmt.Println("bitchtea-daemon installed and started via systemd --user")
	return nil
}

// Uninstall stops, disables, and removes the systemd user unit.
func Uninstall() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl not found")
	}
	cmd := exec.Command("systemctl", "--user", "disable", "--now", "bitchtea-daemon.service")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()

	unitPath := filepath.Join(userSystemdDir(), "bitchtea-daemon.service")
	_ = os.Remove(unitPath)

	reload := exec.Command("systemctl", "--user", "daemon-reload")
	reload.Stdout = os.Stdout
	reload.Stderr = os.Stderr
	_ = reload.Run()

	fmt.Println("bitchtea-daemon uninstalled")
	return nil
}

// WritePID writes the daemon PID to a file for process management.
func WritePID(dataDir string) (func(), error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return func() {}, fmt.Errorf("create data dir: %w", err)
	}
	pidPath := filepath.Join(dataDir, "daemon.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644); err != nil {
		return func() {}, fmt.Errorf("write pid file: %w", err)
	}
	return func() { os.Remove(pidPath) }, nil
}

func printFallback(execPath string) {
	fmt.Printf("systemd not available. Run manually:\n")
	fmt.Printf("  nohup %s > ~/.local/share/bitchtea/daemon.log 2>&1 &\n", execPath)
	fmt.Printf("Add to ~/.profile or ~/.bashrc to start on login.\n")
}

func userSystemdDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user")
}

func envCoalesce(keys ...string) string {
	for i, key := range keys {
		// Last element is treated as a literal fallback value if no env key
		// before it matched.
		if i == len(keys)-1 {
			// If it looks like an env key (no spaces), try it as env first.
			if !strings.Contains(key, " ") && strings.ToUpper(key) == key {
				if v := os.Getenv(key); v != "" {
					return v
				}
			}
			return key // literal fallback
		}
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}
