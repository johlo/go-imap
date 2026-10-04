package imapserver

import (
	"errors"
	"io"

	"github.com/johlo/go-imap/v2"
)

// ErrResponseHandled suppresses the normal reply after a hook has taken over.
var ErrResponseHandled = errors.New("imapserver: response handled by hook")

// Command describes a parsed command without exposing passwords or literals.
// Mailbox is the decoded source mailbox argument, if the command names one.
// HookData belongs to the application and lives until command completion.
type Command struct {
	Tag, Name, Mailbox string
	HookData           interface{}
}

func (c *Conn) Command() *Command { return c.command }
func (c *Conn) Session() Session  { return c.session }

// Close closes the connection without writing a response.
func (c *Conn) Close() error { return c.NetConn().Close() }

// WriteRawResponse writes and flushes a protocol response under the encoder
// lock. Call it between responses, never while a FetchResponseWriter is open.
func (c *Conn) WriteRawResponse(response string) error {
	c.encMutex.Lock()
	defer c.encMutex.Unlock()
	c.setWriteTimeout(respWriteTimeout)
	defer c.setWriteTimeout(0)
	if _, err := io.WriteString(c.bw, response); err != nil {
		return err
	}
	return c.bw.Flush()
}

func (c *Conn) beforeCommand() error {
	if c.command == nil || c.commandHookDone {
		return nil
	}
	c.commandHookDone = true
	if c.server.options.CommandHook == nil {
		return nil
	}
	return c.server.options.CommandHook(c, c.command)
}

func (c *Conn) filterCompletion(resp *imap.StatusResponse) *imap.StatusResponse {
	if c.command == nil || c.responseHookDone {
		return resp
	}
	c.responseHookDone = true
	if hook := c.server.options.ResponseHook; hook != nil {
		if resp != nil {
			copy := *resp
			resp = &copy
		}
		return hook(c, c.command, resp)
	}
	return resp
}

// Preserve structured success codes unless the hook replaces the response.
func (c *Conn) interceptCompletion(tag string, resp *imap.StatusResponse) (bool, error) {
	if tag == "" {
		return false, nil
	}
	filtered := c.filterCompletion(resp)
	if filtered != nil && *filtered == *resp {
		return false, nil
	}
	return true, c.writeStatusResp(tag, filtered)
}
