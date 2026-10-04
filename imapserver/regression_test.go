package imapserver_test

import (
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

func regressionOptions(t *testing.T) *imapserver.Options {
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
func regressionConn(t *testing.T, options *imapserver.Options) *textproto.Conn {
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
func regressionLine(t *testing.T, c *textproto.Conn) string {
	t.Helper()
	line, err := c.ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	return line
}
func regressionReply(t *testing.T, c *textproto.Conn, command, status string) string {
	t.Helper()
	if err := c.PrintfLine("a %s", command); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for {
		line := regressionLine(t, c)
		lines = append(lines, line)
		if strings.HasPrefix(line, "a ") {
			if !strings.HasPrefix(line, "a "+status+" ") {
				t.Fatal(line)
			}
			return strings.Join(lines, "\n")
		}
	}
}
