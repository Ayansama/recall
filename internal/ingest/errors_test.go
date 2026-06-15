package ingest

import "testing"

func TestIsErrorLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"panic: runtime error", true},
		{"Traceback (most recent call last):", true},
		{"Uncaught Exception: boom", true},
		{"UnhandledPromiseRejection: fail", true},
		{"Error: something failed", true},
		{"Fatal: disk full", true},
		{"Exception: bad state", true},
		{"normal log output", false},
		{"  panic: indented", false},
	}

	for _, tc := range tests {
		got := IsErrorLine(tc.line)
		if got != tc.want {
			t.Errorf("IsErrorLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
