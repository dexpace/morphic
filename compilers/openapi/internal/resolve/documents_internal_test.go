// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
)

// TestSameDocument pins when the resolver reading one path reads the document
// at another: the same spelling, or the same path once cleaned, a file URI
// naming the path it names. A relative and an absolute path are two documents,
// the working directory being no input, any other URL is one document only as
// spelled, and an empty path names none.
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
		{abs, filepath.Join(filepath.Dir(abs), "x", "..", "spec.yaml"), true},
		{"api/spec.yaml", abs, false},
		{abs, "api/spec.yaml", false},
		{"../api/spec.yaml", "spec.yaml", false},
		{"api/spec.yaml", "api/other.yaml", false},
		{"api/x:/spec.yaml", "api/x://spec.yaml", true},
		{"https://example.com/a.yaml", "https://example.com/a.yaml", true},
		{"https://example.com/a.yaml", "https://example.com/./a.yaml", false},
		{"file:/api/spec.yaml", "file:/api/./spec.yaml", true},
		{"file:///api/spec.yaml", "/api/x/../spec.yaml", true},
		{"/api/spec.yaml", "file://localhost/api/my%20spec.yaml", false},
		{"/api/my spec.yaml", "file://localhost/api/my%20spec.yaml", true},
		{"file:///api/spec.yaml", "api/spec.yaml", false},
		{"file://host/api/spec.yaml", "/api/spec.yaml", false},
		{"file:spec.yaml", "spec.yaml", false},
		{"", "", false},
	} {
		assert.Equal(t, c.want, (*Places)(nil).SameDocument(c.a, c.b), "%q %q", c.a, c.b)
	}
}

// TestLocate pins the document a reference's document part names against the
// document holding it: as the resolver joins it, but read percent-decoded where
// that names a file, by RFC 3986 against an http(s) URL, an absolute path and a
// network-path reference among them, and with a leading "./" kept where the
// join would spell a URL.
func TestLocate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ uri, base, want string }{
		{"", "/api/spec.yaml", "/api/spec.yaml"},
		{"common.yaml", "/api/spec.yaml", "/api/common.yaml"},
		{"../common.yaml", "/api/spec.yaml", "/common.yaml"},
		{"common.yaml", "api/spec.yaml", "api/common.yaml"},
		{"/abs/common.yaml", "api/spec.yaml", "/abs/common.yaml"},
		{"my%20file.yaml", "/api/spec.yaml", "/api/my file.yaml"},
		{"/abs/my%20file.yaml", "api/spec.yaml", "/abs/my file.yaml"},
		{"a%2Fb.yaml", "/api/spec.yaml", "/api/a%2Fb.yaml"},
		{"./file:x.yaml", "spec.yaml", "./file:x.yaml"},
		{"./file:x.yaml", "/api/spec.yaml", "/api/file:x.yaml"},
		{"sub/../file:x.yaml", "spec.yaml", "./file:x.yaml"},
		{"file:x.yaml", "spec.yaml", "file:x.yaml"},
		{"https://example.com/a.yaml", "/api/spec.yaml", "https://example.com/a.yaml"},
		{"b.yaml", "https://example.com/v1/a.yaml", "https://example.com/v1/b.yaml"},
		{"/schemas/pet.yaml", "https://example.com/v1/a.yaml", "https://example.com/schemas/pet.yaml"},
		{"//cdn.example.com/pet.yaml", "https://example.com/v1/a.yaml", "https://cdn.example.com/pet.yaml"},
		{"my%20file.yaml", "https://example.com/v1/a.yaml", "https://example.com/v1/my%20file.yaml"},
		{"file:///etc/a.yaml", "https://example.com/v1/a.yaml", "file:///etc/a.yaml"},
		{"", "https://example.com/v1/a.yaml", "https://example.com/v1/a.yaml"},
	} {
		got, ok := Locate(c.uri, c.base)
		assert.True(t, ok, "%q in %q", c.uri, c.base)
		assert.Equal(t, c.want, got, "%q in %q", c.uri, c.base)
	}
	_, ok := Locate("a.yaml", "%zz")
	assert.False(t, ok, "a base the resolver cannot read places nothing")
	_, ok = Locate("%zz/a.yaml", "https://example.com/a.yaml")
	assert.False(t, ok, "a reference no URL parser reads names nothing against a URL")
}

// TestFileOf pins the file a document name names: a path itself, and a file
// URI the path a reader reads it as; no other URL, and no file URI naming no
// local file, names one.
func TestFileOf(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, want string
		ok         bool
	}{
		{"api/spec.yaml", "api/spec.yaml", true},
		{"./file:x.yaml", "./file:x.yaml", true},
		{"/api/spec.yaml", "/api/spec.yaml", true},
		{"file:///api/my%20spec.yaml", "/api/my spec.yaml", true},
		{"FILE:///api/spec.yaml", "/api/spec.yaml", true},
		{"file://server/api/spec.yaml", "", false},
		{"file:spec.yaml", "", false},
		{"https://example.com/spec.yaml", "", false},
		{"urn:example:spec", "", false},
	} {
		got, ok := (*Places)(nil).FileOf(c.name)
		assert.Equal(t, c.ok, ok, "%q", c.name)
		assert.Equal(t, c.want, got, "%q", c.name)
	}
}

// TestForResolver pins how a reference's document part is spelled for the
// parser's resolver, so that its own join reads the document Locate names:
// decoded where it names a file, and against an http(s) URL, as the URL Locate
// names wherever the resolver reads another, as it reads an absolute path or a
// network-path reference as a local file, any other part as written.
func TestForResolver(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ uri, base, want string }{
		{"my%20file.yaml", "/api/spec.yaml", "my file.yaml"},
		{" my%20file.yaml ", "spec.yaml", "my file.yaml"},
		{"common.yaml", "/api/spec.yaml", "common.yaml"},
		{"/schemas/pet.yaml", "https://example.com/v1/a.yaml", "https://example.com/schemas/pet.yaml"},
		{"//cdn.example.com/pet.yaml", "https://example.com/v1/a.yaml", "https://cdn.example.com/pet.yaml"},
		{"pet%20s.yaml", "https://example.com/v1/a.yaml", "pet%20s.yaml"},
		{"file:///etc/x.yaml", "https://example.com/v1/a.yaml", "file:///etc/x.yaml"},
		{"/%zz", "https://example.com/v1/a.yaml", "/%zz"},
		{"", "https://example.com/v1/a.yaml", ""},
		{"../b.yaml", "https://example.com/v1/a.yaml", "../b.yaml"},
		{"https://other.example/b.yaml", "https://example.com/v1/a.yaml", "https://other.example/b.yaml"},
	} {
		assert.Equal(t, c.want, ForResolver(c.uri, c.base), "%q in %q", c.uri, c.base)
	}
}

// TestFilePart pins the file a reference's document part names: decoded, but
// kept as written where decoding would name another document or text no IR
// string holds, and for a URL, whose escapes are its own.
func TestFilePart(t *testing.T) {
	t.Parallel()
	for uri, want := range map[string]string{
		"my%20file.yaml":        "my file.yaml",
		"d~x/my%20file@v1.yaml": "d~x/my file@v1.yaml",
		"caf%C3%A9.yaml":        "café.yaml",
		"plain.yaml":            "plain.yaml",
		"100%.yaml":             "100%.yaml",
		"bad%FF.yaml":           "bad%FF.yaml",
		"a%2Fb.yaml":            "a%2Fb.yaml",
		"a%2fb.yaml":            "a%2fb.yaml",
		"a%5Cb.yaml":            "a%5Cb.yaml",
		"a%23b.yaml":            "a%23b.yaml",
		"file%3Ax.yaml":         "file%3Ax.yaml",
		"x%3Ay.yaml":            "x%3Ay.yaml",
		"sub/x%3Ay.yaml":        "sub/x:y.yaml",
		"https://h/my%20f.yaml": "https://h/my%20f.yaml",
	} {
		assert.Equal(t, want, FilePart(uri), "%q", uri)
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

// linkReader places one directory elsewhere, as a link to it would, counting
// each time it is asked, and reads nothing.
type linkReader struct {
	from, to string
	asked    map[string]int
}

func (r *linkReader) ReadDocument(context.Context, compilers.DocumentRequest) ([]byte, error) {
	return nil, errors.New("reads nothing")
}

func (r *linkReader) LocateDirectory(dir string) string {
	r.asked[dir]++
	if dir == r.from {
		return r.to
	}
	return dir
}

// TestPlaces_PlacesAFileWhereItsDirectoryLies pins where a compile's files
// lie: an absolute path, its dot segments removed, in its directory as the
// reader places it, asked once a directory, its own name kept; a relative path
// and every path under a reader placing none, as spelled. One file spelled
// through the link, as its target or by a file URI is one document.
func TestPlaces_PlacesAFileWhereItsDirectoryLies(t *testing.T) {
	t.Parallel()
	r := &linkReader{from: "/link", to: "/real", asked: map[string]int{}}
	p := NewPlaces(r)
	require.NotNil(t, p)

	assert.Equal(t, "/real/a.yaml", p.File("/link/a.yaml"))
	assert.Equal(t, "/real/a.yaml", p.File("/link/x/../a.yaml"))
	assert.Equal(t, "/other/a.yaml", p.File("/other/a.yaml"))
	assert.Equal(t, "link/a.yaml", p.File("link/a.yaml"), "a relative path, kept as spelled")
	assert.Equal(t, "/real", p.Directory("/link"))
	assert.Equal(t, "link", p.Directory("link"))
	assert.Equal(t, 1, r.asked["/link"], "a directory is asked once")
	file, ok := p.FileOf("file:///link/a.yaml")
	assert.True(t, ok)
	assert.Equal(t, "/real/a.yaml", file)
	assert.True(t, p.SameDocument("/link/a.yaml", "file:///real/a.yaml"))
	assert.False(t, p.SameDocument("/link/a.yaml", "/other/a.yaml"))

	assert.Nil(t, NewPlaces(nil), "a reader placing no directory")
	var none *Places
	assert.Equal(t, "/link/a.yaml", none.File("/link/a.yaml"))
	assert.Equal(t, "/link", none.Directory("/link"))
	assert.False(t, none.SameDocument("/link/a.yaml", "/real/a.yaml"), "read lexically")
}
