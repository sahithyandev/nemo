package filesystem

import "time"

// Type = type of "file system"
type Type string

const (
	TypeNTFS    Type = "ntfs"
	TypeAPFS    Type = "apfs"
	TypeEXT4    Type = "ext4"
	TypeUnknown Type = "unknown"
)

// Filesystem implementation.
type FileSystem interface {
	Type() Type
	Root() Entry
	Open(path string) (Entry, error)
}

// File or directory in a filesystem.
type Entry interface {
	Path() string
	IsDir() bool
	Children() ([]Entry, error)
	NamedStreams() ([]string, error)
}

// Entries that support named streams.
type NamedStreamCapable interface {
	WriteStream(name string, data []byte) error
	ReadStream(name string) ([]byte, error)
	DeleteStream(name string) error
}

// Usable slack-space region in the underlying image.
type SlackRegion struct {
	Offset int64
	Length int64
}

// Entries that expose slack-space regions.
type SlackSpaceCapable interface {
	SlackRegions() ([]SlackRegion, error)
}

// Which filesystem timestamp should be modified
type TimeField string

const (
	TimeCreated  TimeField = "created"
	TimeModified TimeField = "modified"
	TimeAccessed TimeField = "accessed"
)

// Entries that allow timestamp changes.
type TimestompCapable interface {
	SetTimestamp(field TimeField, t time.Time) error
}
