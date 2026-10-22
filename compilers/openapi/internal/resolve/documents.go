package resolve

import (
	"path/filepath"

	"github.com/speakeasy-api/openapi/references"
)

// documentOf returns the path of the document ref names when it is written in
// the document at base: its document part joined onto base's directory and
// cleaned, as the resolver resolves it, or base for none. It reports false for
// no base and for a reference the resolver could not place.
func documentOf(ref references.Reference, base string) (string, bool) {
	if base == "" {
		return "", false
	}
	abs, err := references.ResolveAbsoluteReference(ref, base)
	if err != nil {
		return "", false
	}
	return abs.AbsoluteReference, true
}

// SameDocument reports whether paths a and b name one document: the same
// spelling, or, for two file paths, the same path once cleaned and made
// absolute. The test is lexical, as for a URI reference, so a file reached
// through a symlink or another link is another document, and a directory that
// does not exist cleans away like one that does. A URL (IsURL) names one only
// as spelled, and an empty path names none.
//
// The working directory is read only to tell a relative path from an absolute
// one, or from one not below it (filepath.IsLocal).
func SameDocument(a, b string) bool {
	return sameDocument(a, b, systemRules())
}

// pathRules is how the file system reads a path. A test hands in another
// system's reading, so the Windows forms are checked on any host.
type pathRules struct {
	abs     func(path string) (string, error) // against the working directory
	isAbs   func(path string) bool
	isLocal func(path string) bool
}

// systemRules returns filepath's reading of a path on this system.
func systemRules() pathRules {
	return pathRules{abs: filepath.Abs, isAbs: filepath.IsAbs, isLocal: filepath.IsLocal}
}

// sameDocument is SameDocument with the file system's reading handed in, so a
// test can count how often the working directory is read, and read paths as
// another system does.
func sameDocument(a, b string, rules pathRules) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if IsURL(a) || IsURL(b) {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	if sameAnchor(a, b, rules) {
		return false
	}
	absA, errA := rules.abs(a)
	absB, errB := rules.abs(b)
	return errA == nil && errB == nil && absA == absB
}

// sameAnchor reports whether two cleaned paths mean the same wherever the
// working directory is, so that when they differ they name different files:
// both absolute, or both below it. Below is filepath.IsLocal, which on Windows
// also refuses a path rooted without a volume (\a\b), one naming a drive
// (C:a\b) and a reserved name (NUL): none is absolute, and each is made so
// before it is compared.
func sameAnchor(a, b string, rules pathRules) bool {
	if rules.isAbs(a) != rules.isAbs(b) {
		return false
	}
	return rules.isAbs(a) || rules.isLocal(a) && rules.isLocal(b)
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
