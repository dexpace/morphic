package load

import (
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"iter"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// restoring names the work of putting $refs back as written in a panic it
// recovers from.
const restoring = "restoring $refs as written"

// escapes holds each $ref value load respelled for the resolver in a tree it
// builds models from, by the node in the $ref's place, with the value written
// there, and each node it changed as it was (GitHub #911).
//
// The parser reads '+' in a fragment as a space, and builds models from raw
// YAML as well as from the source's model. So the spelling is fixed in the
// trees before anything is built, and put back once it is done: the
// IR, verbatim copies and diagnostics see what the source writes (asWritten
// has the one exception). A nil *escapes does nothing.
type escapes struct {
	mu      sync.Mutex // prepare can run on several readers of one compile
	written map[*yaml.Node]string
	changed []changed
	// spellings maps each spelling the resolver was handed to the one the
	// source writes, the least of several; verbatim holds each $ref the source
	// writes with "%2B" itself. A restore keeps both, for asWritten.
	spellings map[string]string
	verbatim  map[string]bool
	// refuses returns why a read refuses the document a $ref's document part
	// names by that spelling alone, or nil (see forResolver); a nil refuses
	// refuses none. minted holds the refusal of each document part escape
	// respelled, by its respelling.
	refuses func(uri string) error
	minted  map[string]error
}

// changed is a node escape changed in place, and the node as it was.
type changed struct {
	node *yaml.Node
	was  yaml.Node
}

// newEscapes returns a registry holding nothing, which respells each $ref
// whose document part refuses refuses (see forResolver).
func newEscapes(refuses func(uri string) error) *escapes {
	return &escapes{written: map[*yaml.Node]string{}, spellings: map[string]string{}, verbatim: map[string]bool{},
		refuses: refuses, minted: map[string]error{}}
}

// refusedScheme is the scheme forResolver respells a refused document part
// under. The resolver hands a URL of any scheme to external.Do, which answers
// each one escape minted with its refusal, unread (see external.unnamed). The
// part is escaped whole, so the URL is opaque and net/url spells it back as
// forResolver wrote it.
const refusedScheme = "x-morphic-refused"

// refusalOf returns the refusal of the document part escape respelled to key,
// or nil for a key it minted none for.
func (e *escapes) refusalOf(key string) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.minted[key]
}

// refused returns the respelling of the document part of the $ref value v,
// and why e.refuses refuses it, or a nil reason where it refuses none.
func (e *escapes) refused(v string) (key string, why error) {
	if e == nil || e.refuses == nil {
		return "", nil
	}
	uri, _, _ := strings.Cut(v, "#")
	uri = strings.TrimSpace(uri)
	if why = e.refuses(uri); why == nil {
		return "", nil
	}
	return refusedScheme + ":" + url.PathEscape(uri), why
}

// refusal is bind.Config.Refuse for the document parts forResolver respells,
// or nil where it respells none: the binder reads each $ref as written.
func (e *escapes) refusal() func(uri string) error {
	if e == nil {
		return nil
	}
	return e.refuses
}

// forResolver returns the spelling the resolver is handed for the $ref value
// v: its fragment as fragment.ForResolver spells it, and a document part
// e.refuses refuses respelled under refusedScheme, so that the resolver asks
// for it where the read refuses it.
func (e *escapes) forResolver(v string) string {
	spelled := fragment.ForResolver(v)
	key, why := e.refused(spelled)
	if why == nil {
		return spelled
	}
	if _, rest, found := strings.Cut(spelled, "#"); found {
		return key + "#" + rest
	}
	return key
}

// escape respells, in the tree under root, each $ref value the resolver would
// misread to ForResolver's spelling, and records what it changed. A $ref
// written as an alias becomes a scalar of its own, so the scalar it names is
// not changed. One written as a scalar is changed, and an alias naming it
// elsewhere, which may be text, becomes a scalar holding it as written. Every
// node keeps its identity, which models and indexes of the tree are keyed by.
func (e *escapes) escape(root *yaml.Node) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	respelled := map[*yaml.Node]string{}
	eachRefValue(root, func(n *yaml.Node) bool {
		v := nodeview.Deref(n)
		spelled := e.forResolver(v.Value)
		if spelled == v.Value {
			if strings.Contains(v.Value, "%2B") {
				e.verbatim[v.Value] = true
			}
			return true
		}
		if was, ok := e.spellings[spelled]; !ok || v.Value < was {
			e.spellings[spelled] = v.Value
		}
		if key, why := e.refused(v.Value); why != nil {
			e.minted[key] = why
		}
		e.written[n] = v.Value
		if n == v {
			respelled[n] = v.Value
		}
		e.becomeScalar(n, v, spelled)
		return true
	})
	if len(respelled) == 0 {
		return
	}
	for _, a := range aliasesOutsideRefs(root) {
		if was, ok := respelled[a.Alias]; ok {
			e.becomeScalar(a, a.Alias, was)
		}
	}
}

// becomeScalar makes n the scalar v holding value, keeping n's own position
// and anchor, and records n as it was.
func (e *escapes) becomeScalar(n, v *yaml.Node, value string) {
	e.changed = append(e.changed, changed{node: n, was: *n})
	line, column, anchor := n.Line, n.Column, n.Anchor
	*n = *v
	n.Value, n.Line, n.Column, n.Anchor = value, line, column, anchor
}

// rewritable reports whether the tree under root holds a $ref value
// forResolver respells.
func (e *escapes) rewritable(root *yaml.Node) bool {
	found := false
	eachRefValue(root, func(n *yaml.Node) bool {
		v := nodeview.Deref(n)
		found = e.forResolver(v.Value) != v.Value
		return !found
	})
	return found
}

// eachRefValue calls visit with the node written as the value of each $ref
// key in the tree under root that is a scalar or an alias of one, until visit
// returns false. A value that is no scalar names no reference. The tree is
// walked by its content, never through an alias, so each mapping is read once.
func eachRefValue(root *yaml.Node, visit func(*yaml.Node) bool) {
	nodeview.Walk{Nodes: maxTreeNodes, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		for i, c := range n.Content {
			v := nodeview.Deref(c)
			if refValue(n, i) && v != nil && v.Kind == yaml.ScalarNode && !visit(c) {
				return nodeview.Stop
			}
		}
		return nodeview.Descend
	}}.Run(root)
}

// aliasesOutsideRefs returns every alias in the tree under root that is not
// the value of a $ref key, walked as eachRefValue walks.
func aliasesOutsideRefs(root *yaml.Node) []*yaml.Node {
	var out []*yaml.Node
	nodeview.Walk{Nodes: maxTreeNodes, Visit: func(n *yaml.Node, _ int) nodeview.Step {
		for i, c := range n.Content {
			if c.Kind == yaml.AliasNode && !refValue(n, i) {
				out = append(out, c)
			}
		}
		return nodeview.Descend
	}}.Run(root)
	return out
}

// refValue reports whether the node at index i of n's content is the value of
// a $ref key, read as the parser reads a key.
func refValue(n *yaml.Node, i int) bool {
	if n.Kind != yaml.MappingNode || i%2 == 0 {
		return false
	}
	key, ok := nodeview.KeyOf(n.Content[i-1])
	return ok && key == "$ref"
}

// empty reports whether nothing was rewritten.
func (e *escapes) empty() bool {
	if e == nil {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.written) == 0
}

// original returns the value n was written with, or false when it was not
// rewritten.
func (e *escapes) original(n *yaml.Node) (string, bool) {
	if e == nil {
		return "", false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.written[n]
	return v, ok
}

// writtenOf returns the $ref model carries as the source writes it, for a
// diagnostic to quote. It reads the scalar the model was built from, which the
// resolver and the load phase never retarget, where the model's own value may
// be rewritten or, for a held "#/$defs/..." reference, be its definition's
// pointer. A model built from no scalar, such as one the compiler made, answers
// with its own.
func (e *escapes) writtenOf(model any) references.Reference {
	if node, _, ok := refCopies(model); ok {
		if n := nodeview.Deref(node); n != nil && n.Kind == yaml.ScalarNode {
			if v, escaped := e.original(n); escaped {
				return references.Reference(v)
			}
			return references.Reference(n.Value)
		}
	}
	own, _ := model.(interface{ GetReference() references.Reference })
	if own == nil {
		return ""
	}
	return own.GetReference()
}

// trailOf is c.trail() with the reference the chain stopped at spelled as
// written, since the trail is only ever rendered.
func (e *escapes) trailOf(c chain) trail {
	t := c.trail()
	if t.stopped != "" {
		t.stopped = e.writtenOf(c.stoppedAt)
	}
	return t
}

// endOfText marks the end of a message, where a spelling may end as it may
// before a closing quote.
const endOfText = "\x00"

// asWritten puts back, in the messages of diags, each spelling the resolver
// was handed where the source writes another. The library's own text renders
// the model it was resolving as that model then held it, in a reason or in a
// whole value it could not use, which no model restore reaches.
//
// A spelling is put back only where it ends the message or a quoted string,
// never inside a longer reference. One the source writes itself is left
// alone, so where a source spells a reference both ways, the library's text
// may quote the "%2B" one.
func (e *escapes) asWritten(diags []ir.Diagnostic) []ir.Diagnostic {
	r := e.respellings()
	if r == nil {
		return diags
	}
	for i := range diags {
		diags[i].Message = strings.TrimSuffix(r.Replace(diags[i].Message+endOfText), endOfText)
	}
	return diags
}

// respellings returns a replacer from each spelling the resolver was handed to
// the source's, as the library writes each in text and as its JSON encoder
// writes it in a value, at the end of a message or of a quoted string. It
// returns nil when there is nothing to put back.
func (e *escapes) respellings() *strings.Replacer {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var pairs [][2]string
	for spelled, written := range e.spellings {
		if e.verbatim[spelled] {
			continue
		}
		pairs = append(pairs, [2]string{spelled, written})
		if s, w, ok := jsonQuoted(spelled, written); ok {
			pairs = append(pairs, [2]string{s, w})
		}
	}
	if len(pairs) == 0 {
		return nil
	}
	slices.SortFunc(pairs, func(a, b [2]string) int {
		return cmp.Or(strings.Compare(a[0], b[0]), strings.Compare(a[1], b[1]))
	})
	args := make([]string, 0, 4*len(pairs))
	for _, p := range pairs {
		args = append(args, p[0]+endOfText, p[1]+endOfText, p[0]+`"`, p[1]+`"`)
	}
	return strings.NewReplacer(args...)
}

// jsonQuoted returns spelled and written as the library's JSON encoder quotes
// them, escaping HTML and JavaScript line terminators, without the quotes, and
// whether that differs from spelled raw. A string JSON cannot encode is in no
// encoded value.
func jsonQuoted(spelled, written string) (string, string, bool) {
	quote := func(s string) (string, bool) {
		out, err := json.Marshal(s, jsontext.EscapeForHTML(true), jsontext.EscapeForJS(true))
		if err != nil {
			return "", false
		}
		return string(out[1 : len(out)-1]), true
	}
	s, sok := quote(spelled)
	w, wok := quote(written)
	if !sok || !wok || s == spelled {
		return "", "", false
	}
	return s, w, true
}

// restoreTrees puts every node escape changed back as it was, and forgets
// them, so a second call changes nothing.
func (e *escapes) restoreTrees() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.changed {
		*c.node = c.was
	}
	clear(e.written)
	e.changed = nil
}

// restore puts every tree and every model copy of a rewritten value back as
// written, ending the window in which the parser read the rewritten spelling.
// The models are the ones the lowering reads (see restorer) and the schemas
// built for the bindings' landings, and a fault finding them is returned with
// where it stopped; the trees are put back either way.
func (e *escapes) restore(ctx context.Context, doc *soa.OpenAPI, sibs Siblings, built []typed.Built) (jsontext.Pointer, error) {
	return e.restoreFrom(ctx, walkSource(ctx, doc, sibs), built)
}

// restoreFault reports the fault restore ended on, at the model it ended at,
// and nothing for no fault. It is no spec problem: the library faulted reading
// its own models.
func restoreFault(at func(jsontext.Pointer) ir.Provenance, site jsontext.Pointer, err error) []ir.Diagnostic {
	if err == nil {
		return nil
	}
	return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.InternalInvariant, at(site), "internal: %s", err.Error())}
}

// restoreFrom is restore over a walk of the source the caller supplies, so a
// test can hand it one that panics.
func (e *escapes) restoreFrom(ctx context.Context, items iter.Seq[soa.WalkItem], built []typed.Built) (jsontext.Pointer, error) {
	if e.empty() {
		return "", nil // no model holds a rewritten value, so nothing is walked
	}
	defer e.restoreTrees()
	r := &restorer{esc: e, seen: map[any]bool{}, walked: map[any]bool{}}
	site, err := eachSighting(items, restoring, func(_ jsontext.Pointer, _ soa.Locations, model any) error {
		r.add(model)
		return nil
	})
	if err != nil {
		return site, err
	}
	return "", r.finish(ctx, built)
}

// restorer puts the model copies of rewritten values back, starting from the
// models the lowering reads and following what each reaches: the chain of
// every reference, and what each object a chain reached holds, since the
// resolver builds those from raw YAML or another document and no walk of the
// source shows them. Each reference is visited once, and each object walked
// once.
type restorer struct {
	esc    *escapes
	seen   map[any]bool
	walked map[any]bool
	queue  []record
}

// add puts model's reference back as written when it was rewritten, and
// queues its chain, and reports whether the model was new to the restorer.
// Anything but one of the library's reference kinds is ignored.
func (r *restorer) add(model any) bool {
	node, put, ok := refCopies(model)
	if !ok || r.seen[model] {
		return false
	}
	r.seen[model] = true
	if v, rewritten := r.esc.original(nodeview.Deref(node)); rewritten {
		put(v)
	}
	r.queue = append(r.queue, resolutionChain(model).records...)
	return true
}

// finish adds the schemas built for the bindings' landings, which no walk of
// the source shows, and then what everything added reaches, behind the
// barrier the walks have: the library faults on shapes it did not expect.
func (r *restorer) finish(ctx context.Context, built []typed.Built) error {
	return runRecovered(restoring, func() error {
		for _, b := range built {
			if b.Schema != nil {
				r.addObject(ctx, record{object: b.Schema, walk: walkOf(b.Schema)})
			}
		}
		r.drain(ctx)
		return nil
	})
}

// drain adds the object of each queued hop and what it holds, until the queue
// is empty.
func (r *restorer) drain(ctx context.Context) {
	for len(r.queue) > 0 {
		rec := r.queue[0]
		r.queue = r.queue[1:]
		r.addObject(ctx, rec)
	}
}

// addObject adds the object rec reached, which need be no reference itself,
// and every model its walk yields.
func (r *restorer) addObject(ctx context.Context, rec record) {
	if r.walked[rec.object] {
		return
	}
	r.walked[rec.object] = true
	r.add(rec.object)
	eachWithin(ctx, rec, func(model any) { r.add(model) })
}

// eachWithin calls visit with each model the walk of rec's object yields.
func eachWithin(ctx context.Context, rec record, visit func(model any)) {
	for item := range rec.walk(ctx) {
		// Match returns only what its matcher does, and this one never fails.
		_ = item.Match(soa.Matcher{Any: func(model any) error {
			visit(model)
			return nil
		}})
	}
}

// refCopies returns the scalar model's $ref was read from, and a function
// putting a value into the copies of it the model and its core hold. ok is
// false for any other kind, a typed nil, or a model built from no scalar.
//
// put gives each copy a new pointer rather than writing through the old one,
// which a held "#/$defs/..." reference still shares.
func refCopies(model any) (node *yaml.Node, put func(string), ok bool) {
	if v := reflect.ValueOf(model); v.Kind() == reflect.Pointer && v.IsNil() {
		return nil, nil, false
	}
	switch r := model.(type) {
	case *schemaRef:
		s := r.GetSchema()
		if s == nil {
			return nil, nil, false
		}
		return copiesOf(&s.Ref, &s.GetCore().Ref)
	case *soa.ReferencedPathItem:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedParameter:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedHeader:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedRequestBody:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedResponse:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedExample:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedLink:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedCallback:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	case *soa.ReferencedSecurityScheme:
		return copiesOf(&r.Reference, &r.GetCore().Reference)
	default:
		return nil, nil, false
	}
}

// copiesOf is refCopies over the two copies of a $ref a model keeps: its own,
// and the core's, which records the scalar it came from.
func copiesOf(model **references.Reference, core *marshaller.Node[*string]) (*yaml.Node, func(string), bool) {
	if core.ValueNode == nil {
		return nil, nil, false
	}
	return core.ValueNode, func(v string) {
		ref := references.Reference(v)
		*model = &ref
		core.Value = &v
	}, true
}
