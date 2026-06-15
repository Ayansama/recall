package ingest

import (
	"testing"
	"time"
)

func TestPipelineStripsANSIAndSplitsLines(t *testing.T) {
	out := make(chan Line, 8)
	p := NewPipeline(out)

	raw := "\x1b[31mhello\x1b[0m world\nplain line\npartial"
	if _, err := p.Write([]byte(raw)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	p.Close()
	close(out)

	var lines []Line
	for line := range out {
		lines = append(lines, line)
	}

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %#v", len(lines), lines)
	}
	if lines[0].Text != "hello world" {
		t.Errorf("line 0 = %q, want %q", lines[0].Text, "hello world")
	}
	if lines[1].Text != "plain line" {
		t.Errorf("line 1 = %q, want %q", lines[1].Text, "plain line")
	}
	if lines[2].Text != "partial" {
		t.Errorf("line 2 = %q, want %q", lines[2].Text, "partial")
	}
}

func TestPipelineTagsErrors(t *testing.T) {
	out := make(chan Line, 4)
	p := NewPipeline(out)

	_, _ = p.Write([]byte("panic: oops\nok\n"))
	p.Close()
	close(out)

	var lines []Line
	for line := range out {
		lines = append(lines, line)
	}

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if !lines[0].IsError {
		t.Error("expected first line to be tagged as error")
	}
	if lines[1].IsError {
		t.Error("expected second line to be a regular log line")
	}
}

func TestPipelineWriteDoesNotBlock(t *testing.T) {
	out := make(chan Line, 4)
	p := NewPipeline(out)

	done := make(chan struct{})
	go func() {
		_, _ = p.Write([]byte("fast\n"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Write blocked on output channel")
	}

	p.Close()
	close(out)
	for range out {
	}
}

func TestPipelineRedactsSecrets(t *testing.T) {
	out := make(chan Line, 8)
	p := NewPipeline(out)

	raw := "export AWS_SECRET_ACCESS_KEY=AKIA1234567890123456\nghp_123456789012345678901234567890123456\npassword: \"supersecret\"\nnormal line\n"
	if _, err := p.Write([]byte(raw)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	p.Close()
	close(out)

	var lines []Line
	for line := range out {
		lines = append(lines, line)
	}

	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4: %#v", len(lines), lines)
	}
	if lines[0].Text != "export AWS_SECRET_ACCESS_KEY=[REDACTED_SECRET]" {
		t.Errorf("line 0 = %q, want %q", lines[0].Text, "export AWS_SECRET_ACCESS_KEY=[REDACTED_SECRET]")
	}
	if lines[1].Text != "[REDACTED_SECRET]" {
		t.Errorf("line 1 = %q, want %q", lines[1].Text, "[REDACTED_SECRET]")
	}
	if lines[2].Text != "password: [REDACTED_SECRET]" {
		t.Errorf("line 2 = %q, want %q", lines[2].Text, "password: [REDACTED_SECRET]")
	}
	if lines[3].Text != "normal line" {
		t.Errorf("line 3 = %q, want %q", lines[3].Text, "normal line")
	}
}

func TestPipelineIgnoresAltScreen(t *testing.T) {
	out := make(chan Line, 8)
	p := NewPipeline(out)

	raw := "normal start\n\x1b[?1049hhidden vim session\n\x1b[?1049lnormal end\n"
	if _, err := p.Write([]byte(raw)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	p.Close()
	close(out)

	var lines []Line
	for line := range out {
		lines = append(lines, line)
	}

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %#v", len(lines), lines)
	}
	if lines[0].Text != "normal start" {
		t.Errorf("line 0 = %q, want %q", lines[0].Text, "normal start")
	}
	if lines[1].Text != "normal end" {
		t.Errorf("line 1 = %q, want %q", lines[1].Text, "normal end")
	}
}

func TestPipelineMemoryBoundaryUnderBackpressure(t *testing.T) {
	out := make(chan Line, 1)
	p := NewPipeline(out)
	defer p.Close()

	p.out <- Line{Text: "fill"}

	for i := 0; i < 1000; i++ {
		_, err := p.Write([]byte("this line should be dropped because the channel is full\n"))
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	time.Sleep(50 * time.Millisecond)

	p.mu.Lock()
	bufLen := p.buf.Len()
	p.mu.Unlock()

	if bufLen > 1024 {
		t.Errorf("buffer length = %d, expected under 1024 bytes (lines not dropped/freed)", bufLen)
	}
}

func BenchmarkPipelineMemoryConstraints(b *testing.B) {
	out := make(chan Line, 512)
	p := NewPipeline(out)
	defer p.Close()

	go func() {
		for range out {
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Write([]byte("some logs and tracebacks to write sequential line outputs\n"))
	}
}


