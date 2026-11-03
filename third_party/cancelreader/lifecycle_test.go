package cancelreader

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLifecycleCloseCancelsThenJoinsRead(t *testing.T) {
	var lifecycle readerLifecycle
	entered, release := make(chan struct{}), make(chan struct{})
	canceled := make(chan struct{})
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		_, _ = lifecycle.read(func() (int, error) { close(entered); <-release; return 0, ErrCanceled })
	}()
	<-entered
	closeDone := make(chan error, 1)
	closeErr := errors.New("synthetic descriptor release failure")
	go func() {
		closeDone <- lifecycle.close(func() bool { close(canceled); return true }, func() error { return closeErr })
	}()
	<-canceled
	select {
	case <-closeDone:
		close(release)
		<-readDone
		t.Fatal("Close returned while Read still owned descriptor references")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-readDone
	if err := <-closeDone; !errors.Is(err, closeErr) {
		t.Fatalf("close cause changed: %v", err)
	}
	if _, err := lifecycle.read(func() (int, error) { t.Error("Read touched closed resources"); return 1, nil }); err != ErrCanceled {
		t.Fatalf("late Read = %v", err)
	}
}

func TestLifecycleCancellationAndCloseAreIdempotent(t *testing.T) {
	var lifecycle readerLifecycle
	var cancellations, releases int32
	cancel := func() bool { return lifecycle.cancel(func() bool { atomic.AddInt32(&cancellations, 1); return true }) }
	release := func() error { atomic.AddInt32(&releases, 1); return nil }
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := lifecycle.close(cancel, release); err != nil {
				t.Error(err)
			}
			if !cancel() {
				t.Error("successful cancellation changed result")
			}
		}()
	}
	workers.Wait()
	if cancellations != 1 || releases != 1 {
		t.Fatalf("cancel=%d close=%d", cancellations, releases)
	}
}
