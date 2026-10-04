package imapserver_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/big"
	"net/textproto"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johlo/go-imap/v2"
	"github.com/johlo/go-imap/v2/imapserver"
)

func TestResponseHookInPlaceEdits(t *testing.T) {
	for _, command := range []string{"NOOP", "LOGIN", "AUTHENTICATE", "APPEND", "COPY", "STARTTLS"} {
		t.Run(command, func(t *testing.T) {
			options := hookOptions(t)
			options.TLSConfig = &tls.Config{}
			options.ResponseHook = func(_ *imapserver.Conn, cmd *imapserver.Command, r *imap.StatusResponse) *imap.StatusResponse {
				if cmd.Name == command {
					r.Text = "edited completion"
					if command == "STARTTLS" {
						r.Type = imap.StatusResponseTypeNo
					}
				}
				return r
			}
			c := hookConn(t, options)
			hookLine(t, c)
			request, status := command, "OK"
			switch command {
			case "LOGIN":
				request += " user password"
			case "AUTHENTICATE":
				request += " PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00user\x00password"))
			case "APPEND", "COPY":
				hookReply(t, c, "LOGIN user password", "OK")
				hookReply(t, c, "APPEND INBOX {5+}\r\n\r\nabc", "OK")
				if command == "APPEND" {
					request += " INBOX {5+}\r\n\r\nabc"
				} else {
					hookReply(t, c, "SELECT INBOX", "OK")
					hookReply(t, c, "CREATE Archive", "OK")
					request += " 1 Archive"
				}
			case "STARTTLS":
				status = "NO"
			}
			if got := hookReply(t, c, request, status); !strings.Contains(got, "a "+status+" edited completion") {
				t.Fatal(got)
			}
			hookReply(t, c, "CAPABILITY", "OK")
		})
	}
}

// This map must cover every dispatcher case, so adding an upstream handler also
// requires deciding and testing where its hook runs. Rejecting from the hook
// lets the test exercise every parser without implementing optional backends.
func TestEveryCommandRunsHookOnce(t *testing.T) {
	commands := map[string]string{
		"NOOP": "", "CHECK": "", "LOGOUT": "", "CAPABILITY": "", "STARTTLS": "",
		"AUTHENTICATE": " PLAIN", "UNAUTHENTICATE": "", "LOGIN": " user password",
		"ENABLE": " UTF8=ACCEPT", "CREATE": " Archive", "DELETE": " Archive", "RENAME": " INBOX Archive",
		"SUBSCRIBE": " INBOX", "UNSUBSCRIBE": " INBOX", "STATUS": " INBOX (MESSAGES)",
		"LIST": ` "" "*"`, "LSUB": ` "" "*"`, "NAMESPACE": "", "IDLE": "",
		"SELECT": " INBOX", "EXAMINE": " INBOX", "CLOSE": "", "UNSELECT": "", "APPEND": " INBOX {0}",
		"FETCH": " 1 FLAGS", "UID FETCH": " 1 FLAGS", "EXPUNGE": "", "UID EXPUNGE": " 1",
		"STORE": ` 1 +FLAGS (\Seen)`, "UID STORE": ` 1 +FLAGS (\Seen)`,
		"COPY": " 1 Archive", "UID COPY": " 1 Archive", "MOVE": " 1 Archive", "UID MOVE": " 1 Archive",
		"SEARCH": " ALL", "UID SEARCH": " ALL",
	}
	source, err := parser.ParseFile(token.NewFileSet(), "conn.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, decl := range source.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "readCommand" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			name, ok := sw.Tag.(*ast.Ident)
			if !ok || name.Name != "name" {
				return true
			}
			for _, item := range sw.Body.List {
				for _, expr := range item.(*ast.CaseClause).List {
					name, err := strconv.Unquote(expr.(*ast.BasicLit).Value)
					if err != nil {
						t.Fatal(err)
					}
					found[name] = true
					if _, ok := commands[name]; !ok {
						t.Errorf("add hook coverage for command %s", name)
					}
				}
			}
			return false
		})
	}
	if len(found) != len(commands) {
		t.Fatalf("dispatcher has %d commands, fixtures have %d", len(found), len(commands))
	}
	for name, args := range commands {
		t.Run(name, func(t *testing.T) {
			options := hookOptions(t)
			var before, after atomic.Int32
			options.CommandHook = func(_ *imapserver.Conn, cmd *imapserver.Command) error {
				before.Add(1)
				if cmd.Name != name {
					t.Error("unexpected command", cmd.Name)
				}
				return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "hook rejected"}
			}
			options.ResponseHook = func(_ *imapserver.Conn, _ *imapserver.Command, r *imap.StatusResponse) *imap.StatusResponse {
				after.Add(1)
				return r
			}
			c := hookConn(t, options)
			hookLine(t, c)
			if got := hookReply(t, c, name+args, "NO"); !strings.Contains(got, "hook rejected") {
				t.Fatal(got)
			}
			if before.Load() != 1 || after.Load() != 1 {
				t.Fatal("hooks did not run exactly once", before.Load(), after.Load())
			}
		})
	}
}

func TestDecodeFailureSkipsCommandHook(t *testing.T) {
	options := hookOptions(t)
	var before, after atomic.Int32
	options.CommandHook = func(_ *imapserver.Conn, _ *imapserver.Command) error { before.Add(1); return nil }
	options.ResponseHook = func(_ *imapserver.Conn, _ *imapserver.Command, r *imap.StatusResponse) *imap.StatusResponse {
		after.Add(1)
		return r
	}
	c := hookConn(t, options)
	hookLine(t, c)
	hookReply(t, c, "LOGIN user password", "OK")
	before.Store(0)
	after.Store(0)
	hookReply(t, c, "FETCH invalid FLAGS", "BAD")
	hookReply(t, c, "UNKNOWN", "BAD")
	if before.Load() != 0 || after.Load() != 2 {
		t.Fatal(before.Load(), after.Load())
	}
}

type pollHookSession struct {
	imapserver.Session
	polls *atomic.Int32
}

func (s *pollHookSession) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	s.polls.Add(1)
	return s.Session.Poll(w, allowExpunge)
}
func TestCommandHookDoesNotAddPolls(t *testing.T) {
	for _, hooked := range []bool{false, true} {
		options := hookOptions(t)
		var polls atomic.Int32
		newSession := options.NewSession
		options.NewSession = func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			s, g, err := newSession(c)
			return &pollHookSession{Session: s, polls: &polls}, g, err
		}
		if hooked {
			options.CommandHook = func(_ *imapserver.Conn, _ *imapserver.Command) error {
				if polls.Load() != 0 {
					t.Error("polled before command")
				}
				return nil
			}
		}
		c := hookConn(t, options)
		hookLine(t, c)
		hookReply(t, c, "LOGIN user password", "OK")
		polls.Store(0)
		hookReply(t, c, "NOOP", "OK")
		if polls.Load() != 1 {
			t.Fatal("NOOP must poll once after execution", hooked, polls.Load())
		}
	}
}

func TestResponseHookPreservesUnchangedStructuredCompletions(t *testing.T) {
	options := hookOptions(t)
	options.ResponseHook = func(_ *imapserver.Conn, _ *imapserver.Command, r *imap.StatusResponse) *imap.StatusResponse {
		copy := *r
		return &copy
	}
	c := hookConn(t, options)
	hookLine(t, c)
	if got := hookReply(t, c, "LOGIN user password", "OK"); !strings.Contains(got, "[CAPABILITY ") {
		t.Fatal(got)
	}
	if got := hookReply(t, c, "APPEND INBOX {5+}\r\n\r\nabc", "OK"); !strings.Contains(got, "[APPENDUID ") {
		t.Fatal(got)
	}
	hookReply(t, c, "SELECT INBOX", "OK")
	hookReply(t, c, "CREATE Archive", "OK")
	if got := hookReply(t, c, "COPY 1 Archive", "OK"); !strings.Contains(got, "[COPYUID ") {
		t.Fatal(got)
	}
}

func TestEditedStartTLSSuccessStillUpgrades(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	options := hookOptions(t)
	options.TLSConfig = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}}
	options.ResponseHook = func(_ *imapserver.Conn, cmd *imapserver.Command, r *imap.StatusResponse) *imap.StatusResponse {
		if cmd.Name == "STARTTLS" {
			r.Text = "custom TLS completion"
		}
		return r
	}
	raw := hookNetConn(t, options)
	c := textproto.NewConn(raw)
	hookLine(t, c)
	if got := hookReply(t, c, "STARTTLS", "OK"); !strings.Contains(got, "custom TLS completion") {
		t.Fatal(got)
	}
	secure := tls.Client(raw, &tls.Config{RootCAs: roots, ServerName: "localhost"})
	if err := secure.Handshake(); err != nil {
		t.Fatal(err)
	}
	hookReply(t, textproto.NewConn(secure), "LOGIN user password", "OK")
}

func TestHookDoesNotRenumberBeforeCopyOrMove(t *testing.T) {
	for _, command := range []string{"COPY", "MOVE"} {
		t.Run(command, func(t *testing.T) {
			options := hookOptions(t)
			options.Caps = imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMove: {}}
			options.CommandHook = func(_ *imapserver.Conn, _ *imapserver.Command) error { return nil }
			first := hookConn(t, options)
			hookLine(t, first)
			hookReply(t, first, "LOGIN user password", "OK")
			for _, body := range []string{"A", "B", "C"} {
				raw := "Subject: " + body + "\r\n\r\n" + body
				hookReply(t, first, fmt.Sprintf("APPEND INBOX {%d+}\r\n%s", len(raw), raw), "OK")
			}
			hookReply(t, first, "CREATE Archive", "OK")
			hookReply(t, first, "SELECT INBOX", "OK")
			second := hookConn(t, options)
			hookLine(t, second)
			hookReply(t, second, "LOGIN user password", "OK")
			hookReply(t, second, "SELECT INBOX", "OK")
			hookReply(t, second, `STORE 1 +FLAGS (\Deleted)`, "OK")
			hookReply(t, second, "EXPUNGE", "OK")
			hookReply(t, first, command+" 2 Archive", "OK")
			hookReply(t, first, "SELECT Archive", "OK")
			if got := hookReply(t, first, "FETCH 1 BODY.PEEK[HEADER.FIELDS (SUBJECT)]", "OK"); !strings.Contains(got, "Subject: B") {
				t.Fatal("wrong message after external expunge", got)
			}
		})
	}
}
