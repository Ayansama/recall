package project

import (
	"os"
	"path/filepath"
)

// Discover walks upward from startDir looking for a .git/config file.
// It returns the repository root directory and its base folder name.
// If no Git repository is found, startDir (absolute) and its base name are returned.
func Discover(startDir string) (projectPath, projectName string) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		dir = startDir
	}

	for {
		gitConfig := filepath.Join(dir, ".git", "config")
		if _, err := os.Stat(gitConfig); err == nil {
			return dir, filepath.Base(dir)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	abs, err := filepath.Abs(startDir)
	if err != nil {
		return startDir, filepath.Base(startDir)
	}
	return abs, filepath.Base(abs)
}
