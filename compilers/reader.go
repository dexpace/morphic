// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compilers

import (
	"context"
	"errors"
)

// DocumentReader reads a document a compile reaches beyond the sources it was
// handed: a file or URL one of their references names.
//
// A compiler does no I/O of its own, so the caller supplies the reader and
// every read is the caller's. Which document a reference names, and whether
// the compile may leave the source's directory to read it, are the compiler's
// to decide; the reader enforces the confinement a request asks for and the
// size it allows. One reader may serve overlapping reads.
type DocumentReader interface {
	// ReadDocument returns the bytes of the document req names. A document
	// outside req.Within fails with ErrOutsideWithin, one larger than
	// req.Limit with ErrDocumentTooLarge; any other failure is the reader's to
	// describe. ctx bounds the read.
	ReadDocument(ctx context.Context, req DocumentRequest) ([]byte, error)
}

// DocumentRequest names one document to read and the bounds the read is held
// to.
type DocumentRequest struct {
	// Location names the document as the compiler resolved it from a
	// reference: a file path, or an absolute URL.
	Location string
	// Within is the directory the document must lie inside, its symbolic links
	// followed, or "" to confine the read nowhere. A URL lies inside no
	// directory, so a request with Within set never reaches the network.
	Within string
	// Limit is the most bytes the document may hold. Zero or less allows any
	// size.
	Limit int
}

// ErrOutsideWithin reports a document a request confined to a directory that
// lies outside it.
var ErrOutsideWithin = errors.New("outside the directory the read is confined to")

// ErrDocumentTooLarge reports a document larger than a request allows.
var ErrDocumentTooLarge = errors.New("larger than the read allows")
