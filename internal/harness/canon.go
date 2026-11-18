// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// maxCanonicalDepth bounds the canonical form's descent: through the encoded
// document, and through the owners of each digest computed on the way, whose
// rewrite continues the descent that met the ID. How deep that goes follows
// the order the rewrite meets IDs in, which is why it reads an object's
// members in name order: a form is declined on every run or on none
// (TestSequenceOrderInvariant_GivesOneVerdictOnEveryRun). A chain of 255
// schemas, each a property under an allOf, the deepest the permutation admits,
// nests within two source bounds in any order, each link's digest reaching
// through the next (TestCheck_SequenceOrderOracleHoldsOnTheRealCompiler).
const maxCanonicalDepth = 2 * maxReverseDepth

// errTooDeep says a canonical form would nest past maxCanonicalDepth, which
// the oracle declines as DeclineTooDeep.
var errTooDeep = errors.New("canonical form nests too deep")

// errCycle says two or more IDs with owners name each other, whose digests
// would follow the order the rewrite meets them in, which the oracle declines
// as DeclineReferenceCycle.
var errCycle = errors.New("canonical form meets a cycle of digests")

// unorderedMark names the one member of the object markedArray writes.
const unorderedMark = "\x00unordered"

// markedArray writes elems as they stand, inside an object marking them as a
// collection whose order the comparison forgives, so the rewrite sorts them
// once it has rewritten them. No member of the IR is so named.
func markedArray(elems [][]byte) []byte {
	return append(append([]byte(`{"\u0000unordered":`), array(elems)...), '}')
}

// canonical is a document's canonical form: its lines, its shape's, and how it
// writes an ID (rewriter.identify).
type canonical struct {
	document, shape []string
	identify        func(id string) (string, bool)
}

// canonicalForm is doc as the sequence-order oracle compares it, where seqs
// are the sequences reversed, in doc's source's coordinates, and ties what
// the reports that moved decided. It forgives an index of
// a reversed sequence a string names, an ID beneath one told apart by its
// owners' digest; the order of what orderFree lists, mixins, requires and an
// array kept verbatim at a reversed sequence's pointer; and each node a tie
// decided (tied), with what is keyed by its ID (untie). An unmodeled member's
// name is relative, so an index after allOf or required in it is always
// erased.
func canonicalForm(doc *ir.Document, seqs sequences, ties tieSites) (canonical, error) {
	r, err := newRewriter(doc, seqs, ties)
	if err != nil {
		return canonical{}, err
	}
	out := jsontext.Value(r.rewrite(r.tree, place{}, 0))
	r.fail(out.Indent(jsontext.WithIndent("  ")))
	shape := r.shape(doc)
	return canonical{document: lines(out), shape: shape, identify: r.identify}, r.err
}

// newRewriter returns a rewriter of doc's forms reading ties, where seqs are
// the sequences reversed, in doc's source's coordinates, holding doc's
// encoding decoded, each owner of an ID in it indexed.
func newRewriter(doc *ir.Document, seqs sequences, ties tieSites) (*rewriter, error) {
	encoded, err := encodeComparable(doc, markedArray, nil)
	if err != nil {
		return nil, err
	}
	r := &rewriter{
		seqs: seqs, ties: ties.pointers, members: ties.members, nodes: map[string]bool{}, stated: map[string]bool{},
		owners:  map[string][]map[string]any{},
		digests: map[string]string{}, visiting: map[string]bool{},
	}
	for _, node := range ties.nodes {
		r.nodes[node] = true
	}
	for _, node := range ties.stated {
		r.stated[node] = true
	}
	r.tree = r.decode(encoded)
	r.index(r.tree, 0)
	return r, nil
}

// number is a JSON number's text, which the rewrite writes as it stands.
type number string

// rawNumber decodes a JSON number as its text, which no Go number type holds
// whole.
func rawNumber(dec *jsontext.Decoder, v *any) error {
	if dec.PeekKind() != '0' {
		return errors.ErrUnsupported
	}
	raw, err := dec.ReadValue()
	*v = number(raw)
	return err
}

// placement is what a node's place tells the rewrite about it.
type placement uint8

const (
	// sortElements: an array whose order the comparison forgives.
	sortElements placement = 1 << iota
	// relativeNames: an object whose member names are pointers relative to
	// the node holding it, as an unmodeled map's are.
	relativeNames
	// noIdentity: a node to write without its id and provenance members,
	// whatever its depth beneath the node first so placed.
	noIdentity
	// verbatim: a node of a value the IR keeps as the source wrote it, at the
	// pointer its place holds.
	verbatim
)

// place is where a node sits: what its place tells the rewrite, and, within
// a verbatim value, the node's pointer in the source the sequences are in.
type place struct {
	at  string
	how placement
}

// rewriter is the state of a document's forms: the sequences, the document's
// encoding decoded, the tie sites, tied members' pointers and tied nodes it
// reads, the owners of each
// ID erasing changes, the digests computed so far, the IDs whose digest is
// being computed and the innermost of them, and the first error met.
type rewriter struct {
	seqs      sequences
	tree      any
	ties      []string
	members   []string
	nodes     map[string]bool
	stated    map[string]bool
	owners    map[string][]map[string]any
	digests   map[string]string
	visiting  map[string]bool
	computing string
	err       error
}

// fail records err, unless an earlier error is recorded.
func (r *rewriter) fail(err error) { r.err = cmp.Or(r.err, err) }

// decode reads an encoding into a tree of objects, arrays, strings, numbers
// as their text, booleans and nulls.
func (r *rewriter) decode(encoded []byte) any {
	var tree any
	r.fail(json.Unmarshal(encoded, &tree, json.WithUnmarshalers(json.UnmarshalFromFunc(rawNumber))))
	return tree
}

// index records, below v, each owner of an ID: an object holding it as its id
// member, where erasing changes the ID.
func (r *rewriter) index(v any, depth int) {
	if depth > maxCanonicalDepth {
		r.fail(errTooDeep)
		return
	}
	switch v := v.(type) {
	case map[string]any:
		if id, ok := v["id"].(string); ok && erase(id, r.seqs) != id {
			r.owners[id] = append(r.owners[id], v)
		}
		for _, name := range slices.Sorted(maps.Keys(v)) {
			r.index(v[name], depth+1)
		}
	case []any:
		for _, element := range v {
			r.index(element, depth+1)
		}
	default:
		// A scalar owns nothing.
	}
}

// rewrite returns v's canonical encoding, compact, as its place says.
func (r *rewriter) rewrite(v any, p place, depth int) []byte {
	if depth > maxCanonicalDepth {
		r.fail(errTooDeep)
		return []byte("null")
	}
	switch v := v.(type) {
	case map[string]any:
		return r.object(v, p, depth)
	case []any:
		return r.array(v, p, depth)
	case string:
		return r.quote(r.text(v, depth))
	case number:
		return []byte(v)
	default:
		b, err := json.Marshal(v) // a boolean or a null
		r.fail(err)
		return b
	}
}

// object rewrites an object: a marked collection as a sorted array, a tied one
// (tied) as the tie, and any other member by member but those it leaves out
// (leftOut), sorted by name and then by value, since erased names can collide.
// An id is erased alone.
func (r *rewriter) object(o map[string]any, p place, depth int) []byte {
	if elems, ok := o[unorderedMark].([]any); ok && len(o) == 1 {
		return r.array(elems, place{how: p.how&noIdentity | sortElements}, depth)
	}
	o = r.untie(o)
	own := provenancePointer(o)
	if erased, tied := r.tied(o); tied && p.how&noIdentity == 0 {
		return slices.Concat([]byte(`{"reportedTie":`), r.quote(erased), r.foldedOf(o, depth), r.statedOf(o, depth), []byte("}"))
	}
	type member struct{ name, value []byte }
	members := make([]member, 0, len(o))
	for _, name := range slices.Sorted(maps.Keys(o)) {
		v := o[name]
		if r.leftOut(name, v, p) {
			continue
		}
		key := r.text(name, depth)
		if p.how&relativeNames != 0 {
			key = eraseRelative(key)
		}
		var value []byte
		switch text, isText := v.(string); {
		case isText && name == "id":
			value = r.quote(erase(text, r.seqs))
		default:
			value = r.rewrite(v, childPlace(name, p, own), depth+1)
		}
		members = append(members, member{r.quote(key), value})
	}
	slices.SortFunc(members, func(a, b member) int {
		return cmp.Or(bytes.Compare(a.name, b.name), bytes.Compare(a.value, b.value))
	})
	out := []byte{'{'}
	for i, m := range members {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(append(append(out, m.name...), ':'), m.value...)
	}
	return append(out, '}')
}

// foldedOf writes what o, a tied node, holds that its fold settles from every
// declaration alike, where it is a property: whether it is required or secret
// where any declaration or its model says so, and where it is visible, the
// lifecycles every declaration admits
// (docs/compilers/openapi.md#allof-redeclared-property). No tie between two of
// them decides these, so they are compared where nothing else of the member is.
func (r *rewriter) foldedOf(o map[string]any, depth int) []byte {
	required, isProperty := o["required"].(bool)
	if _, named := o["wireName"]; !named || !isProperty {
		return nil
	}
	secret, _ := o["secret"].(bool)
	out := []byte(`,"required":` + strconv.FormatBool(required) + `,"secret":` + strconv.FormatBool(secret) + `,"visibility":`)
	return append(out, r.rewrite(o["visibility"], place{}, depth+1)...)
}

// statedOf writes what else of o, a tied node, folds over every declaration,
// where a stated tie alone decided it (tieSites.stated): its deprecation, docs,
// name and XML hints. Such a tie decides none of them; two declarations giving
// one differently draw a report of their own, which moves with the order and so
// reaches the member as another tie.
func (r *rewriter) statedOf(o map[string]any, depth int) []byte {
	id, _ := o["id"].(string)
	if !r.stated[erase(id, r.seqs)] {
		return nil
	}
	var out []byte
	for _, name := range [...]string{"deprecation", "docs", "name", "xml"} {
		if v, written := o[name]; written {
			out = append(append(append(out, `,"`...), name...), `":`...)
			out = append(out, r.rewrite(v, place{}, depth+1)...)
		}
	}
	return out
}

// childPlace is the place of the member name of an object at p, whose
// provenance's pointer is own: within a verbatim value, or the verbatim value
// an object keeps beside its provenance; mixins and requires, which mirror
// allOf and required whatever the source's order; or the unmodeled map.
func childPlace(name string, p place, own string) place {
	child := place{how: p.how & noIdentity}
	switch {
	case p.how&verbatim != 0:
		return place{how: child.how | verbatim, at: string(appendToken([]byte(p.at), name))}
	case name == "value" && own != "":
		return place{how: child.how | verbatim, at: own}
	case name == "mixins", name == "requires":
		child.how |= sortElements
	case name == "unmodeled":
		child.how |= relativeNames
	}
	return child
}

// array rewrites an array's elements, sorted when its place says so or when,
// within a verbatim value, it is a sequence the permutation reversed.
func (r *rewriter) array(elems []any, p place, depth int) []byte {
	out := make([][]byte, len(elems))
	for i, e := range elems {
		child := place{how: p.how & noIdentity}
		if p.how&verbatim != 0 {
			child = place{how: child.how | verbatim, at: p.at + "/" + strconv.Itoa(i)}
		}
		out[i] = r.rewrite(e, child, depth+1)
	}
	if p.how&sortElements != 0 || p.how&verbatim != 0 && r.seqs[p.at] > 0 {
		return sortedArray(out)
	}
	return array(out)
}

// text is s erased, followed, where s is an ID with owners, by their digest.
func (r *rewriter) text(s string, depth int) string {
	erased := erase(s, r.seqs)
	if len(r.owners[s]) == 0 {
		return erased
	}
	if r.visiting[s] {
		if s != r.computing {
			r.fail(errCycle)
		}
		return erased
	}
	return erased + "#" + r.digest(s, depth)
}

// identify is id as the form writes a reference to it (text), for a report
// naming the node, compared once the form is written. It is false where no
// node the form holds is id, whose erased ID alone tells two nodes apart by
// nothing they hold, and where the digest fails as a form does (errTooDeep,
// errCycle).
func (r *rewriter) identify(id string) (string, bool) {
	if len(r.owners[id]) == 0 {
		return "", false
	}
	text := r.text(id, 0)
	return text, r.err == nil
}

// digest is the first six bytes, in hex, of the SHA-256 of the canonical
// encodings of id's owners, sorted and joined by newlines. An ID met while its
// own digest is computed is written erased alone, which ends a self-reference.
// One met while a digest it leads to is computed closes a cycle of two or
// more, whose digests would follow which of them the rewrite met first, so it
// fails the form with errCycle.
func (r *rewriter) digest(id string, depth int) string {
	if d, ok := r.digests[id]; ok {
		return d
	}
	r.visiting[id] = true
	outer := r.computing
	r.computing = id
	encodings := make([]string, 0, len(r.owners[id]))
	for _, o := range r.owners[id] {
		encodings = append(encodings, string(r.rewrite(o, place{}, depth+1)))
	}
	r.computing = outer
	delete(r.visiting, id)
	slices.Sort(encodings)
	sum := sha256.Sum256([]byte(strings.Join(encodings, "\n")))
	d := fmt.Sprintf("%x", sum[:6])
	r.digests[id] = d
	return d
}

// quote is s as a JSON string.
func (r *rewriter) quote(s string) []byte {
	b, err := jsontext.AppendQuote(nil, s)
	r.fail(err)
	return b
}

// tiedAt returns a provenance's pointer erased, and whether it is at or
// beneath a tie site.
func (r *rewriter) tiedAt(pointer string) (string, bool) {
	erased := erase(pointer, r.seqs)
	return erased, atSite(r.ties, erased)
}

// leftOut reports whether an object at p leaves its member name, holding v, out
// of its form: an identity a place reads without, or a node it keys by its ID
// that a tie decided, as a registry does, since a tie decides whether a fold
// mints it.
func (r *rewriter) leftOut(name string, v any, p place) bool {
	if p.how&noIdentity != 0 {
		return name == "id" || name == "provenance"
	}
	node, isObject := v.(map[string]any)
	if !isObject || node["id"] != name {
		return false
	}
	_, tied := r.tied(node)
	return tied
}

// tied returns how a tie reads o, and whether one decided it: o's pointer
// erased where it is at or beneath a tie site, o is a node a tied member's
// fold minted (minted), or a tie names o where a tied member stands, and its
// ID erased where a tie names it anywhere else.
func (r *rewriter) tied(o map[string]any) (string, bool) {
	erased, atSite := r.tiedAt(provenancePointer(o))
	id, hasID := o["id"].(string)
	named := hasID && r.nodes[erase(id, r.seqs)]
	switch {
	case atSite, hasID && r.minted(id, erased), named && r.atMember(erased):
		return erased, true
	case named:
		return erase(id, r.seqs), true
	default:
		return "", false
	}
}

// atMember reports whether erased, an erased pointer, is where a tied member
// stands.
func (r *rewriter) atMember(erased string) bool {
	_, at := slices.BinarySearch(r.members, erased)
	return at
}

// tiedNode reports whether a tie decided the node td, identified by id: it sits
// at or beneath a tie site, a tied member's fold minted it, or a tie names it.
func (r *rewriter) tiedNode(id ir.TypeID, td ir.TypeDef) bool {
	erased, atSite := r.tiedAt(string(td.Common().Provenance.Pointer))
	return atSite || r.minted(string(id), erased) || r.nodes[erase(string(id), r.seqs)]
}

// minted reports whether the node id, at erased, an erased pointer, is one a
// tied member's fold minted: in the composed namespace, at the member's
// pointer or beneath it at a position the fold builds a member at
// (docs/compilers/openapi.md#allof-redeclared-property), so which declaration
// won decides it. A node written in the source beneath the member, alike in
// either order, is not. The oracle reads one source, so no space is qualified.
func (r *rewriter) minted(id, erased string) bool {
	if parts := strings.SplitN(id, ir.IDSeparator, 3); len(parts) < 2 || parts[1] != "composed" {
		return false
	}
	return slices.ContainsFunc(r.members, func(member string) bool {
		rest, beneath := strings.CutPrefix(erased, member)
		return beneath && mintedPosition(rest)
	})
}

// mintedPosition reports whether rest, what follows a tied member's pointer in
// another, is empty or names a position a fold builds a member at, a level
// down each: items, additionalProperties, or prefixItems and an index.
func mintedPosition(rest string) bool {
	if rest == "" {
		return true
	}
	tokens, rooted := strings.CutPrefix(rest, "/")
	if !rooted {
		return false
	}
	position := strings.Split(tokens, "/")
	for i := 0; i < len(position); i++ {
		switch position[i] {
		case "items", "additionalProperties":
		case "prefixItems":
			if i++; i == len(position) || !decimal(position[i]) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// untie returns o without each member keyed by a tied node's ID, as a part's
// encoding is keyed by its property's: what it holds follows that node. o is
// copied before a member is dropped.
func (r *rewriter) untie(o map[string]any) map[string]any {
	out := o
	for name := range o {
		if !r.nodes[erase(name, r.seqs)] {
			continue
		}
		if len(out) == len(o) {
			out = maps.Clone(o)
		}
		delete(out, name)
	}
	return out
}

// provenancePointer reads the pointer of an object's provenance member. It is
// a pointer of the compiled source, the one the sequences are in, since the
// oracle declines a document read from more than one.
func provenancePointer(o map[string]any) string {
	prov, _ := o["provenance"].(map[string]any)
	pointer, _ := prov["pointer"].(string)
	return pointer
}

// eraseRelative writes as "-" each decimal token of a relative pointer that
// follows an order-free keyword: openapi:allOf/1 names a branch by its index,
// and openapi:oneOf/1 a variant whose order the IR keeps.
func eraseRelative(name string) string {
	tokens := strings.Split(name, "/")
	for i := 1; i < len(tokens); i++ {
		previous := tokens[i-1]
		if decimal(tokens[i]) && orderFreeKeyword(previous[strings.LastIndexByte(previous, ':')+1:]) {
			tokens[i] = "-"
		}
	}
	return strings.Join(tokens, "/")
}

// shape is a line per member of each model's flattened shape: the model's
// rewritten ID, the wire name, and what the member takes, stripped of the
// declaration's identity, or the tie its declaration sits at. A line more
// lists the model's undeclared and unflattened names where either holds any.
func (r *rewriter) shape(doc *ir.Document) []string {
	var out []string
	for id, t := range doc.Types {
		if _, ok := t.(*ir.Model); !ok || r.tiedNode(id, t) {
			continue
		}
		f := ir.FlattenedProperties(doc.Types, id)
		model := r.text(string(id), 0)
		for _, p := range f.Properties {
			out = append(out, model+" member "+strconv.Quote(p.Property.WireName)+" "+r.memberShape(p))
		}
		if len(f.Undeclared)+len(f.Unflattened) == 0 {
			continue
		}
		unflattened := make([]string, len(f.Unflattened))
		for i, u := range f.Unflattened {
			unflattened[i] = r.text(string(u), 0)
		}
		out = append(out, fmt.Sprintf("%s undeclared %q unflattened %q", model,
			slices.Sorted(slices.Values(f.Undeclared)), slices.Sorted(slices.Values(unflattened))))
	}
	slices.Sort(out)
	return out
}

// memberShape is what a flattened member takes: whether it is required,
// secret and deprecated, and its declaration, rewritten without its identity,
// so two declarations saying the same are one. Of a member a tie decided, it
// is the tie, whether the member is required, whether its declaration is
// secret, which a Sensitive type the tie picks does not decide, and where it is
// visible (foldedOf), and, where a stated tie alone decided it, what statedOf
// writes.
func (r *rewriter) memberShape(p ir.FlatProperty) string {
	prov := p.Property.Provenance
	id := erase(string(p.Property.ID), r.seqs)
	erased, atSite := r.tiedAt(string(prov.Pointer))
	minted := r.minted(string(p.Property.ID), erased)
	if atSite || minted || r.nodes[id] {
		tie := "reported tie " + id
		if atSite || minted || r.atMember(erased) {
			tie = "reported tie at " + erased
		}
		visibility, err := json.Marshal(&p.Property.Visibility, json.Deterministic(true))
		r.fail(err)
		tie += " required " + strconv.FormatBool(p.Required) + " secret " + strconv.FormatBool(p.Property.Secret) +
			" visibility " + string(visibility)
		if !r.stated[id] {
			return tie
		}
		stated, err := json.Marshal(struct {
			Deprecation *ir.Deprecation `json:"deprecation"`
			Docs        ir.Docs         `json:"docs"`
			Name        ir.Naming       `json:"name"`
			XML         *ir.XMLHints    `json:"xml"`
		}{p.Property.Deprecation, p.Property.Docs, p.Property.Name, p.Property.XML}, json.Deterministic(true))
		r.fail(err)
		return tie + " stated " + string(stated)
	}
	encoded, err := json.Marshal(struct {
		Declaration *ir.Property    `json:"declaration"`
		Deprecation *ir.Deprecation `json:"deprecation"`
		Required    bool            `json:"required"`
		Secret      bool            `json:"secret"`
	}{p.Property, p.Deprecation, p.Required, p.Secret}, comparableOptions(markedArray, nil))
	r.fail(err)
	return string(r.rewrite(r.decode(encoded), place{how: noIdentity}, 0))
}
