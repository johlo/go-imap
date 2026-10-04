package server_test

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
)

func hookIMAPConn(t *testing.T, s *server.Server) (*textproto.Conn, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(l) }()
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(3 * time.Second))
	return textproto.NewConn(c), func() { c.Close(); s.Close(); <-done }
}
func hookIMAPLine(t *testing.T, c *textproto.Conn) string {
	t.Helper()
	line, err := c.ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	return line
}
func hookIMAPCommand(t *testing.T, c *textproto.Conn, command, status string) string {
	t.Helper()
	if err := c.PrintfLine("a %s", command); err != nil {
		t.Fatal(err)
	}
	for {
		line := hookIMAPLine(t, c)
		if strings.HasPrefix(line, "a ") {
			if !strings.HasPrefix(line, "a "+status+" ") {
				t.Fatal(line)
			}
			return line
		}
	}
}

func TestCommandAndResponseHooks(t *testing.T) {
	s := server.New(memory.New())
	s.AllowInsecureAuth = true
	s.CommandHook = func(c server.Conn, cmd *imap.Command) error {
		if c.Context().Command != cmd || c.Context().HookData != nil {
			t.Error("command context missing or stale")
		}
		c.Context().HookData = cmd.Name
		if cmd.Name == "NOOP" {
			return &imap.ErrStatusResp{Resp: &imap.StatusResp{Type: imap.StatusRespNo, Info: "command hook"}}
		}
		return nil
	}
	s.ResponseHook = func(c server.Conn, cmd *imap.Command, r *imap.StatusResp) *imap.StatusResp {
		if c.Context().HookData != cmd.Name {
			t.Error("hook metadata lost")
		}
		if cmd.Name == "NOOP" {
			t.Error("rejected command reached response hook")
		}
		r.Info = "response hook"
		return r
	}
	c, closeConn := hookIMAPConn(t, s)
	defer closeConn()
	hookIMAPLine(t, c)
	if got := hookIMAPCommand(t, c, "NOOP", "NO"); !strings.Contains(got, "command hook") {
		t.Fatal(got)
	}
	if got := hookIMAPCommand(t, c, "CAPABILITY", "OK"); !strings.Contains(got, "response hook") {
		t.Fatal(got)
	}
}

func TestLoginHooksAndCapabilityFiltering(t *testing.T) {
	for _, command := range []string{"LOGIN username password", "AUTHENTICATE PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00username\x00password"))} {
		t.Run(strings.Fields(command)[0], func(t *testing.T) {
			s := server.New(memory.New())
			s.AllowInsecureAuth = true
			attempts := 0
			s.LoginHook = func(c server.Conn, username string) error {
				if username != "username" || c.Context().Command == nil {
					t.Error("missing login context")
				}
				attempts++
				if attempts == 1 {
					return &imap.ErrStatusResp{Resp: &imap.StatusResp{Type: imap.StatusRespNo, Code: "UNAVAILABLE", Info: "retry"}}
				}
				return nil
			}
			s.CapabilitiesHook = func(c server.Conn, caps []string) []string {
				filtered := make([]string, 0, len(caps))
				for _, cap := range caps {
					if cap != "AUTH=PLAIN" {
						filtered = append(filtered, cap)
					}
				}
				return filtered
			}
			c, closeConn := hookIMAPConn(t, s)
			defer closeConn()
			if greeting := hookIMAPLine(t, c); strings.Contains(greeting, "AUTH=PLAIN") {
				t.Fatal(greeting)
			}
			if got := hookIMAPCommand(t, c, command, "NO"); !strings.Contains(got, "[UNAVAILABLE]") {
				t.Fatal(got)
			}
			// An omitted advertisement must not disable the actual auth handler.
			hookIMAPCommand(t, c, command, "OK")
		})
	}
}

func TestGreetingHookFlushesBeforeDisconnect(t *testing.T) {
	s := server.New(memory.New())
	s.GreetingHook = func(c server.Conn) error {
		if err := c.WriteResp(&imap.StatusResp{Type: imap.StatusRespBye, Info: "unavailable"}); err != nil {
			return err
		}
		c.Close()
		return &imap.ErrStatusResp{}
	}
	c, closeConn := hookIMAPConn(t, s)
	defer closeConn()
	if got := hookIMAPLine(t, c); got != "* BYE unavailable" {
		t.Fatal(got)
	}
	if line, err := c.ReadLine(); err == nil {
		t.Fatal("socket still open", line)
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("socket did not close")
	}
}

func TestResponseHookCanSuppressCompletion(t *testing.T) {
	s := server.New(memory.New())
	s.ResponseHook = func(c server.Conn, cmd *imap.Command, r *imap.StatusResp) *imap.StatusResp {
		c.WriteResp(&imap.StatusResp{Tag: cmd.Tag, Type: imap.StatusRespBad, Info: fmt.Sprintf("replaced %s", cmd.Name)})
		return nil
	}
	c, closeConn := hookIMAPConn(t, s)
	defer closeConn()
	hookIMAPLine(t, c)
	for i := 0; i < 2; i++ {
		if got := hookIMAPCommand(t, c, "NOOP", "BAD"); got != "a BAD replaced NOOP" {
			t.Fatal(got)
		}
	}
}
