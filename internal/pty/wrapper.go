package pty

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/creack/pty"
)

// Wrapper spawns an interactive shell inside a pseudoterminal and mirrors
// output to the user's terminal with zero-latency forwarding.
type Wrapper struct {
	shell  string
	cmd    *exec.Cmd
	ingest io.Writer
}

// SetIngest configures an optional writer that receives a duplicate of the
// raw PTY output stream (used by the ingestion pipeline).
func (w *Wrapper) SetIngest(writer io.Writer) {
	w.ingest = writer
}

// New creates a PTY wrapper that will spawn the given shell binary.
// An empty shell falls back to $SHELL or a platform default.
func New(shell string) (*Wrapper, error) {
	resolved, err := resolveShell(shell)
	if err != nil {
		return nil, err
	}
	return &Wrapper{shell: resolved}, nil
}

func resolveShell(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell, nil
	}
	switch runtime.GOOS {
	case "windows":
		if comspec := os.Getenv("COMSPEC"); comspec != "" {
			return comspec, nil
		}
		return "powershell.exe", nil
	default:
		return "", fmt.Errorf("SHELL environment variable is not set")
	}
}

// Run starts the shell inside a PTY, forwards stdin/stdout, and handles
// terminal resize signals until the shell exits.
func (w *Wrapper) Run() error {
	w.cmd = exec.Command(w.shell)
	w.cmd.Env = os.Environ()
	w.cmd.Dir, _ = os.Getwd()

	ptmx, err := pty.Start(w.cmd)
	if err != nil {
		return fmt.Errorf("start pty: %w", err)
	}
	defer ptmx.Close()

	stopResize := watchResize(ptmx)
	defer stopResize()

	sink := io.Discard
	if w.ingest != nil {
		sink = w.ingest
	}

	// Mirror PTY output to stdout immediately while duplicating bytes to ingest.
	tee := io.TeeReader(ptmx, sink)

	go func() { _, _ = io.Copy(os.Stdout, tee) }()
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()

	if err := w.cmd.Wait(); err != nil {
		return fmt.Errorf("shell exited: %w", err)
	}
	return nil
}
