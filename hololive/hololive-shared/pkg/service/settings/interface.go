package settings

// Reader defines the read-only settings API.
type Reader interface {
	Get() Settings
}

// Writer defines the write settings API.
type Writer interface {
	Update(newSettings Settings) error
}

// ReadWriter is a convenience interface for consumers that need both Get/Update.
type ReadWriter interface {
	Reader
	Writer
}

var _ ReadWriter = (*Service)(nil)
