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
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// maxCanonicalDepth bounds the canonical form's descent: through the encoded
// document, and through the owners of each digest computed on the way, whose
// rewrite continues the descent that met the ID. The IR nests what it keeps
// verbatim below levels of its own: a chain of 255 schemas, each a property
// under an allOf, the deepest the permutation admits, needs 1,106 levels, past
// one source bound and within two. A form nesting deeper is declined.
const maxCanonicalDepth = 2 * maxReverseDepth

// errTooDeep says a canonical form would nest past maxCanonicalDepth, which
// the oracle declines as DeclineTooDeep.
var errTooDeep = errors.New("canonical form nests too deep")

// unorderedMark names the one member of the object markedArray writes.
const unorderedMark = "\x00unordered"

// markedArray writes elems as they stand, inside an object marking them as a
// collection whose order the comparison forgives, so the rewrite sorts them
// once it has rewritten them. No member of the IR is so named.
func markedArray(elems [][]byte) []byte {
	return append(append([]byte(`{"\u0000unordered":`), array(elems)...), '}')
}

// canonical is a document's canonical form: its lines, and its shape's.
type canonical struct{ document, shape []string }

// canonicalForm is doc as the sequence-order oracle compares it, where seqs
// are the sequences reversed, in doc's source's coordinates, and ties the
// erased pointers of the reports that moved. It forgives an index of a
// reversed sequence a string names, an ID beneath one being told apart by its
// owners' digest; the order of what orderFree lists, mixins, requires and an
// array kept verbatim at a reversed sequence's pointer; and every node at or
// beneath a tie.
func canonicalForm(doc *ir.Document, seqs sequences, ties []string) (canonical, error) {
	encoded, err := encodeComparable(doc, markedArray)
	if err != nil {
		return canonical{}, err
	}
	r := rewriter{seqs: seqs, ties: ties, owners: map[string][]map[string]any{},
		digests: map[string]string{}, visiting: map[string]bool{}}
	tree := r.decode(encoded)
	r.index(tree, 0)
	out := jsontext.Value(r.rewrite(tree, place{}, 0))
	r.fail(out.Indent(jsontext.WithIndent("  ")))
	shape := r.shape(doc)
	return canonical{document: lines(out), shape: shape}, r.err
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

// rewriter is the state of one canonicalForm: the sequences and tie sites it
// reads, the owners of each ID erasing changes, the digests computed so far,
// the IDs whose digest is being computed, and the first error met.
type rewriter struct {
	seqs     sequences
	ties     []string
	owners   map[string][]map[string]any
	digests  map[string]string
	visiting map[string]bool
	err      error
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
		for _, member := range v {
			r.index(member, depth+1)
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

// object rewrites an object: a marked collection as a sorted array, one at or
// beneath a tie site as the tie, and any other member by member, sorted by
// name and then by value, since erased names can collide. An id is erased
// alone.
func (r *rewriter) object(o map[string]any, p place, depth int) []byte {
	if elems, ok := o[unorderedMark].([]any); ok && len(o) == 1 {
		return r.array(elems, place{how: p.how&noIdentity | sortElements}, depth)
	}
	own := provenancePointer(o)
	if erased, tied := r.tiedAt(own); tied && p.how&noIdentity == 0 {
		return slices.Concat([]byte(`{"reportedTie":`), r.quote(erased), []byte("}"))
	}
	type member struct{ name, value []byte }
	members := make([]member, 0, len(o))
	for name, v := range o {
		if p.how&noIdentity != 0 && (name == "id" || name == "provenance") {
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
	if len(r.owners[s]) == 0 || r.visiting[s] {
		return erased
	}
	return erased + "#" + r.digest(s, depth)
}

// digest is the first six bytes, in hex, of the SHA-256 of the canonical
// encodings of id's owners, sorted and joined by newlines. An ID met while its
// own digest is computed is written erased alone, which ends a cycle.
func (r *rewriter) digest(id string, depth int) string {
	if d, ok := r.digests[id]; ok {
		return d
	}
	r.visiting[id] = true
	encodings := make([]string, 0, len(r.owners[id]))
	for _, o := range r.owners[id] {
		encodings = append(encodings, string(r.rewrite(o, place{}, depth+1)))
	}
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
	return erased, slices.ContainsFunc(r.ties, func(site string) bool {
		return erased == site || strings.HasPrefix(erased, site) && strings.HasPrefix(erased[len(site):], "/")
	})
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
		if _, ok := t.(*ir.Model); !ok {
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
// so two declarations saying the same are one.
func (r *rewriter) memberShape(p ir.FlatProperty) string {
	prov := p.Property.Provenance
	if erased, tied := r.tiedAt(string(prov.Pointer)); tied {
		return "reported tie at " + erased
	}
	encoded, err := json.Marshal(struct {
		Declaration *ir.Property    `json:"declaration"`
		Deprecation *ir.Deprecation `json:"deprecation"`
		Required    bool            `json:"required"`
		Secret      bool            `json:"secret"`
	}{p.Property, p.Deprecation, p.Required, p.Secret}, comparableOptions(markedArray))
	r.fail(err)
	return string(r.rewrite(r.decode(encoded), place{how: noIdentity}, 0))
}
