// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader

import (
	"context"

	"github.com/dexpace/morphic/compilers"
)

var _ compilers.DocumentReader = Layered{}

// Layered reads each document Held holds from it, and every other through
// Under: a spec held in memory, such as an edited buffer, read as the file at
// its path while the documents beside it stay on disk.
//
// Whether Held holds a request's document is judged before its confinement or
// limit, so each is applied by the reader serving the request, and a document
// Held does not hold is read exactly as Under alone reads it. A nil Held holds
// none, and a nil Under reads through Disk. Layered is safe for concurrent
// reads when Under is.
type Layered struct {
	Held  *Memory
	Under compilers.DocumentReader
}

// ReadDocument implements compilers.DocumentReader.
func (l Layered) ReadDocument(ctx context.Context, req compilers.DocumentRequest) ([]byte, error) {
	if l.Held.holds(req.Location) {
		return l.Held.ReadDocument(ctx, req)
	}
	if l.Under == nil {
		return Disk{}.ReadDocument(ctx, req)
	}
	return l.Under.ReadDocument(ctx, req)
}
