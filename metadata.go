package imap

import "fmt"

// GetMetadataDepth controls how many levels below an entry are retrieved.
type GetMetadataDepth int

const (
	GetMetadataDepthZero     GetMetadataDepth = 0
	GetMetadataDepthOne      GetMetadataDepth = 1
	GetMetadataDepthInfinity GetMetadataDepth = -1
)

func (depth GetMetadataDepth) String() string {
	switch depth {
	case GetMetadataDepthZero:
		return "0"
	case GetMetadataDepthOne:
		return "1"
	case GetMetadataDepthInfinity:
		return "infinity"
	default:
		panic(fmt.Errorf("imap: unknown GETMETADATA depth %d", depth))
	}
}

// GetMetadataOptions contains options for the GETMETADATA command.
type GetMetadataOptions struct {
	MaxSize *uint32
	Depth   GetMetadataDepth
}

// GetMetadataData is the data returned by GETMETADATA. A nil value denotes a
// missing entry, whereas a pointer to an empty slice denotes an empty value.
type GetMetadataData struct {
	Mailbox string
	Entries map[string]*[]byte
	// LongEntries is the largest value omitted because it exceeded MaxSize.
	LongEntries uint32
}
