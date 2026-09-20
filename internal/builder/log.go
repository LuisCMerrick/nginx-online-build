package builder

import (
	"fmt"
	"io"
	"nginx-builder/internal/config"
	"os"
	"sync"
)

// LogBroadcaster manages writing logs to disk and broadcasting to multiple live listeners (SSE).
type LogBroadcaster struct {
	mu        sync.RWMutex
	file      *os.File
	listeners map[chan string]struct{}
	closed    bool
	size      int64
	truncated bool
}

// NewLogBroadcaster creates a log broadcaster that writes to the given filePath.
func NewLogBroadcaster(filePath string) (*LogBroadcaster, error) {
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败 %s: %w", filePath, err)
	}

	return &LogBroadcaster{
		file:      f,
		listeners: make(map[chan string]struct{}),
	}, nil
}

// Write writes bytes to the underlying log file and broadcasts string chunks to active listeners.
func (b *LogBroadcaster) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return 0, io.ErrClosedPipe
	}

	if b.truncated {
		return len(p), nil
	}
	if b.size+int64(len(p)) > config.MaxLogBytes {
		b.truncated = true
		marker := []byte("\n[WARN] Build log size limit reached; further output omitted.\n")
		_, err = b.file.Write(marker)
		for ch := range b.listeners {
			select {
			case ch <- string(marker):
			default:
			}
		}
		return len(p), err
	}
	n, err = b.file.Write(p)
	b.size += int64(n)
	str := string(p)

	for ch := range b.listeners {
		select {
		case ch <- str:
		default:
			// listener channel full or slow, skip to prevent blocking build
		}
	}

	return n, err
}

// Subscribe attaches a listener channel for live log streaming.
func (b *LogBroadcaster) Subscribe() chan string {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan string, 100)
	if !b.closed {
		b.listeners[ch] = struct{}{}
	} else {
		close(ch)
	}
	return ch
}

// Unsubscribe detaches a listener channel.
func (b *LogBroadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.listeners[ch]; ok {
		delete(b.listeners, ch)
		close(ch)
	}
}

// Close closes the underlying log file and terminates all listener channels.
func (b *LogBroadcaster) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}
	b.closed = true

	for ch := range b.listeners {
		close(ch)
		delete(b.listeners, ch)
	}

	return b.file.Close()
}

// SubscribeWithHistory snapshots disk output and registers a listener under one lock.
func (b *LogBroadcaster) SubscribeWithHistory() (chan string, []string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, err := readLog(b.file.Name())
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan string, 100)
	if b.closed {
		close(ch)
	} else {
		b.listeners[ch] = struct{}{}
	}
	return ch, []string{string(data)}, nil
}

// limitedWriter drains excess command output without growing disk or memory use.
// Stdout and stderr may call Write concurrently.
type limitedWriter struct {
	mu        sync.Mutex
	writer    io.Writer
	remaining int64
	truncated bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	size := len(p)
	if int64(size) > w.remaining {
		p = p[:int(w.remaining)]
		w.truncated = true
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	return size, nil
}
func readLog(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, config.MaxLogBytes+128))
}
