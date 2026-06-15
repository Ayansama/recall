//go:build windows

package pty

import "os"

func watchResize(_ *os.File) func() {
	return func() {}
}
