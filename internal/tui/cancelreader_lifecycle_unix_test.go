//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package tui

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/muesli/cancelreader"
)

// Retain the real descriptor interface, but hold a read in progress to make the
// Close/Read ownership contract observable without scheduler luck or private
// dependency fields. All data and descriptors belong to this test.
type heldTerminalFile struct {
	*os.File
	entered, release chan struct{}
	once             sync.Once
}

func (f *heldTerminalFile) Read(p []byte) (int, error) {
	f.once.Do(func() { close(f.entered) })
	<-f.release
	return f.File.Read(p)
}

func TestCancelReaderCloseJoinsReadAndRetainsCallerFile(t *testing.T) {
	input, output, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	defer input.Close()
	defer output.Close()
	source := &heldTerminalFile{File: input, entered: make(chan struct{}), release: make(chan struct{})}
	reader, readerErr := cancelreader.NewReader(source)
	if readerErr != nil {
		t.Fatal(readerErr)
	}
	// Deferred cleanup releases the deliberate gate before closing, even on failure.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(source.release) }) }
	defer func() { release(); _ = reader.Close() }()
	if _, writeErr := output.Write([]byte{'a'}); writeErr != nil {
		t.Fatal(writeErr)
	}
	readDone := make(chan struct{})
	go func() { defer close(readDone); var b [1]byte; _, _ = reader.Read(b[:]) }()
	select {
	case <-source.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("test reader did not acquire the input")
	}
	if !reader.Cancel() {
		t.Fatal("owned file cancellation was unavailable")
	}
	closed := make(chan error, 1)
	go func() { closed <- reader.Close() }()
	select {
	case err := <-closed:
		release()
		<-readDone
		t.Fatalf("Close returned before active Read released ownership: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("read did not finish")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not join the reader")
	}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, cancelreader.ErrCanceled) {
		t.Fatalf("closed reader accepted another read: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if err := input.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte{'b'}); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := input.Read(b[:]); err != nil || b[0] != 'b' {
		t.Fatalf("caller descriptor closed or input stolen: %q %v", b, err)
	}
}
