# go-imap

## Mailarky fork

This public fork follows [emersion/go-imap v2](https://github.com/emersion/go-imap/tree/v2),
based on upstream commit `46e71ca79a54`. It retains the upstream MIT license and tests.
The `imap-v2-protocol-hooks` branch uses its own module path, `github.com/johlo/go-imap/v2`,
so consumers can pin a revision without a `replace` directive.

Optional `imapserver.Options` hooks support protocol testing:

- `GreetingHook` runs after creating the session, before the greeting.
- `CommandHook` runs once after decoding command arguments, before execution.
  It does not run when argument decoding fails or the command is unknown.
  It does not add any mailbox polling or change when EXPUNGE is allowed.
  `Command` exposes the tag, name, decoded mailbox argument, and application
  `HookData`; it does not retain credentials or message literals.
- `ResponseHook` receives a copy of a tagged completion and may edit it in
  place, replace it, or return nil to suppress it. It also sees BAD replies
  for unknown commands and argument decoding failures, even when CommandHook
  did not run. FETCH writers have finished before the hook runs. Structured
  APPEND/COPY response codes remain intact when the response fields are
  unchanged. An edited successful STARTTLS reply still starts TLS; suppressing
  it or changing its status to NO/BAD prevents the upgrade.
- `CapabilitiesHook` filters advertised capabilities without changing support.

`Conn.Session()` and `Conn.Command()` expose the current session and command.
`Conn.WriteRawResponse` serializes and flushes a custom reply; return
`ErrResponseHandled` to suppress the normal completion. Call it between
responses, never while a FETCH response writer is open. SASL callbacks run
outside the encoder lock. `Conn.Close()` disconnects without a reply.

The fork also joins IDLE workers on disconnect, deselects the old mailbox before
a SELECT hook can reject a new selection, and prevents enabling unadvertised
IMAP4rev2. The IDLE, ENABLE, APPEND-state, and SASL-lock fixes are isolated in
four commits on [server-fixes](https://github.com/johlo/go-imap/tree/server-fixes),
using the upstream module path for human review and submission. Published fork
history has not been rewritten. Keyed go-message header literals remain because
unkeyed literals fail `go vet`; they are separate from the protocol behavior.
Run `go test -race ./...` and `go vet ./...` when updating.

### METADATA and UIDPLUS

The server supports [METADATA (RFC 5464)](https://www.rfc-editor.org/rfc/rfc5464)
through the optional `imapserver.SessionMetadata` interface. Set
`imap.CapMetadata` in `Options.Caps` for server and mailbox annotations, or
`imap.CapMetadataServer` for server annotations alone. The capability is only
advertised when the session implements the interface.

The protocol layer handles case-insensitive entry names, NIL deletion, empty
and binary values, DEPTH, MAXSIZE, and LONGENTRIES. Backends supply storage,
access control, atomic updates, and mailbox rename/delete behavior. Each
SETMETADATA command accepts up to 128 entries, each at most 64 KiB; backends
must enforce their own storage limits. Oversized non-synchronizing literals
close the connection after the error to avoid parsing payload as commands.
Unsolicited metadata change notifications are not implemented.

GETMETADATA and SETMETADATA participate in the command and response hooks.
Like APPEND, SETMETADATA runs its command hook before accepting its first value
literal, so a hook can reject an upload before the client sends it. For commands
without literals the hook runs after argument decoding. Metadata values are
never added to `Command`. The client also supports literal8 values, reports
`GetMetadataData.LongEntries`, and sends options before the mailbox as specified
by the [verified RFC errata](https://www.rfc-editor.org/errata/rfc5464).

[UIDPLUS (RFC 4315)](https://www.rfc-editor.org/rfc/rfc4315) uses the existing
`Session.Append`, `Session.Copy`, and `Session.Expunge` methods. Enable
`imap.CapUIDPlus` for IMAP4rev1 and return the assigned UIDs and destination
UIDVALIDITY from the backend. Empty copies omit COPYUID. The in-memory backend
supports selective UID EXPUNGE, including wildcard ranges. Wire tests cover
APPENDUID, sparse COPYUID mappings, empty copies, and selective expunge.


[![Go Reference](https://pkg.go.dev/badge/github.com/johlo/go-imap/v2.svg)](https://pkg.go.dev/github.com/johlo/go-imap/v2)

An [IMAP4rev2] library for Go.

> **Note**
> This is the README for go-imap v2. This new major version is still in
> development. For go-imap v1, see the [v1 branch].

## Usage

To add go-imap to your project, run:

    go get github.com/johlo/go-imap/v2

Documentation and examples for the module are available here:

- [Client docs]
- [Server docs]

## Contributing

See [CONTRIBUTING.md] for contribution guidelines.

## License

MIT

[IMAP4rev2]: https://www.rfc-editor.org/rfc/rfc9051.html
[v1 branch]: https://github.com/emersion/go-imap/tree/v1
[Client docs]: https://pkg.go.dev/github.com/johlo/go-imap/v2/imapclient
[Server docs]: https://pkg.go.dev/github.com/johlo/go-imap/v2/imapserver
[CONTRIBUTING.md]: https://github.com/emersion/.github/blob/main/CONTRIBUTING.md
