// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/dexpace/morphic/ir"
)

// maxSpellingDepth bounds the walk differsIn makes of an encoded document,
// which nests no deeper than the IR it encodes.
const maxSpellingDepth = 2 * maxReverseDepth

// spelled holds the case at, which writes doc, to the partner spelling it must
// equal: both compiled, each folded and encoded, every string of each relocated
// to the partner's coordinates, and the diagnostics compared as a multiset once
// the reports the spelling states are taken out. A difference is
// OutcomeSpellingDependent, its Detail's first line naming where they differ.
// A compile's error, a document that does not encode and a partner the product
// writes nothing for are the harness's or the product's fault, a panic, never
// a finding a record could list.
func spelled(ctx context.Context, s *shard, at []int, doc Document) Verdict {
	eq := doc.Equal
	if eq == nil {
		return declined(OracleSpelling, DeclineNoPartner)
	}
	if eq.Relocate == nil || eq.Rule == "" {
		panic(fmt.Sprintf("harness: case %v names a partner with no relocation or no rule", at))
	}
	mine, ok := s.compile(ctx, at)
	if !ok {
		panic(fmt.Sprintf("harness: case %v writes nothing to compile", at))
	}
	theirs, ok := s.compile(ctx, eq.To)
	if !ok {
		panic(fmt.Sprintf("harness: case %v names the partner %v, which writes nothing", at, eq.To))
	}
	if err := cmp.Or(mine.err, theirs.err); err != nil {
		panic(fmt.Sprintf("harness: compile case %v or its partner: %v", at, err))
	}
	if refused := oneSideRefused(mine.doc, theirs.doc); refused != "" {
		return found(OracleSpelling, refused+"\n"+eq.Rule)
	}

	a, b, why, err := comparableSides(mine.doc, theirs.doc, eq.Relocate)
	if err != nil {
		panic(fmt.Sprintf("harness: encode case %v or its partner: %v", at, err))
	}
	if why != "" {
		return declined(OracleSpelling, why)
	}
	diags, lost := comparableDiagnostics(mine.diags, theirs.diags, eq)
	if what := differsIn(a, b, diags, lost); what != "" {
		partner, _ := s.cases.Write(slices.Clone(eq.To))
		return found(OracleSpelling, spellingDetail(what, eq.Rule, a, b, diags, doc, partner))
	}
	return passed(OracleSpelling)
}

// oneSideRefused says which of a spelling and its partner alone compiled to no
// document, and is empty when both or neither did.
func oneSideRefused(mine, theirs *ir.Document) string {
	switch {
	case mine == nil && theirs != nil:
		return "refused: this spelling compiles to no document, and its partner to one"
	case mine != nil && theirs == nil:
		return "refused: the partner compiles to no document, and this spelling to one"
	default:
		return ""
	}
}

// errTreeTooDeep says an encoded document nests past maxSpellingDepth, which
// the oracle declines as DeclineTooDeep.
var errTreeTooDeep = errors.New("encoded document nests too deep")

// errCollides says two names of one object read alike once relocated, which
// the oracle declines as DeclineRelocationCollides.
var errCollides = errors.New("two names of one object relocate to one")

// comparableSides returns each document folded, encoded and relocated as a
// decoded tree, nil for no document, why the two cannot be compared, or the
// error that stopped one.
func comparableSides(mine, theirs *ir.Document, relocate func(string) string) (any, any, Decline, error) {
	a, why, err := comparableSide(mine, relocate)
	if why != "" || err != nil {
		return nil, nil, why, err
	}
	b, why, err := comparableSide(theirs, relocate)
	return a, b, why, err
}

// comparableSide is one side of comparableSides.
func comparableSide(doc *ir.Document, relocate func(string) string) (any, Decline, error) {
	if doc == nil {
		return nil, "", nil
	}
	own, more := folded(doc)
	encoded, err := encodeComparable(own, sortedArray, more)
	if err != nil {
		return nil, "", err
	}
	tree, err := relocatedTree(encoded, relocate)
	switch {
	case errors.Is(err, errCollides):
		return nil, DeclineRelocationCollides, nil
	case errors.Is(err, errTreeTooDeep):
		return nil, DeclineTooDeep, nil
	default:
		return tree, "", err
	}
}

// relocatedTree reads encoded into a tree of objects, arrays, strings, numbers
// as their text, booleans and nulls, every string, member names included,
// through relocate. Two names of one object it makes one are errCollides.
func relocatedTree(encoded []byte, relocate func(string) string) (any, error) {
	return readTree(jsontext.NewDecoder(bytes.NewReader(encoded)), relocate, 0)
}

// readTree reads the next value of dec as relocatedTree does, depth values
// deep, and refuses one deeper than maxSpellingDepth as errTreeTooDeep.
func readTree(dec *jsontext.Decoder, relocate func(string) string, depth int) (any, error) {
	if depth > maxSpellingDepth {
		return nil, errTreeTooDeep
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return nil, err
	}
	switch tok.Kind() {
	case '{':
		return readObject(dec, relocate, depth)
	case '[':
		array := []any{}
		for dec.PeekKind() != ']' {
			element, err := readTree(dec, relocate, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, element)
		}
		_, err := dec.ReadToken()
		return array, err
	case '"':
		return relocate(tok.String()), nil
	case '0':
		return number(tok.String()), nil
	case 't', 'f':
		return tok.Bool(), nil
	default:
		return nil, nil
	}
}

// readObject reads the members of the object whose start readTree read, at
// depth, each name relocated.
func readObject(dec *jsontext.Decoder, relocate func(string) string, depth int) (any, error) {
	object := map[string]any{}
	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}
		key := relocate(name.String())
		if _, taken := object[key]; taken {
			return nil, fmt.Errorf("%w: %q", errCollides, key)
		}
		if object[key], err = readTree(dec, relocate, depth+1); err != nil {
			return nil, err
		}
	}
	_, err := dec.ReadToken()
	return object, err
}

// diagnosed is each side's diagnostics as diagnosticSet writes them,
// relocated, with the reports the spelling states taken out.
type diagnosed struct{ mine, theirs []string }

// comparableDiagnostics returns the two sides' diagnostics to compare, and
// each report eq states that its side does not hold.
func comparableDiagnostics(mine, theirs []ir.Diagnostic, eq *Equal) (diagnosed, []string) {
	mine, lostMine := takeOut(mine, eq.Stated)
	theirs, lostTheirs := takeOut(theirs, eq.Replaced)
	return diagnosed{mine: relocatedSet(mine, eq.Relocate), theirs: relocatedSet(theirs, eq.Relocate)},
		append(lostMine, lostTheirs...)
}

// takeOut returns diags less one report per stated, and each stated report
// diags does not hold, as "stated:<code>".
func takeOut(diags []ir.Diagnostic, stated []Stated) ([]ir.Diagnostic, []string) {
	left := slices.Clone(diags)
	var lost []string
	for _, st := range stated {
		i := slices.IndexFunc(left, func(d ir.Diagnostic) bool {
			return d.Severity == st.Severity && d.Code == st.Code && d.Provenance.Pointer == st.Pointer
		})
		if i < 0 {
			lost = append(lost, "stated:"+st.Code)
			continue
		}
		left = slices.Delete(left, i, i+1)
	}
	return left, lost
}

// relocatedSet is diagnosticSet of diags, each pointer and node relocated.
func relocatedSet(diags []ir.Diagnostic, relocate func(string) string) []string {
	moved := make([]ir.Diagnostic, len(diags))
	for i, d := range diags {
		d.Provenance.Pointer = jsontext.Pointer(relocate(string(d.Provenance.Pointer)))
		d.Provenance.Node = relocate(d.Provenance.Node)
		moved[i] = d
	}
	return diagnosticSet(moved)
}

// differsIn is the signature of how two spellings differ, empty when they do
// not: the places their documents differ (signature), each code whose reports
// differ as "diagnostics:<code>", and each stated report a side lacks, sorted
// and joined by ",".
func differsIn(a, b any, diags diagnosed, lost []string) string {
	places := map[string]bool{}
	signature(a, b, "", places, 0)
	for _, code := range differingCodes(diags) {
		places["diagnostics:"+code] = true
	}
	for _, l := range lost {
		places[l] = true
	}
	return strings.Join(slices.Sorted(maps.Keys(places)), ",")
}

// signature records in places each place a and b differ, by the member names
// leading to it from at: an array index is left out, and so is a member whose
// value names itself as its id.
func signature(a, b any, at string, places map[string]bool, depth int) {
	if depth > maxSpellingDepth {
		places[at+"…"] = true
		return
	}
	ao, aIsObject := a.(map[string]any)
	bo, bIsObject := b.(map[string]any)
	if aIsObject && bIsObject {
		for _, name := range slices.Sorted(maps.Keys(union(ao, bo))) {
			signature(ao[name], bo[name], memberPlace(at, name, ao[name], bo[name]), places, depth+1)
		}
		return
	}
	aa, aIsArray := a.([]any)
	ba, bIsArray := b.([]any)
	if aIsArray && bIsArray && len(aa) == len(ba) {
		for i := range aa {
			signature(aa[i], ba[i], at, places, depth+1)
		}
		return
	}
	if !gocmp.Equal(a, b) {
		places[cmp.Or(at, "document")] = true
	}
}

// union is a map of a's and b's keys, b's value where both hold one.
func union[V any](a, b map[string]V) map[string]V {
	out := make(map[string]V, len(a)+len(b))
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
}

// memberPlace is the place of member name beneath at, at itself where either
// side's value holds name as its id.
func memberPlace(at, name string, a, b any) string {
	for _, v := range []any{a, b} {
		if o, ok := v.(map[string]any); ok && o["id"] == name {
			return at
		}
	}
	if at == "" {
		return name
	}
	return at + "." + name
}

// differingCodes returns, sorted, each code whose reports differ between the
// two sides.
func differingCodes(d diagnosed) []string {
	// A key is diagnosticKey's: the severity, then the code, each ended by a NUL.
	byCode := func(set []string) map[string][]string {
		out := map[string][]string{}
		for _, key := range set {
			_, rest, _ := strings.Cut(key, "\x00")
			code, _, _ := strings.Cut(rest, "\x00")
			out[code] = append(out[code], key)
		}
		return out
	}
	mine, theirs := byCode(d.mine), byCode(d.theirs)
	var codes []string
	for _, code := range slices.Sorted(maps.Keys(union(mine, theirs))) {
		if !slices.Equal(mine[code], theirs[code]) {
			codes = append(codes, code)
		}
	}
	return codes
}

// spellingDetail is a finding's Detail: what differs, the rule, the two
// documents' and diagnostics' difference, and both sources.
func spellingDetail(what, rule string, a, b any, diags diagnosed, mine, partner Document) string {
	var out strings.Builder
	out.WriteString(what + "\n" + rule + "\n")
	out.WriteString(gocmp.Diff(indented(b), indented(a)))
	out.WriteString(gocmp.Diff(diags.theirs, diags.mine))
	for _, d := range []Document{mine, partner} {
		for _, src := range d.Sources {
			out.WriteString("--- " + src.Path + "\n" + string(src.Data) + "\n")
		}
	}
	return out.String()
}

// indented is a decoded tree as indented lines, members sorted, or a line
// saying why it does not encode, as one a relocation made invalid UTF-8.
func indented(tree any) []string {
	b, err := json.Marshal(tree, json.Deterministic(true), jsontext.WithIndent("  "),
		json.WithMarshalers(json.MarshalToFunc(func(enc *jsontext.Encoder, n number) error {
			return enc.WriteValue(jsontext.Value(n))
		})))
	if err != nil {
		return []string{"does not encode: " + err.Error()}
	}
	return lines(b)
}
