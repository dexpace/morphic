// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"io"
	"iter"

	soa "github.com/speakeasy-api/openapi/openapi"

	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// walk is the parser library's walk of start, stopped once ctx is done. The
// library's walk reads no context (docs/parser-library.md#context-unread), so
// every walk of a model the load phase makes goes through this one, asking ctx
// before each item. A phase whose walk was cut short is never built on: the
// next phase is refused before it starts (Options.enter). A media type
// reference, which the library's walk does not know, is walked by
// mediaTypeWalk.
func walk[T any](ctx context.Context, start *T) iter.Seq[soa.WalkItem] {
	if model, ok := any(start).(*refscope.ReferencedMediaType); ok {
		return mediaTypeWalk(ctx, model)
	}
	return func(yield func(soa.WalkItem) bool) {
		for item := range soa.Walk(ctx, start) {
			if ctx.Err() != nil || !yield(item) {
				return
			}
		}
	}
}

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
