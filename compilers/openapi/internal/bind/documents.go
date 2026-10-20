package bind

import (
	"encoding/json/jsontext"
	"errors"
	"slices"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// maxPointerTokens bounds the tokens a pointer is read through, as
// nodeview's own walk bounds them: no document the compiler admits nests
// deeper.
const maxPointerTokens = 1024

// maxPasses bounds how many reference objects one read passes through. Each
// is read on from where the parser's resolver ended its chain, and an end the
// resolver places past another reference object is read on through that one
// in turn (GitHub #762), so a loop of such ends stops here.
const maxPasses = 64

// errNotRead is why a reference to another document fails when the table
// reads none (Config.Open is nil).
var errNotRead = errors.New("other documents are not read")

// document is one document a table reads: its name, its root, the view that
// reads it as the parser does, and whether its positions are read by the
// OpenAPI grammar, as the source's are; another's are raw YAML.
type document struct {
	name    string
	root    *yaml.Node
	view    *nodeview.View
	grammar bool
}

// docs is every document a table has read, and the work reading them costs.
type docs struct {
	cfg    Config
	list   []*document
	byName map[string]DocID
	failed map[string]error
	work   *budget
}

// newDocs returns the documents of a table over the source tree root.
func newDocs(root *yaml.Node, cfg Config, work *budget) *docs {
	src := &document{name: cfg.Self, root: nodeview.DocumentRoot(root), view: nodeview.New(), grammar: true}
	return &docs{cfg: cfg, list: []*document{src}, byName: map[string]DocID{cfg.Self: Source},
		failed: map[string]error{}, work: work}
}

// locate is the name of the document uri names, written in the document
// named base: Config.Locate's answer, or uri itself without one.
func (d *docs) locate(uri, base string) (string, bool) {
	if d.cfg.Locate == nil {
		return uri, true
	}
	return d.cfg.Locate(uri, base)
}

// open returns the document named name, reading it through Config.Open the
// first time it is asked for, and the same answer every later time.
func (d *docs) open(name string) (DocID, error) {
	if id, ok := d.byName[name]; ok {
		return id, nil
	}
	if err, ok := d.failed[name]; ok {
		return 0, err
	}
	if d.cfg.Open == nil {
		return 0, errNotRead
	}
	d.work.spend(1)
	tree, err := d.cfg.Open(name)
	if root := nodeview.DocumentRoot(tree); err == nil && (root == nil || root.Kind == 0) {
		err = errors.New("the document is empty")
	}
	if err != nil {
		d.failed[name] = err
		return 0, err
	}
	id := DocID(len(d.list))
	d.list = append(d.list, &document{name: name, root: nodeview.DocumentRoot(tree), view: nodeview.New()})
	d.byName[name] = id
	return id, nil
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

// rootState is the state a document's root is read at.
func (d *docs) rootState(id DocID) state {
	if d.list[id].grammar {
		return sDocument
	}
	return sRaw
}

// stateAt is the state the OpenAPI grammar reads p at where p is: its own
// document's, not that of a reference read through on the way.
func (d *docs) stateAt(p Position) state {
	return stateOf(d.rootState(p.Doc), slices.Collect(p.Pointer.Tokens()))
}

// refOf is the $ref n carries, read through id's view as the source writes it,
// and whether it carries a string one: a null $ref is none, as the parser
// reads it.
func (d *docs) refOf(id DocID, n *yaml.Node) (string, bool) {
	if n = nodeview.Deref(n); n == nil || n.Kind != yaml.MappingNode {
		return "", false
	}
	v := nodeview.Deref(d.list[id].view.ChildByToken(n, "$ref"))
	if v == nil || v.Kind != yaml.ScalarNode || v.Tag == "!!null" {
		return "", false
	}
	if d.cfg.Written != nil {
		if written, ok := d.cfg.Written(v); ok {
			return written, true
		}
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

// read reads pointer from the root of document id, as RFC 6901 reads it, and
// through each reference object it passes, into what that names (through).
func (d *docs) read(id DocID, pointer jsontext.Pointer) (reading, Reason) {
	return d.readFrom(id, pointer, 0, nil)
}

// readFrom is read, having passed passes reference objects already. Given a
// node to reach, it stops at the first position holding it, and names nothing
// where none does. The pointer read so far is kept as bytes and spelled into
// the reading only where one is passed on or returned, so a read costs its
// pointer's length rather than its square.
func (d *docs) readFrom(id DocID, pointer jsontext.Pointer, passes int, want *yaml.Node) (reading, Reason) {
	r := reading{at: Position{Doc: id}, node: d.list[id].root, state: d.rootState(id)}
	var soFar []byte
	tokens := 0
	for token := range pointer.Tokens() {
		if tokens++; tokens > maxPointerTokens || !d.work.spend(1) {
			return r.spelledAs(soFar), NoTarget
		}
		if referenceKinds[r.state] && d.writesRef(r) {
			var reason Reason
			if r, reason = d.through(r.spelledAs(soFar), passes); reason != NoReason {
				return r, reason
			}
			soFar = append(soFar[:0], r.at.Pointer...)
		}
		if want != nil && nodeview.Deref(r.node) == want {
			return r.spelledAs(soFar), NoReason
		}
		child := d.child(r.at.Doc, r.node, token)
		if child == nil {
			return r.spelledAs(soFar), d.absent(r.at.Doc, r.node)
		}
		soFar = append(soFar, jsontext.Pointer("").AppendToken(token)...)
		r = reading{at: Position{Doc: r.at.Doc}, node: child, state: step(r.state, token)}
	}
	if want != nil && nodeview.Deref(r.node) != want {
		return r.spelledAs(soFar), NoTarget
	}
	return r.spelledAs(soFar), NoReason
}

// writesRef reports whether r holds a $ref through would read on through.
func (d *docs) writesRef(r reading) bool {
	ref, ok := d.refOf(r.at.Doc, r.node)
	return ok && ref != ""
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

// through carries r on to what the reference object at r names, where the
// parser's resolver ended its chain (Config.Through), read at the same state:
// a pointer through a response reads on in the response it names. The end is
// read as any pointer is, through the reference objects it lies past, up to
// the node the chain reached (ReferenceEnd). An object writing no $ref is read
// where it is. Only the source's reference objects are resolved; one in
// another document is not (GitHub #751).
func (d *docs) through(r reading, passes int) (reading, Reason) {
	if !d.writesRef(r) {
		return r, NoReason
	}
	if r.at.Doc != Source || d.cfg.Through == nil || passes >= maxPasses {
		return r, Unpassable
	}
	e, ok := d.cfg.Through(r.at.Pointer)
	if !ok {
		return r, Unpassable
	}
	id, err := d.open(e.Name)
	if err != nil {
		return r, Unpassable
	}
	end, reason := d.readFrom(id, e.Pointer, passes+1, nodeview.Deref(e.Node))
	if reason != NoReason {
		return r, Unpassable
	}
	end.state = r.state
	return end, NoReason
}

// schemaShaped reports whether n can be read as a schema: a mapping, or a
// boolean.
func schemaShaped(n *yaml.Node) bool {
	n = nodeview.Deref(n)
	return n != nil && (n.Kind == yaml.MappingNode || n.Kind == yaml.ScalarNode && n.Tag == "!!bool")
}
