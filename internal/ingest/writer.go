package ingest

import (
	"sync"
	"time"

	"recall/internal/storage"
)

const (
	batchSize     = 50
	flushInterval = 100 * time.Millisecond
	lineCapacity  = 512
)

// BatchedWriter collects lines from a channel and flushes them to SQLite in
// batches of 50 rows or every 100 ms, whichever comes first.
type BatchedWriter struct {
	db        *storage.DB
	sessionID string
	ch        chan Line
	wg        sync.WaitGroup
}

// NewBatchedWriter starts the background flush loop and returns the writer
// together with the line channel the pipeline should target.
func NewBatchedWriter(db *storage.DB, sessionID string) (*BatchedWriter, chan Line) {
	ch := make(chan Line, lineCapacity)
	w := &BatchedWriter{
		db:        db,
		sessionID: sessionID,
		ch:        ch,
	}
	w.wg.Add(1)
	go w.loop()
	return w, ch
}

// Close drains the line channel, flushes remaining rows, and stops the loop.
func (w *BatchedWriter) Close() {
	close(w.ch)
	w.wg.Wait()
}

func (w *BatchedWriter) loop() {
	defer w.wg.Done()

	batch := make([]storage.LogLineRecord, 0, batchSize)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		_ = w.db.InsertLogLines(w.sessionID, batch)
		batch = batch[:0]
	}

	for {
		select {
		case line, ok := <-w.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, storage.LogLineRecord{
				Text:      line.Text,
				IsError:   line.IsError,
				CreatedAt: line.CreatedAt,
			})
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
