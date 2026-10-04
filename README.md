# go-imap

## Mailarky fork

This public fork of [emersion/go-imap](https://github.com/emersion/go-imap)
is based on **v1.2.1**, not the upstream v2 branch. The `mailarky` branch adds
server hooks used by [Mailarky](https://github.com/johlo/mailarky) for
protocol fault injection. Upstream packages, tests and the MIT license remain.

- `Server.GreetingHook` runs after implicit TLS negotiation and before the
  greeting. Return an error to stop the connection after any custom response.
- `Server.CommandHook` runs before normal handling; errors use ordinary IMAP
  status handling, including `imap.ErrStatusResp` for a custom or suppressed reply.
- `Server.ResponseHook` can replace or suppress a command's completion response.
- `Server.LoginHook` intercepts LOGIN and SASL PLAIN after the username is known
  and before credentials are checked.
- `Server.CapabilitiesHook` filters advertised lists without changing internal
  authentication/feature checks.
- `Context.Command` and `Context.HookData` are available until the command ends.
  Response writes flush before releasing callers that may close the socket.

`Server.IdleHook` can own IDLE continuation, updates and DONE. Mailboxes that
implement `backend.MailboxPoller` own selected-mailbox updates, so the server
does not synthesize duplicate EXPUNGE/EXISTS/FETCH responses. CLOSE preserves
deleted messages in a read-only (EXAMINE) session.

Hooks are optional; configure them before serving connections. Normal behavior
is preserved when they are nil. No sandbox-specific fault registry is included.

The module path is `github.com/johlo/go-imap`. Consumers import this fork
directly and pin a revision with `go get`; no `replace` directive is needed.
Run `go test -race ./...` and `go vet ./...` when updating.

[![godocs.io](https://godocs.io/github.com/johlo/go-imap?status.svg)](https://godocs.io/github.com/johlo/go-imap)
[![builds.sr.ht status](https://builds.sr.ht/~emersion/go-imap/commits/master.svg)](https://builds.sr.ht/~emersion/go-imap/commits/master?)

An [IMAP4rev1](https://tools.ietf.org/html/rfc3501) library written in Go. It
can be used to build a client and/or a server.

## Usage

### Client [![godocs.io](https://godocs.io/github.com/johlo/go-imap/client?status.svg)](https://godocs.io/github.com/johlo/go-imap/client)

```go
package main

import (
	"log"

	"github.com/johlo/go-imap/client"
	"github.com/johlo/go-imap"
)

func main() {
	log.Println("Connecting to server...")

	// Connect to server
	c, err := client.DialTLS("mail.example.org:993", nil)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Connected")

	// Don't forget to logout
	defer c.Logout()

	// Login
	if err := c.Login("username", "password"); err != nil {
		log.Fatal(err)
	}
	log.Println("Logged in")

	// List mailboxes
	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)
	go func () {
		done <- c.List("", "*", mailboxes)
	}()

	log.Println("Mailboxes:")
	for m := range mailboxes {
		log.Println("* " + m.Name)
	}

	if err := <-done; err != nil {
		log.Fatal(err)
	}

	// Select INBOX
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Flags for INBOX:", mbox.Flags)

	// Get the last 4 messages
	from := uint32(1)
	to := mbox.Messages
	if mbox.Messages > 3 {
		// We're using unsigned integers here, only subtract if the result is > 0
		from = mbox.Messages - 3
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, to)

	messages := make(chan *imap.Message, 10)
	done = make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope}, messages)
	}()

	log.Println("Last 4 messages:")
	for msg := range messages {
		log.Println("* " + msg.Envelope.Subject)
	}

	if err := <-done; err != nil {
		log.Fatal(err)
	}

	log.Println("Done!")
}
```

### Server [![godocs.io](https://godocs.io/github.com/johlo/go-imap/server?status.svg)](https://godocs.io/github.com/johlo/go-imap/server)

```go
package main

import (
	"log"

	"github.com/johlo/go-imap/server"
	"github.com/johlo/go-imap/backend/memory"
)

func main() {
	// Create a memory backend
	be := memory.New()

	// Create a new server
	s := server.New(be)
	s.Addr = ":1143"
	// Since we will use this server for testing only, we can allow plain text
	// authentication over unencrypted connections
	s.AllowInsecureAuth = true

	log.Println("Starting IMAP server at localhost:1143")
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
```

You can now use `telnet localhost 1143` to manually connect to the server.

## Extensions

Support for several IMAP extensions is included in go-imap itself. This
includes:

* [APPENDLIMIT](https://tools.ietf.org/html/rfc7889)
* [CHILDREN](https://tools.ietf.org/html/rfc3348)
* [ENABLE](https://tools.ietf.org/html/rfc5161)
* [IDLE](https://tools.ietf.org/html/rfc2177)
* [IMPORTANT](https://tools.ietf.org/html/rfc8457)
* [LITERAL+](https://tools.ietf.org/html/rfc7888)
* [MOVE](https://tools.ietf.org/html/rfc6851)
* [SASL-IR](https://tools.ietf.org/html/rfc4959)
* [SPECIAL-USE](https://tools.ietf.org/html/rfc6154)
* [UNSELECT](https://tools.ietf.org/html/rfc3691)

Support for other extensions is provided via separate packages. See below.

## Extending go-imap

### Extensions

Commands defined in IMAP extensions are available in other packages. See [the
wiki](https://github.com/johlo/go-imap/wiki/Using-extensions#using-client-extensions)
to learn how to use them.

* [COMPRESS](https://github.com/johlo/go-imap-compress)
* [ID](https://github.com/ProtonMail/go-imap-id)
* [METADATA](https://github.com/johlo/go-imap-metadata)
* [NAMESPACE](https://github.com/foxcpp/go-imap-namespace)
* [QUOTA](https://github.com/johlo/go-imap-quota)
* [SORT and THREAD](https://github.com/johlo/go-imap-sortthread)
* [UIDPLUS](https://github.com/johlo/go-imap-uidplus)

### Server backends

* [Memory](https://github.com/johlo/go-imap/tree/master/backend/memory) (for testing)
* [Multi](https://github.com/johlo/go-imap-multi)
* [PGP](https://github.com/johlo/go-imap-pgp)
* [Proxy](https://github.com/johlo/go-imap-proxy)
* [Notmuch](https://github.com/stbenjam/go-imap-notmuch) - Experimental gateway for [Notmuch](https://notmuchmail.org/)

### Related projects

* [go-message](https://github.com/emersion/go-message) - parsing and formatting MIME and mail messages
* [go-msgauth](https://github.com/emersion/go-msgauth) - handle DKIM, DMARC and Authentication-Results
* [go-pgpmail](https://github.com/emersion/go-pgpmail) - decrypting and encrypting mails with OpenPGP
* [go-sasl](https://github.com/emersion/go-sasl) - sending and receiving SASL authentications
* [go-smtp](https://github.com/emersion/go-smtp) - building SMTP clients and servers

## License

MIT
