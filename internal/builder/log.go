package builder

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// LogBroadcaster manages writing logs to disk and broadcasting to multiple live listeners (SSE).
type LogBroadcaster struct {
	mu        sync.RWMutex
	file      *os.File
	listeners map[chan string]struct{}
	closed    bool
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

	n, err = b.file.Write(p)
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
