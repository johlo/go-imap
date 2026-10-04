package imapserver_test

import (
	"strings"
	"testing"
)

func TestAppendChecksStateBeforeLiteralContinuation(t *testing.T) {
	for _, literal := range []string{"{3}", "{3+}\r\nabc"} {
		c := hookConn(t, hookOptions(t))
		hookLine(t, c)
		if err := c.PrintfLine("a APPEND INBOX %s", literal); err != nil {
			t.Fatal(err)
		}
		if line := hookLine(t, c); !strings.HasPrefix(line, "a BAD ") {
			t.Fatal("requested literal before rejecting invalid state", line)
		}
		hookReply(t, c, "NOOP", "OK")
	}
}
