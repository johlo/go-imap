package imapserver_test

import (
	"strings"
	"testing"

	"github.com/johlo/go-imap/v2"
)

func TestEnableIMAP4rev2RequiresSupport(t *testing.T) {
	for _, supported := range []bool{false, true} {
		options := hookOptions(t)
		options.Caps = imap.CapSet{imap.CapIMAP4rev1: {}}
		if supported {
			options.Caps[imap.CapIMAP4rev2] = struct{}{}
		}
		c := hookConn(t, options)
		hookLine(t, c)
		hookReply(t, c, "LOGIN user password", "OK")
		reply := hookReply(t, c, "ENABLE IMAP4rev2 UTF8=ACCEPT", "OK")
		if strings.Contains(reply, "IMAP4rev2") != supported {
			t.Fatal("enabled unsupported revision", reply)
		}
		if !strings.Contains(reply, "UTF8=ACCEPT") {
			t.Fatal("lost UTF8 support", reply)
		}
	}
}
