package imapserver_test

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/johlo/go-imap/v2"
	"github.com/johlo/go-imap/v2/imapclient"
	"github.com/johlo/go-imap/v2/imapserver"
)

type metadataTestSession struct {
	imapserver.Session
	mu     sync.Mutex
	values map[string]map[string][]byte
}

func (s *metadataTestSession) GetMetadata(mailbox string, entries []string, depth imap.GetMetadataDepth) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := make(map[string][]byte)
	for key, value := range s.values[mailbox] {
		for _, entry := range entries {
			suffix := strings.TrimPrefix(key, entry+"/")
			if key == entry || suffix != key && (depth == imap.GetMetadataDepthInfinity || depth == imap.GetMetadataDepthOne && !strings.Contains(suffix, "/")) {
				values[key] = append([]byte{}, value...)
			}
		}
	}
	return values, nil
}
func (s *metadataTestSession) SetMetadata(mailbox string, entries map[string]*[]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values[mailbox] == nil {
		s.values[mailbox] = make(map[string][]byte)
	}
	for key, value := range entries {
		if value == nil {
			delete(s.values[mailbox], key)
		} else {
			s.values[mailbox][key] = append([]byte{}, (*value)...)
		}
	}
	return nil
}
func metadataOptions(t *testing.T) *imapserver.Options {
	opts := hookOptions(t)
	newSession := opts.NewSession
	opts.NewSession = func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		s, greeting, err := newSession(c)
		return &metadataTestSession{Session: s, values: make(map[string]map[string][]byte)}, greeting, err
	}
	opts.Caps = imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMetadata: {}, imap.CapUIDPlus: {}}
	return opts
}

func TestMetadataWire(t *testing.T) {
	c := hookConn(t, metadataOptions(t))
	hookLine(t, c)
	hookReply(t, c, `GETMETADATA "" /private/comment`, "BAD")
	hookReply(t, c, `SETMETADATA "" (/private/comment {4}`, "BAD") // no continuation before login
	hookReply(t, c, "SETMETADATA \"\" (/private/comment {4+}\r\nnope)", "BAD")
	hookReply(t, c, `LOGIN user password`, "OK")
	if got := hookReply(t, c, `GETMETADATA "" /private/comment`, "OK"); !strings.Contains(got, "NIL") {
		t.Fatal("unauthenticated write reached backend:", got)
	}
	if got := hookReply(t, c, "CAPABILITY", "OK"); !strings.Contains(got, " METADATA") || !strings.Contains(got, " UIDPLUS") {
		t.Fatal(got)
	}
	hookReply(t, c, `SETMETADATA "" (/PRIVATE/comment "hello" /private/comment/child "child" /private/comment/child/deep "deep" /private/empty "")`, "OK")
	for _, tc := range []struct{ command, want string }{
		{`GETMETADATA "" (/private/comment /private/empty /private/missing)`, `("/private/comment" "hello" "/private/empty" "" "/private/missing" NIL)`},
		{`GETMETADATA (DEPTH 1) "" /private/comment`, `("/private/comment" "hello" "/private/comment/child" "child")`},
		{`GETMETADATA (MAXSIZE 4 DEPTH infinity) "" /private/comment`, `("/private/comment/child/deep" "deep")`},
		{`GETMETADATA (MAXSIZE 0) "" /private/comment`, `[METADATA LONGENTRIES 5]`},
	} {
		if got := hookReply(t, c, tc.command, "OK"); !strings.Contains(got, tc.want) {
			t.Fatalf("%s: %s", tc.command, got)
		}
	}
	for _, command := range []string{
		`SETMETADATA "" (/private/comment "changed" /private//invalid "bad")`,
		`SETMETADATA "" (/private/comment "changed" /PRIVATE/comment "duplicate")`,
		`SETMETADATA "" (/private/vendor/alps "incomplete vendor path")`,
		`SETMETADATA "" ("/private/Key" "non-ASCII")`,
		`GETMETADATA (DEPTH 2) "" /private/comment`,
		`GETMETADATA "" ()`, `SETMETADATA "" ()`,
	} {
		hookReply(t, c, command, "BAD")
		hookReply(t, c, "NOOP", "OK")
	}
	if got := hookReply(t, c, `GETMETADATA "" /private/comment`, "OK"); !strings.Contains(got, `"hello"`) {
		t.Fatal("failed SET was not atomic:", got)
	}
	hookReply(t, c, `SETMETADATA "" (/private/comment nil)`, "OK")
	if got := hookReply(t, c, `GETMETADATA "" /private/comment`, "OK"); !strings.Contains(got, `"/private/comment" NIL`) {
		t.Fatal(got)
	}
	hookReply(t, c, `SETMETADATA INBOX (/private/comment "mailbox")`, "OK")
	if got := hookReply(t, c, `GETMETADATA "" /private/comment`, "OK"); !strings.Contains(got, "NIL") {
		t.Fatal("scopes mixed:", got)
	}
}

func TestMetadataLiteralsAndLimits(t *testing.T) {
	c := hookConn(t, metadataOptions(t))
	hookLine(t, c)
	hookReply(t, c, "LOGIN user password", "OK")
	if err := c.PrintfLine(`a SETMETADATA "" (/private/comment {5}`); err != nil {
		t.Fatal(err)
	}
	if got := hookLine(t, c); !strings.HasPrefix(got, "+ ") {
		t.Fatal(got)
	}
	if err := c.PrintfLine("hello)"); err != nil {
		t.Fatal(err)
	}
	if got := hookLine(t, c); !strings.HasPrefix(got, "a OK ") {
		t.Fatal(got)
	}
	hookReply(t, c, "SETMETADATA \"\" (/private/binary ~{3+}\r\na\x00b)", "OK")
	got := hookReply(t, c, `GETMETADATA "" /private/binary`, "OK")
	if !strings.Contains(got, "~{3}\na\x00b") {
		t.Fatal(got)
	}
	got = hookReply(t, c, fmt.Sprintf(`SETMETADATA "" (/private/comment {%d}`, imapserver.MetadataMaxSize+1), "NO")
	if !strings.Contains(got, "[METADATA MAXSIZE 65536]") {
		t.Fatal(got)
	}
	hookReply(t, c, "NOOP", "OK") // rejecting a synchronizing literal must not consume the next command
	hookReply(t, c, `GETMETADATA "" {4097}`, "NO")
	hookReply(t, c, "NOOP", "OK")
	hookReply(t, c, `SETMETADATA "" ({4097}`, "NO")
	hookReply(t, c, "NOOP", "OK")
	if got := hookReply(t, c, `GETMETADATA "" /private/comment`, "OK"); !strings.Contains(got, `"hello"`) {
		t.Fatal(got)
	}
}

func TestMetadataRejectsOversizedNonSyncLiteralAndCloses(t *testing.T) {
	c := hookConn(t, metadataOptions(t))
	hookLine(t, c)
	hookReply(t, c, "LOGIN user password", "OK")
	hookReply(t, c, `SETMETADATA "" (/private/comment {65537+}`, "NO")
	if line, err := c.ReadLine(); err == nil {
		t.Fatal("expected closed connection", line)
	} else if err, ok := err.(net.Error); ok && err.Timeout() {
		t.Fatal("connection did not close")
	}
}

func TestMetadataClientRoundTrip(t *testing.T) {
	c := imapclient.New(hookNetConn(t, metadataOptions(t)), nil)
	t.Cleanup(func() { c.Close() })
	if err := c.Login("user", "password").Wait(); err != nil {
		t.Fatal(err)
	}
	for _, value := range [][]byte{{}, []byte("plain"), []byte("a\x00b\r\n\xff"), bytes.Repeat([]byte("x"), imapserver.MetadataMaxSize)} {
		if err := c.SetMetadata("", map[string]*[]byte{"/private/vendor/alps/settings": &value}).Wait(); err != nil {
			t.Fatal(err)
		}
		data, err := c.GetMetadata("", []string{"/private/vendor/alps"}, &imap.GetMetadataOptions{Depth: imap.GetMetadataDepthInfinity}).Wait()
		if err != nil {
			t.Fatal(err)
		}
		got := data.Entries["/private/vendor/alps/settings"]
		if got == nil || !bytes.Equal(*got, value) {
			t.Fatalf("value did not round trip (%d bytes)", len(value))
		}
	}
	maxSize := uint32(0)
	data, err := c.GetMetadata("", []string{"/private/vendor/alps/settings"}, &imap.GetMetadataOptions{MaxSize: &maxSize}).Wait()
	if err != nil || data.LongEntries != imapserver.MetadataMaxSize || len(data.Entries) != 0 {
		t.Fatalf("%+v, %v", data, err)
	}
}

func TestMetadataCapabilitiesAndHooks(t *testing.T) {
	for _, serverOnly := range []bool{false, true} {
		opts := metadataOptions(t)
		if serverOnly {
			opts.Caps = imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMetadataServer: {}}
		}
		opts.CommandHook = func(_ *imapserver.Conn, cmd *imapserver.Command) error {
			if cmd.Name == "SETMETADATA" && cmd.Mailbox == "Archive" {
				return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "blocked by hook"}
			}
			return nil
		}
		c := hookConn(t, opts)
		hookLine(t, c)
		hookReply(t, c, "LOGIN user password", "OK")
		hookReply(t, c, `SETMETADATA Archive (/private/comment {5}`, "NO")
		hookReply(t, c, "NOOP", "OK")
		status := "OK"
		if serverOnly {
			status = "NO"
		}
		hookReply(t, c, `GETMETADATA INBOX /private/comment`, status)
		hookReply(t, c, `SETMETADATA "" (/private/comment "server")`, "OK")
	}
	opts := hookOptions(t) // advertising an extension without a backend cannot enable it
	opts.Caps = imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMetadata: {}}
	c := hookConn(t, opts)
	hookLine(t, c)
	hookReply(t, c, "LOGIN user password", "OK")
	if got := hookReply(t, c, "CAPABILITY", "OK"); strings.Contains(got, " METADATA") {
		t.Fatal(got)
	}
	hookReply(t, c, `GETMETADATA "" /private/comment`, "BAD")
}

func TestUIDPlusMappingsAndSelectiveExpunge(t *testing.T) {
	opts := hookOptions(t)
	opts.Caps = imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}}
	c := hookConn(t, opts)
	hookLine(t, c)
	hookReply(t, c, "LOGIN user password", "OK")
	for i := 1; i <= 3; i++ {
		got := hookReply(t, c, "APPEND INBOX {5+}\r\n\r\nabc", "OK")
		if !strings.Contains(got, fmt.Sprintf("[APPENDUID 1 %d]", i)) {
			t.Fatal(got)
		}
	}
	hookReply(t, c, "CREATE Archive", "OK")
	hookReply(t, c, "SELECT INBOX", "OK")
	if got := hookReply(t, c, "UID COPY 1,3 Archive", "OK"); !strings.Contains(got, "[COPYUID 2 1,3 1:2]") {
		t.Fatal(got)
	}
	if got := hookReply(t, c, "UID COPY 99 Archive", "OK"); strings.Contains(got, "COPYUID") {
		t.Fatal(got)
	}
	hookReply(t, c, `UID STORE 1:2 +FLAGS (\Deleted)`, "OK")
	hookReply(t, c, "UID EXPUNGE 2:3", "OK")
	if got := hookReply(t, c, "UID SEARCH ALL", "OK"); !strings.Contains(got, "* SEARCH 1 3") {
		t.Fatal("UID EXPUNGE removed wrong messages:", got)
	}
	hookReply(t, c, `UID STORE 3 +FLAGS (\Deleted)`, "OK")
	hookReply(t, c, "UID EXPUNGE 99:*", "OK")
	if got := hookReply(t, c, "UID SEARCH ALL", "OK"); !strings.Contains(got, "* SEARCH 1") || strings.Contains(got, "* SEARCH 1 3") {
		t.Fatal(got)
	}
	hookReply(t, c, "UID EXPUNGE *", "OK")
	if got := hookReply(t, c, "UID SEARCH ALL", "OK"); strings.Contains(got, "* SEARCH 1") {
		t.Fatal(got)
	}
}
