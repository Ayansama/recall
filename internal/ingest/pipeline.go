package ingest

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/acarl005/stripansi"
)

const notifyCapacity = 1

var (
	altScreenOnPatterns = [][]byte{
		[]byte("\x1b[?1049h"),
		[]byte("\x1b[?1047h"),
		[]byte("\x1b[?47h"),
	}
	altScreenOffPatterns = [][]byte{
		[]byte("\x1b[?1049l"),
		[]byte("\x1b[?1047l"),
		[]byte("\x1b[?47l"),
	}
)

// Line is a cleansed terminal line ready for persistence.
type Line struct {
	Text      string
	IsError   bool
	CreatedAt int64
}

// Pipeline strips ANSI codes from raw PTY bytes, splits on newlines, and
// forwards complete lines to the output channel from a background goroutine
// so that io.TeeReader never blocks on database back-pressure.
type Pipeline struct {
	mu          sync.Mutex
	buf         bytes.Buffer
	notify      chan struct{}
	out         chan<- Line
	done        chan struct{}
	wg          sync.WaitGroup
	once        sync.Once
	inAltScreen bool
}

// NewPipeline creates an ingestion pipeline that emits lines on out.
func NewPipeline(out chan<- Line) *Pipeline {
	p := &Pipeline{
		notify: make(chan struct{}, notifyCapacity),
		out:    out,
		done:   make(chan struct{}),
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.run()
	}()
	return p
}

// Write implements io.Writer for use as the TeeReader duplicate sink.
func (p *Pipeline) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	current := b
	for len(current) > 0 {
		if p.inAltScreen {
			idx := -1
			patLen := 0
			for _, pat := range altScreenOffPatterns {
				i := bytes.Index(current, pat)
				if i != -1 && (idx == -1 || i < idx) {
					idx = i
					patLen = len(pat)
				}
			}
			if idx == -1 {
				break
			}
			current = current[idx+patLen:]
			p.inAltScreen = false
		} else {
			idx := -1
			patLen := 0
			for _, pat := range altScreenOnPatterns {
				i := bytes.Index(current, pat)
				if i != -1 && (idx == -1 || i < idx) {
					idx = i
					patLen = len(pat)
				}
			}
			if idx == -1 {
				cleaned := stripansi.Strip(string(current))
				p.buf.WriteString(cleaned)
				break
			}
			chunkToLog := current[:idx]
			cleaned := stripansi.Strip(string(chunkToLog))
			p.buf.WriteString(cleaned)

			current = current[idx+patLen:]
			p.inAltScreen = true
		}
	}

	select {
	case p.notify <- struct{}{}:
	default:
	}

	return len(b), nil
}

// Close flushes any buffered partial line and stops the background processor.
func (p *Pipeline) Close() {
	p.once.Do(func() {
		close(p.done)
		p.wg.Wait()
	})
}

func (p *Pipeline) run() {
	for {
		select {
		case <-p.done:
			p.flushRemaining()
			return
		case <-p.notify:
			p.drainCompleteLines()
		}
	}
}

func (p *Pipeline) drainCompleteLines() {
	for {
		line, ok := p.extractLine()
		if !ok {
			return
		}
		redacted := RedactSecrets(line)
		select {
		case p.out <- Line{
			Text:      redacted,
			IsError:   IsErrorLine(redacted),
			CreatedAt: time.Now().UnixNano(),
		}:
		default:
			// Discard the line when queue is full to preserve memory bounds
		}
	}
}

func (p *Pipeline) extractLine() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	data := p.buf.Bytes()
	idx := bytes.IndexByte(data, '\n')
	if idx < 0 {
		return "", false
	}

	line := strings.TrimSuffix(string(data[:idx]), "\r")
	remainder := data[idx+1:]
	p.buf.Reset()
	p.buf.Write(remainder)

	return line, true
}

func (p *Pipeline) flushRemaining() {
	p.drainCompleteLines()

	p.mu.Lock()
	remaining := p.buf.String()
	p.buf.Reset()
	p.mu.Unlock()

	if remaining == "" {
		return
	}

	redacted := RedactSecrets(remaining)
	select {
	case p.out <- Line{
		Text:      redacted,
		IsError:   IsErrorLine(redacted),
		CreatedAt: time.Now().UnixNano(),
	}:
	default:
		// Discard to preserve memory bounds
	}
}

// Compile-time check that Pipeline satisfies io.Writer.
var _ io.Writer = (*Pipeline)(nil)
