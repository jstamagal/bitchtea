package daemon

import "path/filepath"

// Paths bundles the daemon's well-known file locations so callers (CLI,
// daemon binary, tests) do not have to redo string concat. Construct one
// with Layout(baseDir) where baseDir is typically config.BaseDir().
type Paths struct {
	Base       string // ~/.bitchtea
	LockPath   string // ~/.bitchtea/daemon.lock
	PidPath    string // ~/.bitchtea/daemon.pid
	LogPath    string // ~/.bitchtea/daemon.log
	MailboxDir string // ~/.bitchtea/daemon
	MailDir    string // ~/.bitchtea/daemon/mail
	DoneDir    string // ~/.bitchtea/daemon/done
	FailedDir  string // ~/.bitchtea/daemon/failed
}

// Layout returns the canonical Paths anchored at baseDir.
func Layout(baseDir string) Paths {
	mailbox := filepath.Join(baseDir, "daemon")
	return Paths{
		Base:       baseDir,
		LockPath:   filepath.Join(baseDir, "daemon.lock"),
		PidPath:    filepath.Join(baseDir, "daemon.pid"),
		LogPath:    filepath.Join(baseDir, "daemon.log"),
		MailboxDir: mailbox,
		MailDir:    filepath.Join(mailbox, "mail"),
		DoneDir:    filepath.Join(mailbox, "done"),
		FailedDir:  filepath.Join(mailbox, "failed"),
	}
}
