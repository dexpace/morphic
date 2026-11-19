// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/ir/irverify"
	"github.com/dexpace/morphic/pass"
)

// registryFields returns the fields of ir.Document that file nodes under a key
// of a class of ID, read off Document's own shape, a map keyed by a named
// string type, and held to ir.KeyedClasses, so a registry Document gains is
// swept the moment it exists.
func registryFields(t *testing.T) []reflect.StructField {
	t.Helper()
	doc := reflect.TypeFor[ir.Document]()
	var fields []reflect.StructField
	classes := map[reflect.Type]bool{}
	for f := range doc.Fields() {
		if f.Type.Kind() == reflect.Map && f.Type.Key().Kind() == reflect.String && f.Type.Key().PkgPath() != "" {
			fields = append(fields, f)
			classes[f.Type.Key()] = true
		}
	}
	require.Equal(t, ir.KeyedClasses(), classes)
	return fields
}

// registryNode is the ID the registry node v declares, and the group the sweep
// moves one node of: a type's kind, or a scheme's kind and its ID's space.
func registryNode(v reflect.Value) (id, group string) {
	if td, ok := reflect.TypeAssert[ir.TypeDef](v); ok {
		return string(td.Common().ID), string(td.Kind())
	}
	id = v.FieldByName("ID").String()
	if k := v.FieldByName("Kind"); k.IsValid() {
		kind, _, _ := strings.Cut(id, ir.IDSeparator)
		space, _ := ir.IDSpace(kind, id)
		return id, k.String() + " in " + space
	}
	return id, ""
}

// unfiled returns a copy of doc without the node the registry at field files
// under key, sharing the rest and writing none of it.
func unfiled(doc *ir.Document, field reflect.StructField, key string) *ir.Document {
	out := *doc
	m := reflect.ValueOf(doc).Elem().FieldByIndex(field.Index)
	kept := reflect.MakeMapWithSize(m.Type(), m.Len())
	for entries := m.MapRange(); entries.Next(); {
		if entries.Key().String() != key {
			kept.SetMapIndex(entries.Key(), entries.Value())
		}
	}
	reflect.ValueOf(&out).Elem().FieldByIndex(field.Index).Set(kept)
	return &out
}

// refiledUnder returns a copy of doc with the node the registry at field files
// under from filed under to instead, sharing the rest and writing none of it.
func refiledUnder(doc *ir.Document, field reflect.StructField, from, to string) *ir.Document {
	out := unfiled(doc, field, from)
	m := reflect.ValueOf(doc).Elem().FieldByIndex(field.Index)
	key := func(s string) reflect.Value { return reflect.ValueOf(s).Convert(m.Type().Key()) }
	reflect.ValueOf(out).Elem().FieldByIndex(field.Index).SetMapIndex(key(to), m.MapIndex(key(from)))
	return out
}

// keysOtherThan returns keys for a node whose ID is id, none of them id: one in
// its space, one in each other space the document declares for the kind or ir
// reserves, one in a space nothing declares, one of another kind, two
// malformed and the empty key.
func keysOtherThan(doc *ir.Document, id string) []string {
	kind, rest, _ := strings.Cut(id, ir.IDSeparator)
	space, path, _ := strings.Cut(rest, ir.IDSeparator)
	join := func(parts ...string) string { return strings.Join(parts, ir.IDSeparator) }
	keys := []string{join(kind, space, path+"Refiled"), join(kind, "undeclared", path), "garbage", join(kind, "", path), ""}
	for _, other := range append(slices.Clone(doc.IDSpaces[kind]), ir.IDSpacePrim, ir.IDSpaceUnresolved) {
		if other != space {
			keys = append(keys, join(kind, other, path))
		}
	}
	other := ir.IDKinds()[0]
	if other == kind {
		other = ir.IDKinds()[1]
	}
	return append(keys, join(other, space, path))
}

// keyedDocument holds, unreferenced and drawing nothing from either checker,
// what no golden holds unreferenced: a scheme of each kind ir declares, the
// unresolved kind in its own space and the rest in a format's, a primitive, an
// enum with members, an external type, a channel, a message and a server.
func keyedDocument(t *testing.T) *ir.Document {
	t.Helper()
	p, err := decls.Load(reflect.TypeFor[ir.AuthKind]().PkgPath())
	require.NoError(t, err)
	kinds, err := p.StringConsts("AuthKind")
	require.NoError(t, err)
	at := func(path string) ir.Provenance { return ir.Provenance{Pointer: jsontext.Pointer("/" + path)} }
	pid := &ir.External{Identity: "erlang:pid"}
	pid.ID, pid.Name, pid.Provenance = "t/x/Pid", named("pid"), at("Pid")
	doc := &ir.Document{
		IRVersion: ir.IRVersion,
		Sources: []ir.SourceInfo{{
			Format: "openapi@3.1", Path: "api.yaml", Hash: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		}},
		Services: []ir.Service{{ID: "s/x/S", Name: named("svc")}},
		Types: ir.TypeRegistry{
			"t/prim/int32": &ir.Primitive{ID: "t/prim/int32", Prim: ir.PrimInt32, Provenance: ir.Provenance{Source: ir.NoSource}},
			"t/x/Color": &ir.Enum{ID: "t/x/Color", Name: named("color"), Provenance: at("Color"), ValueType: ir.PrimString,
				Members: []ir.EnumMember{{ID: "e/x/Color/red", Name: named("red"), Value: ir.Value{Kind: ir.ValueString, Str: "red"}}}},
			"t/x/Pid": pid,
		},
		Auth:     map[ir.AuthID]ir.AuthScheme{},
		Channels: map[ir.ChannelID]ir.Channel{"chan/x/c": {ID: "chan/x/c", Name: named("c")}},
		Messages: map[ir.MessageID]ir.Message{"msg/x/m": {ID: "msg/x/m", Name: named("m")}},
		Servers:  map[ir.ServerID]ir.Server{"server/x/s": {ID: "server/x/s", Name: named("s"), URLTemplate: "https://s.example", Provenance: at("s")}},
	}
	for _, k := range kinds {
		id, from := ir.AuthID("auth/x/"+k.Value), at(k.Value)
		if ir.AuthKind(k.Value) == ir.AuthKindUnresolved {
			id, from = ir.AuthID("auth/"+ir.IDSpaceUnresolved+"/"+k.Value), ir.Provenance{Source: ir.NoSource}
		}
		doc.Auth[id] = ir.AuthScheme{ID: id, Name: named("token"), Kind: ir.AuthKind(k.Value), Provenance: from}
	}
	spaces, cut := irtest.SpacesUsed(doc)
	require.False(t, cut)
	doc.IDSpaces = spaces
	require.Empty(t, irverify.Verify(doc))
	require.Empty(t, validatedOnce(t, doc))
	return doc
}

// spelledDiagnostics spells each diagnostic as its code, node and message.
func spelledDiagnostics(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code+" @ "+d.Provenance.Node+": "+d.Message)
	}
	return out
}

// spelledViolations spells each violation as its code, path and message.
func spelledViolations(vs []irverify.Violation) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Code+" @ "+v.Path+": "+v.Message)
	}
	return out
}

// beyond returns what got holds beyond base, counting repeats.
func beyond(got, base []string) []string {
	left := map[string]int{}
	for _, s := range base {
		left[s]++
	}
	var out []string
	for _, s := range got {
		if left[s] > 0 {
			left[s]--
			continue
		}
		out = append(out, s)
	}
	return out
}

// TestValidate_ANodeFiledUnderAnotherKeyDrawsTheMismatchAlone re-files, in every
// IR golden under testdata, reachDocument and keyedDocument, one node of each
// group of each registry under keys other than its ID, and holds both checkers
// to docs/ir/identity.md: Verify adds the mismatch at the key, or for the empty
// key the empty-key code, and Validate adds nothing. A node a reference names
// or a finding quotes stays put: moving it would make its referrers dangle.
func TestValidate_ANodeFiledUnderAnotherKeyDrawsTheMismatchAlone(t *testing.T) {
	t.Parallel()
	fields := registryFields(t)
	swept := map[string]bool{}
	for _, target := range append(sweepDocuments(t), sweepDocument{name: "keyedDocument", doc: keyedDocument(t)}) {
		doc := target.doc
		encoded, err := json.Marshal(doc)
		require.NoError(t, err)
		verified, validated := spelledViolations(irverify.Verify(doc)), spelledDiagnostics(validatedOnce(t, doc))
		mentioned := strings.Join(slices.Concat(verified, validated), "\n")
		for _, field := range fields {
			m := reflect.ValueOf(doc).Elem().FieldByIndex(field.Index)
			keys := make([]string, 0, m.Len())
			for entries := m.MapRange(); entries.Next(); {
				keys = append(keys, entries.Key().String())
			}
			slices.Sort(keys)
			moved := map[string]bool{}
			for _, key := range keys {
				id, group := registryNode(m.MapIndex(reflect.ValueOf(key).Convert(m.Type().Key())))
				quoted, err := jsontext.AppendQuote(nil, id)
				require.NoError(t, err)
				if moved[group] || id != key || bytes.Count(encoded, quoted) != 2 || strings.Contains(mentioned, id) {
					continue
				}
				moved[group], swept[field.Name] = true, true
				for _, to := range keysOtherThan(doc, id) {
					refiled := refiledUnder(doc, field, key, to)
					holdsTheMismatchAlone(t, target.name+": "+id+" under "+strconv.Quote(to), field, to, verified, refiled)
					want := validated
					if to == "" {
						// Validate reads a document without what is filed under the
						// empty key, so the node's own findings go with it.
						want = spelledDiagnostics(validatedOnce(t, unfiled(doc, field, key)))
					}
					assert.Equal(t, want, spelledDiagnostics(validatedOnce(t, refiled)), "%s: %s under %q", target.name, id, to)
				}
			}
		}
	}
	for _, field := range fields {
		assert.True(t, swept[field.Name], "the sweep moved no node of %s", field.Name)
	}
}

// holdsTheMismatchAlone holds Verify over refiled to base and the one code its
// registry at field draws for the key to: the mismatch, or for an empty key
// the empty-key code.
func holdsTheMismatchAlone(t *testing.T, name string, field reflect.StructField, to string, base []string, refiled *ir.Document) {
	t.Helper()
	noun := ir.RefNoun(field.Type.Key())
	label, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	want := "ir/" + noun + "-id-mismatch @ " + label + "[" + to + "]: "
	if to == "" {
		want = "ir/empty-" + noun + "-id @ " + label + `[""]: `
	}
	got := spelledViolations(irverify.Verify(refiled))
	assert.Empty(t, beyond(base, got), "%s: a violation went", name)
	added := beyond(got, base)
	if assert.Len(t, added, 1, "%s: %v", name, added) {
		assert.True(t, strings.HasPrefix(added[0], want), "%s: %s", name, added[0])
	}
}

// TestValidate_AnEmptyReferenceResolvesToNoNodeUnderTheEmptyKey files a node
// of each shape a reader settles under the empty key, beside a document whose
// empty references reach the value and type cycle checks, and holds Validate
// to what it reports with no such node: an empty reference names nothing,
// whatever a registry files under the empty key.
func TestValidate_AnEmptyReferenceResolvesToNoNodeUnderTheEmptyKey(t *testing.T) {
	t.Parallel()
	str := ir.Value{Kind: ir.ValueString, Str: "v"}
	empties := func() *ir.Document {
		return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{
			"t/prim/int32": &ir.Primitive{ID: "t/prim/int32", Prim: ir.PrimInt32},
			"t/x/U": &ir.Model{ID: "t/x/U", Name: named("u"), Base: &ir.TypeRef{}, Properties: []ir.Property{
				{ID: "p/x/U/a", Name: named("a"), WireName: "a", Required: true, Type: ir.TypeRef{}, Default: &str},
			}},
			"t/x/V": &ir.Union{ID: "t/x/V", Name: named("v"), Variants: []ir.Variant{{Type: ir.TypeRef{}}}},
			"t/x/L": &ir.List{ID: "t/x/L", Elem: ir.TypeRef{}},
		}, Services: []ir.Service{{ID: "s/x/S", Groups: []ir.OperationGroup{{ID: "g/x/G", Operations: []ir.Operation{{
			ID: "op/x/o", Params: []ir.Parameter{{ID: "param/x/o/q", Name: named("q")}},
			Examples: []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{Params: map[ir.ParamID]ir.Value{"param/x/o/q": str}}}},
		}}}}}}}
	}
	want := validatedOnce(t, empties())
	for _, node := range []ir.TypeDef{
		&ir.Model{ID: "t/x/M", Name: named("m"), Properties: []ir.Property{
			{ID: "p/x/M/a", Name: named("a"), WireName: "a", Required: true, Type: ir.TypeRef{Target: "t/x/U"}},
		}},
		&ir.Union{ID: "t/x/M", Name: named("m"), Variants: []ir.Variant{{Type: ir.TypeRef{Target: "t/x/U"}}}},
		&ir.Enum{ID: "t/x/M", Name: named("m"), ValueType: ir.PrimInt32, Members: []ir.EnumMember{
			{ID: "e/x/M/one", Name: named("one"), Value: ir.Value{Kind: ir.ValueNumber, Num: "1"}},
		}},
		&ir.List{ID: "t/x/M", Elem: ir.TypeRef{Target: "t/x/U"}, Constraints: &ir.Constraints{MinItems: new(int64(1))}},
		&ir.Scalar{ID: "t/x/M", Name: named("m"), Base: &ir.TypeRef{Target: "t/prim/int32"}},
	} {
		t.Run(string(node.Kind()), func(t *testing.T) {
			t.Parallel()
			doc := empties()
			doc.Types[""] = node
			assert.Equal(t, spelledDiagnostics(want), spelledDiagnostics(validatedOnce(t, doc)))
		})
	}
}

// TestWithoutEmptyKeys_DropsTheEntryFromEveryRegistry holds withoutEmptyKeys to
// each registry ir.Document declares: filed under the empty key beside another
// key, the entry is gone from the edit Validate reads, with nothing refused,
// and kept in the document it was given, and a document filing nothing there
// is read as it is.
func TestWithoutEmptyKeys_DropsTheEntryFromEveryRegistry(t *testing.T) {
	t.Parallel()
	clean := &ir.Document{}
	same, refused := pass.WithoutEmptyKeys(clean)
	require.Same(t, clean, same)
	require.Empty(t, refused)
	for _, field := range registryFields(t) {
		t.Run(field.Name, func(t *testing.T) {
			t.Parallel()
			doc := &ir.Document{}
			m := reflect.MakeMap(field.Type)
			for _, key := range []string{"", "k"} {
				m.SetMapIndex(reflect.ValueOf(key).Convert(field.Type.Key()), reflect.Zero(field.Type.Elem()))
			}
			reflect.ValueOf(doc).Elem().FieldByIndex(field.Index).Set(m)

			edited, refused := pass.WithoutEmptyKeys(doc)
			assert.Empty(t, refused)
			read := reflect.ValueOf(edited).Elem().FieldByIndex(field.Index)
			require.Equal(t, 1, read.Len())
			assert.Equal(t, "k", read.MapKeys()[0].String())
			assert.Equal(t, 2, m.Len(), "the document Validate was given is not written")
		})
	}
}
