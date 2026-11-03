package cancelreader

import "sync"

// readerLifecycle keeps descriptor teardown behind every active read, including
// the wait and cancellation-pipe drain. Cancel runs before taking the read lock
// so it can wake a reader blocked in epoll/kqueue/select. Tea's forced shutdown
// does not itself join the read loop, so Close must provide that guarantee.
// The caller-owned input file is never closed by this lifecycle.
type readerLifecycle struct {
	mu         sync.Mutex
	closed     bool
	closeErr   error
	cancelOnce sync.Once
	cancelOK   bool
}

func (l *readerLifecycle) read(fn func() (int, error)) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, ErrCanceled
	}
	return fn()
}

func (l *readerLifecycle) cancel(fn func() bool) bool {
	// Repeated cancellation must not fill a pipe while Close is joining Read.
	l.cancelOnce.Do(func() { l.cancelOK = fn() })
	return l.cancelOK
}

func (l *readerLifecycle) close(cancel func() bool, release func() error) error {
	cancel()
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.closed = true
		l.closeErr = release()
	}
	return l.closeErr
}
