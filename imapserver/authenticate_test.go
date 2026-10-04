package imapserver_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/johlo/go-imap/v2"
	"github.com/johlo/go-imap/v2/imapserver"
)

type rejectingLoginSession struct {
	imapserver.Session
	conn *imapserver.Conn
}

func (s *rejectingLoginSession) Login(_, _ string) error {
	if err := s.conn.Bye("Authentication unavailable"); err != nil {
		return err
	}
	return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Unavailable"}
}

func TestSASLCallbackCanDisconnect(t *testing.T) {
	for _, initialResponse := range []bool{false, true} {
		options := hookOptions(t)
		newSession := options.NewSession
		options.NewSession = func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			s, g, err := newSession(c)
			return &rejectingLoginSession{Session: s, conn: c}, g, err
		}
		c := hookConn(t, options)
		hookLine(t, c)
		credentials := base64.StdEncoding.EncodeToString([]byte("\x00user\x00password"))
		if initialResponse {
			c.PrintfLine("a AUTHENTICATE PLAIN %s", credentials)
		} else {
			c.PrintfLine("a AUTHENTICATE PLAIN")
			if line := hookLine(t, c); !strings.HasPrefix(line, "+") {
				t.Fatal(line)
			}
			c.PrintfLine("%s", credentials)
		}
		if line := hookLine(t, c); line != "* BYE Authentication unavailable" {
			t.Fatal(line)
		}
		if line, err := c.ReadLine(); err == nil {
			t.Fatal("connection stayed open", line)
		}
	}
}
