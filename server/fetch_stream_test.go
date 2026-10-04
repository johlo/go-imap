package server

import (
	"errors"
	"testing"
	"time"

	"github.com/johlo/go-imap"
	"github.com/johlo/go-imap/backend"
)

type failingFetchMailbox struct {
	backend.Mailbox
	writerStarted <-chan struct{}
	returned      chan struct{}
	err           error
}

func (m *failingFetchMailbox) ListMessages(_ bool, _ *imap.SeqSet, _ []imap.FetchItem, ch chan<- *imap.Message) error {
	<-m.writerStarted
	close(ch)
	close(m.returned)
	return m.err
}

type blockedFetchConn struct {
	Conn
	ctx     Context
	started chan struct{}
	release chan struct{}
}

func (c *blockedFetchConn) Context() *Context { return &c.ctx }
func (c *blockedFetchConn) WriteResp(imap.WriterTo) error {
	close(c.started)
	<-c.release
	return nil
}

func TestFetchWaitsForWriterOnBackendError(t *testing.T) {
	c := &blockedFetchConn{started: make(chan struct{}), release: make(chan struct{})}
	fetchErr := errors.New("interrupted FETCH")
	m := &failingFetchMailbox{writerStarted: c.started, returned: make(chan struct{}), err: fetchErr}
	c.ctx.Mailbox = m
	done := make(chan error, 1)
	go func() { done <- (&Fetch{}).Handle(c) }()
	<-m.returned
	select {
	case err := <-done:
		close(c.release)
		t.Fatalf("FETCH returned while its writer was still active: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(c.release)
	select {
	case err := <-done:
		if err != fetchErr {
			t.Fatalf("FETCH error = %v, want %v", err, fetchErr)
		}
	case <-time.After(time.Second):
		t.Fatal("FETCH did not finish after its writer completed")
	}
}
