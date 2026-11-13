// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compilers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
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

// DirectoryLocator is what a DocumentReader that reads files may also offer:
// where a directory lies, so a compile names a file reached through two
// spellings of its directory, such as a symbolic link and its target, as one
// document. A reader without it leaves every path as spelled.
type DirectoryLocator interface {
	// LocateDirectory returns the absolute directory dir with its symbolic
	// links resolved, or dir itself where they do not resolve.
	LocateDirectory(dir string) string
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

// FilePath returns the path of the local file the file URI u names (RFC
// 8089), as readers and compiles read it: percent-decoded,
// a drive letter's leading slash removed where windows says paths are read as
// Windows reads them. It refuses a URI naming a user, a file on another host,
// no absolute path or a query, a path opening with two slashes, which Windows
// reads as another host's share, a drive no slash follows, which Windows reads
// against that drive's working directory, and a separator that would decode
// into another segmentation: an escaped slash, or on Windows a backslash.
func FilePath(u *url.URL, windows bool) (string, error) {
	switch {
	case u.Path == "": // an opaque URI, file:a.yaml, has none
		return "", errors.New("a file URI names no absolute path")
	case u.User != nil:
		return "", errors.New("a file URI's user names no local file")
	case u.Host != "" && !strings.EqualFold(u.Host, "localhost"):
		return "", fmt.Errorf("a file URI names a file on host %q, not a local one", u.Host)
	case u.RawQuery != "" || u.ForceQuery:
		return "", errors.New("a file URI's query names nothing in a file")
	}
	escaped := strings.ToLower(u.EscapedPath())
	if strings.Contains(escaped, "%2f") {
		return "", errors.New("a file URI's escaped slash names no single path segment")
	}
	// EscapedPath escapes a backslash written as itself too.
	if windows && strings.Contains(escaped, "%5c") {
		return "", errors.New("a file URI's backslash is a separator on Windows, so it names no single path segment")
	}
	path := u.Path
	if windows && len(path) >= 3 && path[0] == '/' && path[2] == ':' && isDriveLetter(path[1]) {
		if !strings.HasPrefix(path[3:], "/") {
			return "", errors.New("a file URI's drive is followed by no slash, so it names no absolute path")
		}
		path = path[1:]
	}
	if strings.HasPrefix(path, "//") {
		return "", errors.New("a file URI's path opens with two slashes, naming a network share, not a local file")
	}
	return path, nil
}

// isDriveLetter reports whether c names a Windows drive.
func isDriveLetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}
