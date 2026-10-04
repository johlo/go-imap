package imapserver

import (
	"strings"

	"github.com/johlo/go-imap/v2"
	"github.com/johlo/go-imap/v2/internal"
	"github.com/johlo/go-imap/v2/internal/imapwire"
)

func (c *Conn) handleCreate(dec *imapwire.Decoder) error {
	var (
		name    string
		options imap.CreateOptions
	)
	if !dec.ExpectSP() || !dec.ExpectMailbox(&name) {
		return dec.Err()
	}
	if dec.SP() {
		var name string
		if !dec.ExpectSpecial('(') || !dec.ExpectAtom(&name) || !dec.ExpectSP() {
			return dec.Err()
		}
		switch strings.ToUpper(name) {
		case "USE":
			var err error
			options.SpecialUse, err = internal.ExpectMailboxAttrList(dec)
			if err != nil {
				return err
			}
		default:
			return newClientBugError("unknown CREATE parameter")
		}
		if !dec.ExpectSpecial(')') {
			return dec.Err()
		}
	}
	if !dec.ExpectCRLF() {
		return dec.Err()
	}
	c.command.Mailbox = name
	if err := c.checkState(imap.ConnStateAuthenticated); err != nil {
		return err
	}
	return c.session.Create(name, &options)
}
