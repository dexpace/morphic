// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSameDocument pins when the resolver reading one path reads the document
// at another: the same spelling, or the same file however reached. A URL is
// one document only as spelled, and an empty path names none.
func TestSameDocument(t *testing.T) {
	t.Parallel()
	abs, err := filepath.Abs("api/spec.yaml")
	require.NoError(t, err)
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"api/spec.yaml", "api/spec.yaml", true},
		{"api/spec.yaml", "api/./sub/../spec.yaml", true},
		{"api/spec.yaml", abs, true},
		{"api/spec.yaml", "api/other.yaml", false},
		{"api/x:/spec.yaml", "api/x://spec.yaml", true},
		{"https://example.com/a.yaml", "https://example.com/a.yaml", true},
		{"https://example.com/a.yaml", "https://example.com/./a.yaml", false},
		{"file:/api/spec.yaml", "file:/api/./spec.yaml", false},
		{"", "", false},
	} {
		assert.Equal(t, c.want, SameDocument(c.a, c.b), "%q %q", c.a, c.b)
	}
}

// sameDocumentByWorkingDirectory is SameDocument's reading when every pair of
// file paths is made absolute against the working directory: the reading the
// quicker one must agree with.
func sameDocumentByWorkingDirectory(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if IsURL(a) || IsURL(b) {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}

// TestSameDocument_AgreesWithTheWorkingDirectoryReading pins that reading the
// working directory only where it matters changes no answer. The grid is every
// pair of spellings of two files and a directory, relative and absolute, with
// parents climbing out of the working directory and back in, which cleaned
// strings alone cannot equate. Each file is also spelled rooted without its
// volume: on Windows that path is neither absolute nor below the directory,
// and elsewhere, with no volume, it is the absolute spelling again. So only a
// Windows run reaches that form here; the test for non-local Windows paths pins
// it on any host.
func TestSameDocument_AgreesWithTheWorkingDirectoryReading(t *testing.T) {
	t.Parallel()
	cwd, err := filepath.Abs(".")
	require.NoError(t, err)
	rooted := strings.TrimPrefix(cwd, filepath.VolumeName(cwd))
	up := filepath.Join("..", filepath.Base(cwd))
	names := []string{"spec.yaml", "api/spec.yaml", "other.yaml"}
	forms := []string{"%s", "./%s", "api/../%s", "x/./../%s", up + "/%s", "../../%s"}
	rest := []string{"", ".", "..", cwd, "https://example.com/spec.yaml", "https://example.com/./spec.yaml"}
	paths := make([]string, 0, len(names)*len(forms)*5+len(rest))
	for _, name := range names {
		for _, form := range forms {
			rel := strings.Replace(form, "%s", name, 1)
			paths = append(paths, rel, filepath.Join(cwd, rel), cwd+"//"+rel, cwd+"/./"+rel,
				filepath.Join(rooted, rel))
		}
	}
	paths = append(paths, rest...)
	for _, a := range paths {
		for _, b := range paths {
			assert.Equal(t, sameDocumentByWorkingDirectory(a, b), SameDocument(a, b), "%q %q", a, b)
		}
	}
}

// TestSameDocument_ReadsTheWorkingDirectoryOnlyWhereItMatters pins when
// SameDocument makes a path absolute, which reads the working directory: two
// absolute paths, or two relative ones that stay below it, are compared as
// cleaned and never read it, so a compile that names no document through an
// absolute path has no ambient read. A relative path against an absolute one,
// or one not below the directory, cannot be told apart without it.
func TestSameDocument_ReadsTheWorkingDirectoryOnlyWhereItMatters(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		a, b  string
		wd    string // the working directory the paths are made absolute against
		same  bool
		reads bool
	}{
		{"spec.yaml", "spec.yaml", "/abs", true, false},
		{"spec.yaml", "./sub/../spec.yaml", "/abs", true, false},
		{"spec.yaml", "other.yaml", "/abs", false, false},
		{"api/spec.yaml", "api/sub/spec.yaml", "/abs", false, false},
		{"/abs/spec.yaml", "/abs/./sub/../spec.yaml", "/elsewhere", true, false},
		{"/abs/spec.yaml", "/abs/other.yaml", "/elsewhere", false, false},
		{"https://example.com/a.yaml", "a.yaml", "/abs", false, false},
		{"", "spec.yaml", "/abs", false, false},
		{"spec.yaml", "/abs/spec.yaml", "/abs", true, true},
		{"spec.yaml", "/abs/spec.yaml", "/elsewhere", false, true},
		{"/abs/spec.yaml", "spec.yaml", "/abs", true, true},
		{"../api/spec.yaml", "spec.yaml", "/abs/api", true, true},
		{"../api/spec.yaml", "spec.yaml", "/abs/other", false, true},
		{"..", ".", "/abs", false, true},
		{"../a.yaml", "../b.yaml", "/abs", false, true},
		{"../a.yaml", "../b.yaml", "", false, true}, // no working directory to read: neither is made absolute
	} {
		reads := 0
		abs := func(p string) (string, error) {
			reads++
			if filepath.IsAbs(p) {
				return filepath.Clean(p), nil
			}
			if c.wd == "" {
				return "", errors.New("no working directory")
			}
			return filepath.Join(c.wd, p), nil
		}

		rules := systemRules()
		rules.abs = abs

		got := sameDocument(c.a, c.b, rules)

		assert.Equal(t, c.same, got, "%q %q from %s", c.a, c.b, c.wd)
		assert.Equal(t, c.reads, reads > 0, "reads the working directory: %q %q", c.a, c.b)
	}
}

// TestSameDocument_ReadsTheWorkingDirectoryForNonLocalWindowsPaths pins how
// SameDocument treats the Windows paths that are neither absolute nor below the
// working directory: one rooted without a volume, one naming a drive, a
// reserved name. Each is made absolute before it is compared, so two spellings
// of one file are one document. The table states what filepath documents for
// Windows (IsAbs, IsLocal) and what Abs names, so the test checks how
// SameDocument uses those answers on any host; only a Windows run of the grid
// test checks the answers themselves.
func TestSameDocument_ReadsTheWorkingDirectoryForNonLocalWindowsPaths(t *testing.T) {
	t.Parallel()
	// A path's reading on Windows with C:\specs the working directory: whether
	// IsAbs and IsLocal accept it, and the file Abs names.
	type reading struct {
		abs, local bool
		file       string
	}
	windows := map[string]reading{
		`C:\specs\api.yaml`:   {abs: true, file: `C:\specs\api.yaml`},
		`C:\specs\other.yaml`: {abs: true, file: `C:\specs\other.yaml`},
		`api.yaml`:            {local: true, file: `C:\specs\api.yaml`},
		`sub\api.yaml`:        {local: true, file: `C:\specs\sub\api.yaml`},
		`\specs\api.yaml`:     {file: `C:\specs\api.yaml`},     // rooted, no volume
		`C:sub\api.yaml`:      {file: `C:\specs\sub\api.yaml`}, // names a drive; the backslash makes IsURL read a file
		`NUL`:                 {file: `\\.\NUL`},               // a reserved name
	}
	read := func(path string) reading {
		r, ok := windows[path]
		if !ok {
			t.Errorf("path %q has no Windows reading in this test", path)
		}
		return r
	}
	for _, c := range []struct {
		a, b  string
		same  bool
		reads bool
	}{
		{`\specs\api.yaml`, `api.yaml`, true, true},
		{`api.yaml`, `\specs\api.yaml`, true, true},
		{`C:sub\api.yaml`, `sub\api.yaml`, true, true},
		{`sub\api.yaml`, `C:sub\api.yaml`, true, true},
		{`\specs\api.yaml`, `C:\specs\api.yaml`, true, true},
		{`NUL`, `api.yaml`, false, true},
		{`api.yaml`, `sub\api.yaml`, false, false},
		{`C:\specs\api.yaml`, `C:\specs\other.yaml`, false, false},
	} {
		require.Equal(t, c.same, read(c.a).file == read(c.b).file, "the table disagrees with the row %q %q", c.a, c.b)
		reads := 0
		rules := pathRules{
			abs: func(path string) (string, error) {
				reads++
				return read(path).file, nil
			},
			isAbs:   func(path string) bool { return read(path).abs },
			isLocal: func(path string) bool { return read(path).local },
		}

		got := sameDocument(c.a, c.b, rules)

		assert.Equal(t, c.same, got, "%q %q", c.a, c.b)
		assert.Equal(t, c.reads, reads > 0, "reads the working directory: %q %q", c.a, c.b)
	}
}

// TestIsURL pins which locations name a document by URL, as the resolver
// classifies them: those with a scheme before their first colon, whether or
// not "//" follows it. A file path is no URL, even one holding "://" past a
// directory named like a scheme, nor is a Windows drive before a backslash, or
// a location the resolver cannot parse.
func TestIsURL(t *testing.T) {
	t.Parallel()
	for location, want := range map[string]bool{
		"https://example.com/spec.yaml": true,
		"file:///api/spec.yaml":         true,
		"file:/api/spec.yaml":           true,
		"urn:example:spec":              true,
		"x://spec.yaml":                 true,
		"/api/x://spec.yaml":            false,
		"./x://spec.yaml":               false,
		"api/x://spec.yaml":             false,
		"api/spec.yaml":                 false,
		`C:\api\spec.yaml`:              false,
		"":                              false,
		"%zz":                           false,
	} {
		assert.Equal(t, want, IsURL(location), "%q", location)
	}
}
