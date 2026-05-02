package daemon

import (
	"bytes"
	"io"
)

// bytesReader is a tiny shim so envelope.go doesn't have to import bytes
// (we want to keep envelope.go visually focused on the wire schema).
func bytesReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}
