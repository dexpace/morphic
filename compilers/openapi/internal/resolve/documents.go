// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// documentOf returns the path of the document ref names when it is written in
// the document at base (Locate), or base for none. It reports false for no
// base and for a reference the resolver could not place.
func documentOf(ref references.Reference, base string) (string, bool) {
	if base == "" {
		return "", false
	}
	return Locate(ref.GetURI(), base)
}

// Locate returns the document uri, the part of a reference before its '#',
// names when written in the document at base, spelled as the parser's resolver
// spells what it reads (references.ResolveAbsoluteReference), and false where
// it names none. It is the one reading of a document part, the binder's and
// the resolver's, departing from the resolver's own thrice: against an
// http(s) URL, an absolute path resolves by RFC 3986 §5.2; a file path is read
// decoded (FilePart); and a relative path whose first segment holds a colon
// keeps a leading "./" (RFC 3986 §4.2). The load phase respells the first
// two for the resolver (ForResolver).
func Locate(uri, base string) (string, bool) {
	uri = strings.TrimSpace(uri)
	if b, ok := networkURL(base); ok && uri != "" {
		if u, err := url.Parse(uri); err == nil && u.Scheme == "" {
			return b.ResolveReference(u).String(), true
		}
	}
	part := FilePart(uri)
	abs, err := references.ResolveAbsoluteReference(references.Reference(part), base)
	if err != nil {
		return "", false
	}
	located := abs.AbsoluteReference
	if !IsURL(part) && !IsURL(base) && IsURL(located) {
		located = "./" + located
	}
	return located, true
}

// ForResolver returns uri, the document part of a reference written in the
// document at base, spelled so that the parser's resolver, joining it its own
// way, reads the document Locate names: as the file it names (FilePart), and,
// against an http(s) URL, as the URL Locate names wherever the resolver's own
// reading names another, as it does an absolute path or a network-path
// reference, which it reads as a local file. So no reference in a document
// fetched over the network is read where Locate does not place it.
func ForResolver(uri, base string) string {
	uri = strings.TrimSpace(uri)
	if _, ok := networkURL(base); !ok {
		return FilePart(uri)
	}
	located, ok := Locate(uri, base)
	if !ok {
		return uri
	}
	own, err := references.ResolveAbsoluteReference(references.Reference(uri), base)
	if err == nil && own.AbsoluteReference == located {
		return uri
	}
	return located
}

// networkURL returns base parsed as an http or https URL (bind.OverNetwork),
// and false for any other location.
func networkURL(base string) (*url.URL, bool) {
	if !bind.OverNetwork(base) {
		return nil, false
	}
	u, err := url.Parse(base)
	return u, err == nil
}

// FilePart returns uri, a reference's document part, as the file it names
// spells it: percent-decoded, since a reference is a URI reference (RFC 3986
// §2.1), so "my%20file.yaml" names the file "my file.yaml". A URL is kept as
// written, and so is a part decoding would read otherwise: one with an escape
// that is not one, decoding to text that is not UTF-8, which no IR string
// holds, or decoding to a separator, a '#' or a scheme, each of which would
// name another document.
func FilePart(uri string) string {
	if !strings.Contains(uri, "%") || IsURL(uri) || escapesADelimiter(uri) {
		return uri
	}
	decoded, err := url.PathUnescape(uri)
	if err != nil || !utf8.ValidString(decoded) || IsURL(decoded) {
		return uri
	}
	return decoded
}

// escapesADelimiter reports whether uri escapes a character that delimits a
// file reference once decoded: a separator, which would split a segment, or a
// '#', which would start a fragment.
func escapesADelimiter(uri string) bool {
	lower := strings.ToLower(uri)
	return strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%23")
}

// Places is where a compile's files lie: an absolute path's directory as the
// compile's reader locates it (compilers.DirectoryLocator), the rule the
// engine places the spec's own location by, each directory asked once. A nil
// *Places, as for a reader locating none, leaves every path where it is
// spelled. Safe for concurrent use.
type Places struct {
	locator compilers.DirectoryLocator
	mu      sync.Mutex
	dirs    map[string]string
}

// NewPlaces returns where the files reader reads lie, or nil for a reader
// locating no directory.
func NewPlaces(reader compilers.DocumentReader) *Places {
	locator, ok := reader.(compilers.DirectoryLocator)
	if !ok {
		return nil
	}
	return &Places{locator: locator, dirs: map[string]string{}}
}

// File returns where the file at path lies: an absolute path, its dot
// segments removed, in its directory as located, its own name kept, so a
// file that is a link is read beside the link. A relative path is kept as
// spelled, as only the working directory, which a compile never reads,
// relates it to a directory.
func (p *Places) File(path string) string {
	if p == nil || !filepath.IsAbs(path) {
		return path
	}
	clean := filepath.Clean(path)
	return filepath.Join(p.Directory(filepath.Dir(clean)), filepath.Base(clean))
}

// Directory returns where the absolute directory dir lies, asking the locator
// the first time, so a compile reads one answer for each directory however
// often it is asked. A relative one is kept as spelled, as by File.
func (p *Places) Directory(dir string) string {
	if p == nil || !filepath.IsAbs(dir) {
		return dir
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	located, ok := p.dirs[dir]
	if !ok {
		located = p.locator.LocateDirectory(dir)
		p.dirs[dir] = located
	}
	return located
}

// SameDocument reports whether two documents a reference reads, a and b, are
// one: the same spelling, or for two files, a file URI naming its path, the
// same file where it lies (FileOf), its dot segments removed (RFC 3986
// §5.2.4). A file through a link is another document only where p places no
// directory, and a directory that does not exist cleans away like one that
// does. A relative path and an absolute one are two documents, as only the
// working directory, which is not read, relates them. Any other URL is one
// only as spelled, and an empty name names none.
func (p *Places) SameDocument(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	fileA, okA := p.FileOf(a)
	fileB, okB := p.FileOf(b)
	return okA && okB && filepath.Clean(fileA) == filepath.Clean(fileB)
}

// FileOf returns where the file the document name names lies (File): for a
// path, the path, and for a file URI the path it names, as a reader reads one
// (compilers.FilePath), so that one file is one document however a reference
// spells it. It is false for any other URL, and for a file URI naming no
// local file.
func (p *Places) FileOf(name string) (string, bool) {
	if !IsURL(name) {
		return p.File(name), true
	}
	u, err := url.Parse(name)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return "", false
	}
	path, err := compilers.FilePath(u, runtime.GOOS == "windows")
	if err != nil {
		return "", false
	}
	return p.File(path), true
}

// IsURL reports whether location names a document by URL rather than as a
// file, as the resolver classifies it (references.ResolveAbsoluteReference):
// by the scheme url.Parse finds, but for a drive letter before a backslash. A
// scheme needs no "//" after it, and a file path holding "://" past a
// directory named like a scheme is still a file. A location the resolver
// cannot parse is none; it resolves nothing.
func IsURL(location string) bool {
	abs, err := references.ResolveAbsoluteReference("", location)
	return err == nil && abs.Classification != nil && abs.Classification.IsURL
}
