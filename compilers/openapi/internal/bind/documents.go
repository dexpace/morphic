// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"encoding/json/jsontext"
	"errors"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// maxPointerTokens bounds the tokens a pointer is read through, as
// nodeview's own walk bounds them: no document the compiler admits nests
// deeper.
const maxPointerTokens = 1024

// errNotRead is why a reference to another document fails when the table
// reads none (Config.Open is nil).
var errNotRead = errors.New("other documents are not read")

// document is one document a table reads: its name, its root, the view that
// reads it as the parser does, whether its positions are read by the OpenAPI
// grammar, as the source's are, another's being raw YAML, and whether its root
// is an OpenAPI Object, which places its schemas (docs.identityState).
type document struct {
	name    string
	root    *yaml.Node
	view    *nodeview.View
	grammar bool
	openapi bool
	// self is the URI the document's $self names, resolved against its name,
	// its base and a second name of it, or empty for none (selfOf), and
	// absolute whether the $self is written as a URI or an absolute path.
	self     string
	absolute bool
}

// docs is every document a table has read, and the work reading them costs.
// reached is those the pass under way asked for, which a document read for a
// pass before it need not be, and fresh those of them it reached since a
// drain last took them (takeFresh).
type docs struct {
	cfg     Config
	list    []*document
	byName  map[string]DocID
	failed  map[string]error
	work    *work
	reached map[DocID]bool
	fresh   []DocID
	// rules is each document's reading of each dialect it names, which
	// Config.Dialect gives (dialectRule), asked once, and last the one asked
	// last.
	rules map[ruleKey]DialectRule
	last  struct {
		key   ruleKey
		rule  DialectRule
		asked bool
	}
	// components is Config.Components as a set, which a mapping value spelling
	// one names (finder.mappingsOf), and schemes Config.SecuritySchemes, which
	// a requirement key spelling one names (finder.requirementKeys).
	components map[string]bool
	schemes    map[string]bool
	// keyRefs is the key each requirement key position is read as a
	// reference by, which no $ref spells (objects.refAt).
	keyRefs map[Position]string
}

// ruleKey is a document and a dialect its schemas name.
type ruleKey struct {
	doc     DocID
	dialect string
}

// newDocs returns the documents of a table over the source tree root.
func newDocs(root *yaml.Node, cfg Config, w *work) *docs {
	src := &document{name: cfg.Self, root: nodeview.DocumentRoot(root), view: nodeview.New(), grammar: true,
		openapi: true}
	components := make(map[string]bool, len(cfg.Components))
	for _, name := range cfg.Components {
		components[name] = true
	}
	d := &docs{cfg: cfg, list: []*document{src}, byName: map[string]DocID{cfg.Self: Source},
		failed: map[string]error{}, work: w, reached: map[DocID]bool{Source: true}, rules: map[ruleKey]DialectRule{},
		components: components, schemes: setOf(cfg.SecuritySchemes), keyRefs: map[Position]string{}}
	src.self, src.absolute = d.selfOf(src)
	return d
}

// selfOf is the URI the $self doc's OpenAPI Object writes names, resolved
// against where doc was read (resources.resolveURI), and whether it is written
// as a URI or an absolute path; or empty: for a document no OpenAPI Object
// roots, one writing no string $self, one whose $self holds a `#`, which
// OpenAPI 3.2 forbids and the rule table reports, and one that resolves to
// nothing. A URI is a name, not a location, so no mapping the caller wrote
// moves it (Config.Locate): it is spelled one way.
func (d *docs) selfOf(doc *document) (string, bool) {
	written, ok := scalarOf(doc.view.ChildByToken(doc.root, "$self"))
	written = strings.TrimSpace(written)
	if !doc.openapi || !ok || strings.Contains(written, "#") {
		return "", false
	}
	if u, err := url.Parse(written); err == nil && len(u.Scheme) > 1 {
		return NormalURI(u), true
	}
	uri, ok := resources{d}.resolveURI(written, doc.name)
	if !ok {
		return "", false
	}
	return uri, filepath.IsAbs(written)
}

// setOf returns names as a set.
func setOf(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// keysByURI reports whether document id's minor reads a requirement key as a
// URI (Config.KeysByURI).
func (d *docs) keysByURI(id DocID) bool {
	return d.cfg.KeysByURI != nil && d.cfg.KeysByURI(d.list[id].name)
}

// locate is the name of the document uri names, written in the document
// named base: Config.Locate's answer, or uri itself without one.
func (d *docs) locate(uri, base string) (string, bool) {
	if d.cfg.Locate == nil {
		return uri, true
	}
	return d.cfg.Locate(uri, base)
}

// refusal is why Config.Refuse refuses a reference whose document part is
// spelled uri, unread, or empty where it refuses none.
func (d *docs) refusal(uri string) string {
	if refuse := d.cfg.Refuse; refuse != nil {
		if err := refuse(uri); err != nil {
			return err.Error()
		}
	}
	return ""
}

// open returns the document named name, reading it through Config.Open the
// first time it is asked for, by from, and the same answer every later time.
// A read refused to from alone (BarredError) is no answer of the document's.
// Opening reaches nothing: a hop that resolves into the document does
// (binder.read), so a document read for a reference that a declaration then
// answers declares nothing.
func (d *docs) open(name, from string) (DocID, error) {
	if id, ok := d.byName[name]; ok {
		return id, nil
	}
	if err, ok := d.failed[name]; ok {
		return 0, err
	}
	if d.cfg.Open == nil {
		return 0, errNotRead
	}
	tree, err := askOpen(d, name, from)
	if err == nil && IsEmpty(tree) {
		err = ErrEmptyDocument
	}
	if barred := (*BarredError)(nil); errors.As(err, &barred) {
		return 0, err
	}
	if err != nil {
		d.failed[name] = err
		return 0, err
	}
	id := DocID(len(d.list))
	root := nodeview.DocumentRoot(tree)
	view := nodeview.New()
	opened := &document{name: name, root: root, view: view, openapi: writesOpenAPI(view, root)}
	opened.self, opened.absolute = d.selfOf(opened)
	d.list = append(d.list, opened)
	d.byName[name] = id
	return id, nil
}

// judge returns the refusal Config.Open gives the document from for name,
// one read before, or nil where from may read it.
func (d *docs) judge(name, from string) error {
	if d.cfg.Open == nil {
		return nil
	}
	_, err := askOpen(d, name, from)
	return err
}

// askOpen asks Config.Open, which is set, for the document named name as from
// reads it, a step of the work: the one call bind makes into the reader, for a
// read and a judgement alike.
func askOpen(d *docs, name, from string) (*yaml.Node, error) {
	d.work.spend(1)
	return d.cfg.Open(name, from)
}

// reach notes that the pass under way asked for document id.
func (d *docs) reach(id DocID) {
	if !d.reached[id] {
		d.reached[id] = true
		d.fresh = append(d.fresh, id)
	}
}

// takeFresh returns the documents reached since it last ran, and forgets them.
func (d *docs) takeFresh() []DocID {
	fresh := d.fresh
	d.fresh = nil
	return fresh
}

// writesOpenAPI reports whether root, a document's, is an OpenAPI Object: one
// naming its version, which OAS 3.1 §4.3.1 lets a read detect a document by.
func writesOpenAPI(view *nodeview.View, root *yaml.Node) bool {
	_, ok := scalarOf(view.ChildByToken(nodeview.Deref(root), "openapi"))
	return ok
}

// unread reports whether a read of name would be the first, one that a compile
// reading other documents has yet to make, for any asker (reads.unread).
func (d *docs) unread(name string) bool {
	_, opened := d.byName[name]
	_, failed := d.failed[name]
	return !opened && !failed && d.cfg.Open != nil
}

// path returns the nodes along p's pointer from its document's root, aliases
// dereferenced, and whether it reaches p, charging a step a node.
func (d *docs) path(p Position) ([]*yaml.Node, bool) {
	doc := d.list[p.Doc]
	path, complete := doc.view.DocumentPath(doc.root, p.Pointer)
	d.work.spend(len(path))
	return path, complete
}

// node returns the node at p, or nil where its document holds none.
func (d *docs) node(p Position) *yaml.Node {
	path, ok := d.path(p)
	if !ok {
		return nil
	}
	return path[len(path)-1]
}

// compare orders positions as a table reads them: the source's first,
// another document's by its name, then by pointer (comparePositions).
func (d *docs) compare(p, q Position) int {
	return compareNamed(p, q, d.name)
}

// name returns the name of the document id names.
func (d *docs) name(id DocID) string { return d.list[id].name }

// rootState is the state a document's root is read at.
func (d *docs) rootState(id DocID) state {
	if d.list[id].grammar {
		return sDocument
	}
	return sRaw
}

// identityState is the state a document's root is read at for what its schemas
// declare, and for the resource a position of it is in: the grammar's where it
// is an OpenAPI document, which places them as the source does. Any other has no
// grammar, so its mappings are schemas but for what a schema's keywords say is
// not one, as OAS 3.1 §4.3.1 leaves such a document to the reader: its root may
// be a schema, or a map of them under keys of its own.
func (d *docs) identityState(id DocID) state {
	if d.list[id].openapi {
		return sDocument
	}
	return sLoose
}

// stateAt is the state the OpenAPI grammar reads p at where p is: its own
// document's, not that of a reference read through on the way.
func (d *docs) stateAt(p Position) state {
	return stateOf(d.rootState(p.Doc), slices.Collect(p.Pointer.Tokens()))
}

// refOf is the $ref n carries, read through id's view as the model reads the
// mapping, a `<<` merging one in included, and whether it carries a string one:
// a null $ref is none, as the parser reads it.
func (d *docs) refOf(id DocID, n *yaml.Node) (string, bool) {
	if n = nodeview.Deref(n); n == nil || n.Kind != yaml.MappingNode {
		return "", false
	}
	v := nodeview.Deref(d.list[id].view.ChildByToken(n, "$ref"))
	if v == nil || v.Kind != yaml.ScalarNode || v.Tag == "!!null" {
		return "", false
	}
	return v.Value, true
}

// reading is where a read of a pointer stands: the position, the node there,
// and the state the grammar reads it at.
type reading struct {
	at    Position
	node  *yaml.Node
	state state
}

// spelledAs is r at the pointer spelled in b.
func (r reading) spelledAs(b []byte) reading {
	r.at.Pointer = jsontext.Pointer(b)
	return r
}

// child is the node token names below n in document id, read as RFC 6901
// reads a token (section 4): a key of a mapping, or an index of a sequence,
// written as a decimal with no leading zero. nodeview reads any integer
// strconv.Atoi does, "00" and "+0" too, which would place one node at two
// positions (GitHub #843).
func (d *docs) child(id DocID, n *yaml.Node, token string) *yaml.Node {
	if n = nodeview.Deref(n); n != nil && n.Kind == yaml.SequenceNode && !isIndex(token) {
		return nil
	}
	return nodeview.Deref(d.list[id].view.ChildByToken(n, token))
}

// absent is why no child of n in document id answers a token: n merges deeper
// than its view expands, so the child may lie past the bound, or it holds none.
func (d *docs) absent(id DocID, n *yaml.Node) Reason {
	if !d.list[id].view.Expanded(n) {
		return MergeBound
	}
	return NoTarget
}

// isIndex reports whether token is an array index as RFC 6901 writes one:
// "0", or a decimal with no leading zero.
func isIndex(token string) bool {
	if token == "" || len(token) > 1 && token[0] == '0' {
		return false
	}
	return strings.Trim(token, "0123456789") == ""
}

// schemaShaped reports whether n can be read as a schema: a mapping, or a
// boolean.
func schemaShaped(n *yaml.Node) bool {
	n = nodeview.Deref(n)
	return n != nil && (n.Kind == yaml.MappingNode || n.Kind == yaml.ScalarNode && n.Tag == "!!bool")
}
