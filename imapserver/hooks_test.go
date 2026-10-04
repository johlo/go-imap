package imapserver_test

import (
	"encoding/base64"
	"net"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johlo/go-imap/v2"
	"github.com/johlo/go-imap/v2/imapserver"
	"github.com/johlo/go-imap/v2/imapserver/imapmemserver"
)

func hookOptions(t *testing.T) *imapserver.Options {
	t.Helper()
	backend := imapmemserver.New()
	user := imapmemserver.NewUser("user", "password")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	backend.AddUser(user)
	return &imapserver.Options{InsecureAuth: true, NewSession: func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return backend.NewSession(), nil, nil
	}}
}
func hookConn(t *testing.T, options *imapserver.Options) *textproto.Conn {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := imapserver.New(options)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	t.Cleanup(func() { conn.Close(); server.Close(); <-done })
	return textproto.NewConn(conn)
}
func hookLine(t *testing.T, c *textproto.Conn) string {
	t.Helper()
	line, err := c.ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	return line
}
func hookReply(t *testing.T, c *textproto.Conn, command, status string) string {
	t.Helper()
	if err := c.PrintfLine("a %s", command); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for {
		line := hookLine(t, c)
		lines = append(lines, line)
		if strings.HasPrefix(line, "a ") {
			if !strings.HasPrefix(line, "a "+status+" ") {
				t.Fatal(line)
			}
			return strings.Join(lines, "\n")
		}
	}
}
func TestCommandMetadataAndCompletionHooks(t *testing.T) {
	options := hookOptions(t)
	var before, after atomic.Int32
	options.CommandHook = func(c *imapserver.Conn, cmd *imapserver.Command) error {
		if c.Command() != cmd || cmd.HookData != nil {
			t.Error("command context missing or stale")
		}
		if cmd.Name == "SELECT" && cmd.Mailbox != "INBOX" {
			t.Error("missing decoded mailbox", cmd.Mailbox)
		}
		cmd.HookData = "before"
		before.Add(1)
		return nil
	}
	options.ResponseHook = func(c *imapserver.Conn, cmd *imapserver.Command, r *imap.StatusResponse) *imap.StatusResponse {
		if cmd.HookData != "before" {
			t.Error("lost command context")
		}
		after.Add(1)
		if cmd.Name == "NOOP" {
			return &imap.StatusResponse{Type: imap.StatusResponseTypeNo, Text: "injected"}
		}
		return r
	}
	c := hookConn(t, options)
	hookLine(t, c)
	hookReply(t, c, `LOGIN user password`, "OK")
	hookReply(t, c, `SELECT INBOX`, "OK")
	hookReply(t, c, `NOOP`, "NO")
	if before.Load() != 3 || after.Load() != 3 {
		t.Fatal("hooks must run once per command", before.Load(), after.Load())
	}
}
func TestResponseHookCanWriteAndSuppressCompletion(t *testing.T) {
	options := hookOptions(t)
	options.ResponseHook = func(c *imapserver.Conn, cmd *imapserver.Command, r *imap.StatusResponse) *imap.StatusResponse {
		if cmd.Name == "NOOP" {
			if err := c.WriteRawResponse(cmd.Tag + " NO synthetic\r\n"); err != nil {
				t.Error(err)
			}
			return nil
		}
		return r
	}
	c := hookConn(t, options)
	hookLine(t, c)
	hookReply(t, c, "NOOP", "NO")
	hookReply(t, c, "CAPABILITY", "OK")
}
func TestGreetingAndCapabilitiesHooks(t *testing.T) {
	t.Run("raw", func(t *testing.T) {
		options := hookOptions(t)
		options.GreetingHook = func(c *imapserver.Conn) error {
			_ = c.WriteRawResponse("* BYE unavailable\r\n")
			return imapserver.ErrResponseHandled
		}
		c := hookConn(t, options)
		if line := hookLine(t, c); line != "* BYE unavailable" {
			t.Fatal(line)
		}
		if line, err := c.ReadLine(); err == nil {
			t.Fatal("extra greeting", line)
		}
	})
	t.Run("capabilities", func(t *testing.T) {
		options := hookOptions(t)
		options.CapabilitiesHook = func(_ *imapserver.Conn, caps []imap.Cap) []imap.Cap {
			var out []imap.Cap
			for _, cap := range caps {
				if cap != imap.CapSASLIR {
					out = append(out, cap)
				}
			}
			return out
		}
		c := hookConn(t, options)
		if line := hookLine(t, c); strings.Contains(line, "SASL-IR") {
			t.Fatal(line)
		}
		if reply := hookReply(t, c, "CAPABILITY", "OK"); strings.Contains(reply, "SASL-IR") {
			t.Fatal(reply)
		}
	})
}

type loginHookSession struct {
	imapserver.Session
	conn   *imapserver.Conn
	failed bool
}

func (s *loginHookSession) Login(user, password string) error {
	if !s.failed {
		s.failed = true
		if err := s.conn.WriteRawResponse(s.conn.Command().Tag + " NO try again\r\n"); err != nil {
			return err
		}
		return imapserver.ErrResponseHandled
	}
	return s.Session.Login(user, password)
}
func TestSASLLoginCanWriteResponseWithoutEncoderDeadlock(t *testing.T) {
	options := hookOptions(t)
	newSession := options.NewSession
	options.NewSession = func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		s, g, err := newSession(c)
		return &loginHookSession{Session: s, conn: c}, g, err
	}
	c := hookConn(t, options)
	hookLine(t, c)
	command := "AUTHENTICATE PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00user\x00password"))
	hookReply(t, c, command, "NO")
	hookReply(t, c, command, "OK")
}

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

func TestAppendHookRejectsBeforeSynchronizingLiteral(t *testing.T) {
	options := hookOptions(t)
	options.CommandHook = func(_ *imapserver.Conn, cmd *imapserver.Command) error {
		if cmd.Name == "APPEND" {
			if cmd.Mailbox != "INBOX" {
				t.Error("missing decoded APPEND mailbox")
			}
			return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "unavailable"}
		}
		return nil
	}
	c := hookConn(t, options)
	hookLine(t, c)
	hookReply(t, c, "LOGIN user password", "OK")
	if err := c.PrintfLine("a APPEND INBOX {100}"); err != nil {
		t.Fatal(err)
	}
	if line := hookLine(t, c); line != "a NO unavailable" {
		t.Fatal("requested literal instead of rejecting", line)
	}
	hookReply(t, c, "NOOP", "OK")
}
