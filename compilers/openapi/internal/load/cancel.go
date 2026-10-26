package load

import (
	"context"
	"io"
)

// contextReader reads r until ctx is done, then fails with ctx's error, so a
// parser reading through it stops within one of its reads. yaml.v3 reads a
// stream in small chunks as it parses, where a parse of bytes in hand runs to
// its end. It holds ctx because io.Reader's Read takes none, for the one
// decode it is made for.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

// Read implements io.Reader.
func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// stopped returns ctx's error once ctx is done, and err otherwise: what a step
// that failed while its context was done reports, since the parser quotes a
// read that failed in an error of its own, and its context is what stopped it.
func stopped(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return err
}
