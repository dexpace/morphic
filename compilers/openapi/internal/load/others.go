// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// Other is a document the compile read beside the source because a reference
// named it: where Document.Sources records it, how the IDs of what it declares
// name it, and the minor it is read at, which is the one it declares, or the
// source's where it declares none the table covers.
type Other struct {
	// Index is its position in Document.Sources.
	Index int
	// Label is its location as the source's directory reaches it, which
	// qualifies the IDs of what it declares (ids.In).
	Label string
	// Info is its Document.Sources entry, whose Format names the minor.
	Info ir.SourceInfo
	// Rows is what the table says at the minor in the dialect its
	// jsonSchemaDialect names, which its Encoding Objects and its schemas where
	// none names its own (Dialects) are read with.
	Rows Rows
	// version is the minor's version as the document, or the source, writes
	// it, which an object a reference reaches in it is held to (reached.check),
	// and dialect its jsonSchemaDialect.
	version, dialect string
}

// Others is every document a reference reached beside the source, in the order
// Document.Sources lists them, the names a reference reads each by, and where
// each one's trees write a node (see tree).
type Others struct {
	list   []Other
	byName map[string]int
	trees  []provenance.Tree
}

// tree returns where a node of the document a reference reads by name is
// placed, as the source's tree places its own: in that document's
// Document.Sources entry, at the node's line and column, and at its pointer in
// whichever of the document's trees holds it. It returns false for a document
// the compile did not read beside the source.
func (o Others) tree(name string) (provenance.Tree, bool) {
	i, ok := o.index(name)
	if !ok {
		return provenance.Tree{}, false
	}
	return o.trees[i], true
}

// List returns the documents, in the order Document.Sources lists them.
func (o Others) List() []Other { return slices.Clone(o.list) }

// At returns the document at index in Document.Sources, and false for an
// index no document of o is at.
func (o Others) At(index int) (Other, bool) {
	if len(o.list) == 0 {
		return Other{}, false
	}
	i := index - o.list[0].Index
	if i < 0 || i >= len(o.list) {
		return Other{}, false
	}
	return o.list[i], true
}

// Named returns the document a reference reads by name, as the binder spells
// it, and false for one the compile did not read beside the source.
func (o Others) Named(name string) (Other, bool) {
	i, ok := o.index(name)
	if !ok {
		return Other{}, false
	}
	return o.list[i], true
}

// index returns where in list the document a reference reads by name is.
func (o Others) index(name string) (int, bool) {
	if i, ok := o.byName[name]; ok {
		return i, true
	}
	i, ok := o.byName[readKey(name)]
	return i, ok
}

// unreachedKeys is the key of each document table read that nothing it holds is
// in (bind.Document.Unreached). Such a document lowers nothing and is no input
// of the result.
func unreachedKeys(table *bind.Table) map[string]bool {
	unused := map[string]bool{}
	for id := range bind.DocID(table.Documents()) {
		if d := table.Document(id); d.Unreached {
			unused[readKey(d.Name)] = true
		}
	}
	return unused
}

// newOthers lists the documents read prepares beside the source, the first at
// Document.Sources index first, but those under the keys unused holds. They are
// ordered by label, not as they were read, so the order no declaration order
// changes; two names one label reaches, a file spelled two ways, are one
// document, as RFC 3986 reads a path. Each is read at the minor it declares,
// one declaring none the table covers at the source's (dialects), and in the
// dialect its jsonSchemaDialect names.
func newOthers(ctx context.Context, read *externalReads, first int, source *dialects, unused map[string]bool) Others {
	read.mu.Lock()
	defer read.mu.Unlock()
	byLabel := map[string][]string{}
	for key := range read.data {
		if unused[key] {
			continue
		}
		label, _ := read.self.labelOf(key)
		byLabel[label] = append(byLabel[label], key)
	}
	o := Others{byName: map[string]int{}}
	for i, label := range slices.Sorted(maps.Keys(byLabel)) {
		keys := slices.Sorted(slices.Values(byLabel[label]))
		data := read.data[keys[0]]
		declared, dialect := declaredVersion(read.trees[keys[0]]), declaredDialect(read.trees[keys[0]])
		version, minor, rows := source.of(declared, dialect)
		o.list = append(o.list, Other{Index: first + i, Label: label, Info: ir.SourceInfo{
			Format: "openapi@" + minor, Path: read.self.shownPathOf(keys[0]), Hash: ir.SourceHash(data),
		}, Rows: rows, version: version, dialect: dialect})
		o.trees = append(o.trees, provenance.Tree{Doc: provenance.Doc{Source: first + i},
			Pointers: pointersIn(ctx, read.trees, keys)})
		for _, key := range keys {
			o.byName[key] = i
		}
	}
	return o
}

// pointersIn returns scan.Pointers over the trees read prepared under keys, all
// one document's, each indexed under ctx: a file spelled two ways is prepared
// under each spelling, and a node of any of them is placed where the tree
// holding it writes it.
func pointersIn(ctx context.Context, trees map[string]*yaml.Node, keys []string) func(*yaml.Node) (jsontext.Pointer, bool) {
	var finds []func(*yaml.Node) (jsontext.Pointer, bool)
	var indexed []*yaml.Node
	for _, key := range keys {
		if tree := trees[key]; !slices.Contains(indexed, tree) {
			indexed = append(indexed, tree)
			finds = append(finds, scan.Pointers(ctx, tree))
		}
	}
	return func(n *yaml.Node) (jsontext.Pointer, bool) {
		for _, find := range finds {
			if p, held := find(n); held {
				return p, true
			}
		}
		return "", false
	}
}

// labelOf is the label of the document read under key, from the source: a
// file's path, where it lies, relative to the source's directory, with '/'
// between its segments whatever the system, or its cleaned path where no
// relative path reaches it, as from a relative directory to an absolute path,
// which nothing relates without the working directory; a URL as the resolver
// spells it. A file URI names a file (resolve.Places.FileOf), so the file is
// one label however a reference spells it. relative reports the first.
func (s sourceDocument) labelOf(key string) (label string, relative bool) {
	file, ok := s.places.FileOf(key)
	if !ok {
		return key, false
	}
	dir, clean := filepath.Dir(s.path), filepath.Clean(file)
	if filepath.IsAbs(dir) == filepath.IsAbs(clean) {
		if rel, err := filepath.Rel(dir, clean); err == nil {
			return filepath.ToSlash(rel), true
		}
	}
	return filepath.ToSlash(clean), false
}

// shownPathOf is the path Document.Sources records for the document read
// under key: a URL's as spelled, and a file's as the path the caller handed
// names the source, so a source named relatively lists the documents beside it
// relatively too, wherever its Location lies. That is its label joined to the
// handed path's directory, keeping the dot segments a label climbing out of
// that directory starts with, since a link may place the directory elsewhere
// than cleaning them away would. A file no relative path reaches from the
// source is its path as read (fileNamed).
func (s sourceDocument) shownPathOf(key string) string {
	label, relative := s.labelOf(key)
	dir, local := filepath.Dir(s.handed), filepath.FromSlash(label)
	switch {
	case refscope.IsURL(key):
		return key
	case !relative || s.handed == "" || s.handed == s.path || refscope.IsURL(s.handed):
		return fileNamed(key)
	case dir != "." && (label == ".." || strings.HasPrefix(label, "../")):
		return dir + string(filepath.Separator) + local
	default:
		return fileNamed(filepath.Join(dir, local))
	}
}

// readKey is the key a document a reference names by name is read under: a
// URL's as the request built from it spells it (see external.document), any
// other name itself.
func readKey(name string) string {
	u, err := url.Parse(name)
	if err != nil || !refscope.IsURL(name) {
		return name
	}
	return u.String()
}
