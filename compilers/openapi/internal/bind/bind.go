package bind

import (
	"encoding/json/jsontext"
	"slices"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// Status is how the chain from a reference site ended.
type Status uint8

const (
	// Resolved is a chain that ends at a schema, or at raw YAML read as one.
	Resolved Status = iota + 1
	// Unresolved is a chain that names a position holding no schema, or none.
	Unresolved
	// External is a chain that names another document, which a table does not
	// read.
	External
	// Anchor is a chain that names a $anchor, which the compiler does not
	// honour (ir-design §4.7, GitHub #141).
	Anchor
	// Cyclic is a chain that comes back to a position it already read.
	Cyclic
	// OverBudget is a chain the work budget stopped.
	OverBudget
)

// statusNames spells each Status, in order.
var statusNames = []string{"resolved", "unresolved", "external", "anchor", "cyclic", "over-budget"}

// String spells s, or "invalid" for a value no constant names.
func (s Status) String() string {
	if s < Resolved || int(s) > len(statusNames) {
		return "invalid"
	}
	return statusNames[s-1]
}

// Entry is one schema reference site and where its chain ends.
type Entry struct {
	Site   jsontext.Pointer // where the $ref is written
	Ref    string           // the $ref as written
	First  jsontext.Pointer // the position its own $ref names, once read
	Target jsontext.Pointer // the position the chain ends at, when Resolved
	Role   Role             // what Target holds, when Resolved
	Hops   int              // the $refs the chain read
	Status Status
}

// DefaultLimit bounds the work one table costs: each node read, token walked
// and hop followed is a step.
const DefaultLimit = 1 << 24

// Config is how a Binder reads its document.
type Config struct {
	// Names reports whether a reference's document part names this document;
	// nil names only an empty one.
	Names func(uri string) bool
	// Limit bounds the work; zero is DefaultLimit.
	Limit int
}

// Binder resolves the schema $refs of one document over its YAML tree, read
// through nodeview as the parser reads it, and never through the parser's own
// resolver. It is synchronous and keeps no state between documents.
type Binder struct {
	view  *nodeview.View
	root  *yaml.Node
	cfg   Config
	steps int
	bases map[jsontext.Pointer]base
}

// New returns a Binder over root, a decoded document's root node.
func New(root *yaml.Node, cfg Config) *Binder {
	if cfg.Limit <= 0 {
		cfg.Limit = DefaultLimit
	}
	if cfg.Names == nil {
		cfg.Names = func(uri string) bool { return uri == "" }
	}
	return &Binder{view: nodeview.New(), root: nodeview.DocumentRoot(root), cfg: cfg,
		bases: map[jsontext.Pointer]base{}}
}

// spend charges n steps and reports whether the budget still holds.
func (b *Binder) spend(n int) bool {
	b.steps += n
	return b.steps <= b.cfg.Limit
}

// Resolve follows the schema $ref written at site to the end of its chain. A
// site holding no $ref is Unresolved with no hop read. An answer the budget ran
// out before is OverBudget, whatever the chain had read by then.
func (b *Binder) Resolve(site jsontext.Pointer) Entry {
	e := b.chain(site)
	if b.steps > b.cfg.Limit {
		e.Target, e.Role, e.Status = "", NoRole, OverBudget
	}
	return e
}

// chain is Resolve's walk of the chain from site, its answer judged by the
// budget after.
func (b *Binder) chain(site jsontext.Pointer) Entry {
	e := Entry{Site: site, Status: Unresolved}
	ref, ok := b.refOf(b.at(site))
	if !ok {
		return e
	}
	e.Ref = ref
	seen := map[jsontext.Pointer]bool{site: true}
	for at := site; b.spend(1); e.Hops++ {
		next, status := b.hop(at, ref)
		if status != Resolved {
			e.Status = status
			return e
		}
		if e.Hops == 0 {
			e.First = next
		}
		if seen[next] {
			e.Hops, e.Status = e.Hops+1, Cyclic
			return e
		}
		seen[next], at = true, next
		if ref, ok = b.refOf(b.at(at)); !ok {
			e.Hops, e.Target, e.Role, e.Status = e.Hops+1, at, roleOf(tokens(at)), Resolved
			return e
		}
	}
	return e
}

// hop is the position ref, written at at, names, and Resolved where it names
// one a schema reference may: a schema, or raw YAML.
func (b *Binder) hop(at jsontext.Pointer, ref string) (jsontext.Pointer, Status) {
	uri, _, hasFragment := strings.Cut(ref, "#")
	if uri = strings.TrimSpace(uri); uri != "" && !b.cfg.Names(uri) {
		return "", External
	}
	pointer := fragment.Pointer(ref)
	if hasFragment && pointer != "" && !strings.HasPrefix(string(pointer), "/") {
		return "", Anchor
	}
	var target jsontext.Pointer
	var found bool
	if uri == "" && strings.HasPrefix(string(pointer), defsPrefix) {
		target, found = b.defs(at, pointer)
	} else {
		target, found = b.fromRoot(pointer)
	}
	if !found {
		return "", Unresolved
	}
	if r := roleOf(tokens(target)); r != Schema && r != Raw {
		return "", Unresolved
	}
	return target, Resolved
}

// fromRoot is the position pointer names from the document's root, and whether
// the document holds a node there. A lone '/' names the root, as the parser
// reads it (nodeview.PointerTarget).
func (b *Binder) fromRoot(pointer jsontext.Pointer) (jsontext.Pointer, bool) {
	if pointer == "/" {
		pointer = ""
	}
	return pointer, b.at(pointer) != nil
}

// at is the node the document holds at pointer, or nil, charging a step a
// token walked.
func (b *Binder) at(pointer jsontext.Pointer) *yaml.Node {
	path, complete := b.view.DocumentPath(b.root, pointer)
	b.spend(len(path))
	if !complete {
		return nil
	}
	return path[len(path)-1]
}

// refOf is the $ref n carries, read through the view, and whether it carries a
// string one: a null $ref is none, as the parser reads it.
func (b *Binder) refOf(n *yaml.Node) (string, bool) {
	if n = nodeview.Deref(n); n == nil || n.Kind != yaml.MappingNode {
		return "", false
	}
	v := nodeview.Deref(b.view.ChildByToken(n, "$ref"))
	if v == nil || v.Kind != yaml.ScalarNode || v.Tag == "!!null" {
		return "", false
	}
	return v.Value, true
}

// tokens are pointer's tokens.
func tokens(pointer jsontext.Pointer) []string {
	return slices.Collect(pointer.Tokens())
}
