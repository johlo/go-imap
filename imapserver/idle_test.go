package imapserver_test

import (
	"strings"
	"testing"
	"time"

	"github.com/johlo/go-imap/v2/imapserver"
)

type idleJoinSession struct {
	imapserver.Session
	exited, closed chan struct{}
	t              *testing.T
}

func (s *idleJoinSession) Idle(_ *imapserver.UpdateWriter, stop <-chan struct{}) error {
	<-stop
	close(s.exited)
	return nil
}
func (s *idleJoinSession) Close() error {
	select {
	case <-s.exited:
	default:
		s.t.Error("Close raced the IDLE worker")
	}
	close(s.closed)
	return s.Session.Close()
}
func TestIdleDisconnectJoinsSessionWorker(t *testing.T) {
	options := hookOptions(t)
	newSession := options.NewSession
	exited, closed := make(chan struct{}), make(chan struct{})
	options.NewSession = func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		s, g, err := newSession(c)
		return &idleJoinSession{Session: s, exited: exited, closed: closed, t: t}, g, err
	}
	c := hookConn(t, options)
	hookLine(t, c)
	hookReply(t, c, "LOGIN user password", "OK")
	if err := c.PrintfLine("a IDLE"); err != nil {
		t.Fatal(err)
	}
	if line := hookLine(t, c); !strings.HasPrefix(line, "+") {
		t.Fatal(line)
	}
	c.Close()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("IDLE session leaked after disconnect")
	}
}
