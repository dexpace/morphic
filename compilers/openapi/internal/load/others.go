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

	yaml "go.yaml.in/yaml/v3"

	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// Other is a document the compile read beside the source because a reference
// named it: where Document.Sources records it, and how the IDs of what it
// declares name it.
type Other struct {
	// Index is its position in Document.Sources.
	Index int
	// Label is its location as the source's directory reaches it, which
	// qualifies the IDs of what it declares (ids.In).
	Label string
	// Info is its Document.Sources entry.
	Info ir.SourceInfo
}

// Others is every document the compile read beside the source, in the order
// Document.Sources lists them, the names a reference reads each by, and where
// each one's trees write a node (see locator).
type Others struct {
	list     []Other
	byName   map[string]int
	pointers []func(*yaml.Node) (jsontext.Pointer, bool)
}

// locator returns the Locator for the nodes of the document a reference reads
// by name, which places one as the source's tree places its own (scan.InTree):
// in that document's Document.Sources entry, at the node's line and column,
// and at its pointer in whichever of the document's trees holds it. It returns
// false for a document the compile did not read beside the source.
func (o Others) locator(name string) (scan.Locator, bool) {
	i, ok := o.index(name)
	if !ok {
		return nil, false
	}
	return scan.InTree(o.list[i].Index, o.pointers[i]), true
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

// Named returns the document a reference reads by name, as the parser's
// resolver or the binder spells it, and false for one the compile did not
// read beside the source.
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

// newOthers lists the documents read prepares beside the source, the first at
// Document.Sources index first. They are ordered by label, not as they were
// read, so the order no declaration order changes; two names one label
// reaches, a file spelled two ways, are one document, as RFC 3986 reads a
// path. format is the source's, which a referenced document is read as part of.
func newOthers(ctx context.Context, read *externalReads, first int, format string) Others {
	read.mu.Lock()
	defer read.mu.Unlock()
	byLabel := map[string][]string{}
	for key := range read.data {
		// The source is held under each spelling the resolver reaches it by,
		// with no bytes of its own; it is Document.Sources' first entry.
		if read.self.names(key) || read.trees[key] == read.self.root {
			continue
		}
		label := labelOf(read.self.path, key, filepath.Abs)
		byLabel[label] = append(byLabel[label], key)
	}
	o := Others{byName: map[string]int{}}
	for i, label := range slices.Sorted(maps.Keys(byLabel)) {
		keys := slices.Sorted(slices.Values(byLabel[label]))
		data := read.data[keys[0]]
		o.list = append(o.list, Other{Index: first + i, Label: label, Info: ir.SourceInfo{
			Format: format, Path: sourcePathOf(keys[0]), Hash: ir.SourceHash(data),
		}})
		o.pointers = append(o.pointers, pointersIn(ctx, read.trees, keys))
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

// labelOf is the label of the document read under key, from the source at
// self: a file's path relative to the source's directory, with '/' between its
// segments whatever the system, or its cleaned path where no relative path
// reaches it; a URL as the resolver spells it. A relative path and an absolute
// one are related through abs, filepath.Abs's reading of the working
// directory, as resolve.SameDocument relates them, so one file is one label
// however the source was named.
func labelOf(self, key string, abs func(string) (string, error)) string {
	if refscope.IsURL(key) {
		return key
	}
	dir, clean := filepath.Dir(self), filepath.Clean(key)
	if filepath.IsAbs(dir) != filepath.IsAbs(clean) {
		absDir, dirErr := abs(dir)
		absKey, keyErr := abs(clean)
		if dirErr == nil && keyErr == nil {
			dir, clean = absDir, absKey
		}
	}
	if rel, err := filepath.Rel(dir, clean); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(clean)
}

// sourcePathOf is the path Document.Sources records for the document read under
// key: a file's cleaned, as it was read (fileNamed), a URL's as spelled.
func sourcePathOf(key string) string {
	if refscope.IsURL(key) {
		return key
	}
	return fileNamed(key)
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
