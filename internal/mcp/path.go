package mcp

import (
	"os"
	"path/filepath"
)

const socketFileName = "mcp.sock"

// DefaultSocketPath returns the canonical Unix domain socket path (~/.recall/mcp.sock).
func DefaultSocketPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".recall", socketFileName), nil
}
