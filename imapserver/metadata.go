package imapserver

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/johlo/go-imap/v2"
	"github.com/johlo/go-imap/v2/internal/imapwire"
)

// MetadataMaxSize limits each SETMETADATA value, including binary literals.
const MetadataMaxSize = 64 * 1024

// MetadataMaxEntries limits entries in a single command. Backends may impose
// additional limits on stored annotations, subject to RFC 5464's minimums.
const MetadataMaxEntries = 128

func metadataError(code, text string) error {
	return &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCode("METADATA " + code), Text: text}
}

func validMetadataEntry(name string, query bool) bool {
	if strings.Contains(name, "//") || strings.HasSuffix(name, "/") {
		return false
	}
	for _, ch := range []byte(name) {
		if ch <= 0x19 || ch > 0x7f || ch == '*' || ch == '%' {
			return false
		}
	}
	parts := strings.Split(strings.ToLower(name), "/")
	if len(parts) < 2 || parts[0] != "" || (parts[1] != "private" && parts[1] != "shared") {
		return false
	}
	return query || (len(parts) >= 3 && (parts[2] != "vendor" || len(parts) >= 5))
}

func (c *Conn) metadataSession(mailbox string) (SessionMetadata, error) {
	sess, ok := c.session.(SessionMetadata)
	caps := c.server.options.caps()
	if !ok || (!caps.Has(imap.CapMetadata) && !caps.Has(imap.CapMetadataServer)) {
		return nil, newClientBugError("METADATA is not supported")
	}
	if mailbox != "" && !caps.Has(imap.CapMetadata) {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Only server metadata is supported"}
	}
	return sess, nil
}

func (c *Conn) handleGetMetadata(tag string, dec *imapwire.Decoder) error {
	dec.CheckBufferedLiteralFunc = c.checkMetadataNameLiteral
	if !dec.ExpectSP() {
		return dec.Err()
	}
	var options imap.GetMetadataOptions
	seen := make(map[string]bool)
	isList, err := dec.List(func() error {
		var name, value string
		if !dec.ExpectAtom(&name) || !dec.ExpectSP() {
			return dec.Err()
		}
		name = strings.ToUpper(name)
		if seen[name] {
			return newClientBugError("Duplicate GETMETADATA option")
		}
		seen[name] = true
		switch name {
		case "MAXSIZE":
			var size uint32
			if !dec.ExpectNumber(&size) {
				return dec.Err()
			}
			options.MaxSize = &size
		case "DEPTH":
			if !dec.ExpectAtom(&value) {
				return dec.Err()
			}
			switch strings.ToLower(value) {
			case "0":
				options.Depth = imap.GetMetadataDepthZero
			case "1":
				options.Depth = imap.GetMetadataDepthOne
			case "infinity":
				options.Depth = imap.GetMetadataDepthInfinity
			default:
				return newClientBugError("Invalid GETMETADATA depth")
			}
		default:
			return newClientBugError("Unknown GETMETADATA option")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if isList && (len(seen) == 0 || !dec.ExpectSP()) {
		return newClientBugError("Empty GETMETADATA options")
	}
	var mailbox string
	if !dec.ExpectMailbox(&mailbox) || !dec.ExpectSP() {
		return dec.Err()
	}
	var entries []string
	readEntry := func() error {
		var name string
		if !dec.ExpectAString(&name) {
			return dec.Err()
		}
		if !validMetadataEntry(name, true) {
			return newClientBugError("Invalid metadata entry name")
		}
		if len(entries) >= MetadataMaxEntries {
			c.state = imap.ConnStateLogout
			return metadataError("TOOMANY", "Too many requested entries")
		}
		entries = append(entries, strings.ToLower(name))
		return nil
	}
	isList, err = dec.List(readEntry)
	if err != nil {
		return err
	}
	if !isList {
		if err := readEntry(); err != nil {
			return err
		}
	}
	if len(entries) == 0 {
		return newClientBugError("Empty metadata entry list")
	}
	if !dec.ExpectCRLF() {
		return dec.Err()
	}
	c.command.Mailbox = mailbox
	if err := c.checkState(imap.ConnStateAuthenticated); err != nil {
		return err
	}
	sess, err := c.metadataSession(mailbox)
	if err != nil {
		return err
	}
	values, err := sess.GetMetadata(mailbox, entries, options.Depth)
	if err != nil {
		return err
	}
	// Exact queries return NIL for missing annotations. Depth queries return
	// existing descendants without inventing entries at hierarchy roots.
	keys := make(map[string]bool)
	if options.Depth == imap.GetMetadataDepthZero {
		for _, entry := range entries {
			keys[entry] = true
		}
	}
	for entry := range values {
		keys[entry] = true
	}
	names := make([]string, 0, len(keys))
	var largest uint32
	for entry := range keys {
		size := uint32(len(values[entry]))
		if options.MaxSize != nil && size > *options.MaxSize {
			if size > largest {
				largest = size
			}
			continue
		}
		names = append(names, entry)
	}
	sort.Strings(names)
	if len(names) > 0 {
		enc := newResponseEncoder(c)
		enc.Atom("*").SP().Atom("METADATA").SP().Mailbox(mailbox).SP().List(len(names), func(i int) {
			name := names[i]
			enc.String(name).SP()
			if value, ok := values[name]; ok {
				enc.String8(string(value))
			} else {
				enc.NIL()
			}
		})
		err = enc.CRLF()
		enc.end()
		if err != nil {
			return err
		}
	}
	if err := c.poll("GETMETADATA"); err != nil {
		return err
	}
	resp := &imap.StatusResponse{Type: imap.StatusResponseTypeOK, Text: "GETMETADATA completed"}
	if largest > 0 {
		resp.Code = imap.ResponseCode(fmt.Sprintf("METADATA LONGENTRIES %d", largest))
	}
	return c.writeStatusResp(tag, resp)
}

func (c *Conn) handleSetMetadata(dec *imapwire.Decoder) error {
	dec.CheckBufferedLiteralFunc = c.checkMetadataNameLiteral
	var mailbox string
	if !dec.ExpectSP() || !dec.ExpectMailbox(&mailbox) || !dec.ExpectSP() {
		return dec.Err()
	}
	c.command.Mailbox = mailbox
	var sess SessionMetadata
	var checked bool
	var commandErr error
	check := func() error {
		if !checked {
			checked = true
			commandErr = c.checkState(imap.ConnStateAuthenticated)
			if commandErr == nil {
				sess, commandErr = c.metadataSession(mailbox)
			}
		}
		return commandErr
	}
	entries := make(map[string]*[]byte)
	var validationErr error
	count := 0
	isList, err := dec.List(func() error {
		count++
		if count > MetadataMaxEntries {
			// The remainder can contain literals. Close after the tagged error
			// rather than interpret unconsumed bytes as another command.
			c.state = imap.ConnStateLogout
			return metadataError("TOOMANY", "Too many metadata entries")
		}
		var name string
		if !dec.ExpectAString(&name) || !dec.ExpectSP() {
			return dec.Err()
		}
		if !validMetadataEntry(name, false) {
			validationErr = newClientBugError("Invalid metadata entry name")
		}
		name = strings.ToLower(name)
		if _, exists := entries[name]; exists {
			validationErr = newClientBugError("Duplicate metadata entry")
		}
		value, err := c.readMetadataValue(dec, check)
		if err != nil {
			return err
		}
		entries[name] = value
		return nil
	})
	if err != nil {
		return err
	}
	if !isList || count == 0 {
		return newClientBugError("Expected non-empty metadata entry list")
	}
	if !dec.ExpectCRLF() {
		return dec.Err()
	}
	if validationErr != nil {
		return validationErr
	}
	if err := check(); err != nil {
		return err
	}
	return sess.SetMetadata(mailbox, entries)
}

func (c *Conn) checkMetadataNameLiteral(size int64, nonSync bool) error {
	err := c.checkBufferedLiteral(size, nonSync)
	if err != nil && nonSync {
		c.state = imap.ConnStateLogout
	}
	return err
}

func (c *Conn) readMetadataValue(dec *imapwire.Decoder, check func() error) (*[]byte, error) {
	var value string
	if dec.Quoted(&value) {
		if len(value) > MetadataMaxSize {
			return nil, metadataError(fmt.Sprintf("MAXSIZE %d", MetadataMaxSize), "Metadata value too large")
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return nil, newClientBugError("Invalid quoted metadata value")
		}
		b := []byte(value)
		return &b, nil
	}
	binary := dec.Special('~')
	lit, nonSync, ok := dec.LiteralReader()
	if !ok {
		if binary || !dec.ExpectAtom(&value) || !strings.EqualFold(value, "NIL") {
			return nil, newClientBugError("Expected metadata value or NIL")
		}
		return nil, nil
	}
	if lit.Size() > MetadataMaxSize {
		if nonSync {
			c.state = imap.ConnStateLogout
		}
		return nil, metadataError(fmt.Sprintf("MAXSIZE %d", MetadataMaxSize), "Metadata value too large")
	}
	commandErr := check()
	if commandErr != nil && !nonSync {
		return nil, commandErr
	}
	if err := c.acceptLiteral(lit.Size(), nonSync); err != nil {
		if nonSync {
			c.state = imap.ConnStateLogout
		}
		return nil, err
	}
	c.setReadTimeout(literalReadTimeout)
	defer c.setReadTimeout(cmdReadTimeout)
	b, err := io.ReadAll(lit)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) != lit.Size() {
		return nil, io.ErrUnexpectedEOF
	}
	if !binary && strings.IndexByte(string(b), 0) >= 0 {
		return nil, newClientBugError("NUL requires a binary literal")
	}
	return &b, nil
}
