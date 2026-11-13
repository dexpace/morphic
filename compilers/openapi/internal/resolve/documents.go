// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/compilers"
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
// the resolver's, departing from the resolver's own thrice, where the load
// phase respells what the resolver reads (ForResolver): against an http(s)
// URL, an absolute path resolves by RFC 3986 §5.2; a file path is read
// decoded (FilePart); and a relative path whose first segment holds a colon
// keeps a leading "./" (RFC 3986 §4.2).
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
// against an http(s) URL, an absolute path or a network-path reference as the
// URL it names, which the resolver would read as a local file.
func ForResolver(uri, base string) string {
	uri = strings.TrimSpace(uri)
	if _, ok := networkURL(base); !ok {
		return FilePart(uri)
	}
	if !strings.HasPrefix(uri, "/") {
		return uri
	}
	if located, ok := Locate(uri, base); ok {
		return located
	}
	return uri
}

// networkURL returns base parsed as an http or https URL, and false for any
// other location.
func networkURL(base string) (*url.URL, bool) {
	u, err := url.Parse(base)
	if err != nil || !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return nil, false
	}
	return u, true
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

// SameDocument reports whether two documents a reference reads, a and b, are
// one: the same spelling, or for two files, a file URI naming its path
// (FileOf), the same path with its dot segments removed (RFC 3986 §5.2.4). The
// test is lexical, so a file reached through a link is another document, and a
// directory that does not exist cleans away like one that does. A relative
// path and an absolute one are two documents, as only the working directory,
// which is not read, relates them. Any other URL is one only as spelled, and
// an empty name names none.
func SameDocument(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	fileA, okA := FileOf(a)
	fileB, okB := FileOf(b)
	return okA && okB && filepath.Clean(fileA) == filepath.Clean(fileB)
}

// FileOf returns the path of the file the document name names: name itself
// for a path, and for a file URI the path it names, as a reader reads one
// (compilers.FilePath), so that one file is one document however a reference
// spells it. It is false for any other URL, and for a file URI naming no
// local file.
func FileOf(name string) (string, bool) {
	if !IsURL(name) {
		return name, true
	}
	u, err := url.Parse(name)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return "", false
	}
	path, err := compilers.FilePath(u, runtime.GOOS == "windows")
	return path, err == nil
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
