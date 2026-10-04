# go-imap

## Mailarky fork

This public fork follows [emersion/go-imap v2](https://github.com/emersion/go-imap/tree/v2),
based on upstream commit `46e71ca79a54`. It retains the upstream MIT license and tests.
The `mailarky-v2` branch uses its own module path, `github.com/johlo/go-imap/v2`,
so consumers can pin a revision without a `replace` directive.

Optional `imapserver.Options` hooks support protocol testing:

- `GreetingHook` runs after creating the session, before the greeting.
- `CommandHook` runs once after decoding command arguments, before execution.
  `Command` exposes the tag, name, decoded mailbox argument, and application
  `HookData`; it does not retain credentials or message literals.
- `ResponseHook` can replace or suppress a tagged completion. FETCH writers
  have finished before the hook runs. Structured APPEND/COPY response codes
  remain intact when the hook leaves the response unchanged.
- `CapabilitiesHook` filters advertised capabilities without changing support.

`Conn.Session()` and `Conn.Command()` expose the current session and command.
`Conn.WriteRawResponse` serializes and flushes a custom reply; return
`ErrResponseHandled` to suppress the normal completion. Call it between
responses, never while a FETCH response writer is open. SASL callbacks run
outside the encoder lock. `Conn.Close()` disconnects without a reply.

The fork also joins IDLE workers on disconnect, deselects the old mailbox before
a SELECT hook can reject a new selection, and prevents enabling unadvertised
IMAP4rev2. Run `go test -race ./...` and `go vet ./...` when updating.


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
