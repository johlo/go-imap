package imapserver_test

import (
	"strings"
	"testing"
)

func TestAppendChecksStateBeforeLiteralContinuation(t *testing.T) {
	for _, literal := range []string{"{3}", "{3+}\r\nabc"} {
		c := regressionConn(t, regressionOptions(t))
		regressionLine(t, c)
		if err := c.PrintfLine("a APPEND INBOX %s", literal); err != nil {
			t.Fatal(err)
		}
		if line := regressionLine(t, c); !strings.HasPrefix(line, "a BAD ") {
			t.Fatal("requested literal before rejecting invalid state", line)
		}
		regressionReply(t, c, "NOOP", "OK")
	}
}
