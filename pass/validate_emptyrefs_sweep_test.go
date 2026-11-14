// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/ir/irverify"
)

// sitesPerPosition bounds how many IDs of one position the sweep blanks, so it
// reads every position the documents hold without blanking each of thousands.
const sitesPerPosition = 3

// maxSiteDepth bounds the reflection walk that finds the sites.
const maxSiteDepth = 64

// blankSite is one ID a document holds at a reference position: the position's
// name, the class of ID, whether docs/ir/identity.md#ids-as-references gives
// it an empty form, and how to blank it and put it back.
type blankSite struct {
	position  string
	class     reflect.Type
	emptyForm bool
	blank     func() (restore func())
}

// siteCollector finds the refSites of a document by reflection, apart from
// ir's own table, so a wrong rule in that table disagrees with it.
type siteCollector struct {
	t     *testing.T
	sites []blankSite
}

// isIDClass reports whether t is one of ir's classes of ID.
func isIDClass(t reflect.Type) bool {
	return t.Kind() == reflect.String && t.PkgPath() == reflect.TypeFor[ir.TypeID]().PkgPath() &&
		strings.HasSuffix(t.Name(), "ID")
}

// value finds the sites below v; commit writes v back where it was copied from.
func (c *siteCollector) value(v reflect.Value, commit func(), depth int) {
	require.Less(c.t, depth, maxSiteDepth, "the document nests deeper than the sweep reads")
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			c.value(v.Elem(), commit, depth+1)
		}
	case reflect.Struct:
		if !v.CanSet() {
			return
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				c.field(v, i, commit, depth)
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			c.value(v.Index(i), commit, depth+1)
		}
	case reflect.Map:
		c.entries(v, commit, depth)
	default:
	}
}

// field finds the sites field i of the struct owner holds.
func (c *siteCollector) field(owner reflect.Value, i int, commit func(), depth int) {
	f, ft := owner.Field(i), owner.Type().Field(i).Type
	position := owner.Type().Name() + "." + owner.Type().Field(i).Name
	switch {
	case isIDClass(ft):
		c.scalar(owner, position, f, commit)
	case ft.Kind() == reflect.Pointer && isIDClass(ft.Elem()):
		if !f.IsNil() {
			c.scalar(owner, position, f.Elem(), commit)
		}
	case ft.Kind() == reflect.Slice && isIDClass(ft.Elem()):
		for j := range f.Len() {
			c.scalar(owner, position, f.Index(j), commit)
		}
	case ft.Kind() == reflect.Map && isIDClass(ft.Elem()):
		for _, k := range sortedKeys(f) {
			c.mapValue(position, f, k, commit)
		}
	case ft.Kind() == reflect.Map && isIDClass(ft.Key()) && owner.Type() != reflect.TypeFor[ir.Document]():
		for _, k := range sortedKeys(f) {
			c.mapKey(position, f, k, commit)
		}
		c.value(f, commit, depth+1)
	default:
		c.value(f, commit, depth+1)
	}
}

// entries finds the sites below each value of m, a map's struct value through a
// copy written back by its commit.
func (c *siteCollector) entries(m reflect.Value, commit func(), depth int) {
	for _, k := range sortedKeys(m) {
		val := m.MapIndex(k)
		if val.Kind() != reflect.Struct {
			c.value(val, commit, depth+1)
			continue
		}
		held := reflect.New(val.Type()).Elem()
		held.Set(val)
		c.value(held, func() { m.SetMapIndex(k, held); commit() }, depth+1)
	}
}

// sortedKeys returns m's keys in the order their spellings sort, so the sweep
// blanks the same sites on every run.
func sortedKeys(m reflect.Value) []reflect.Value {
	keys := m.MapKeys()
	slices.SortFunc(keys, func(a, b reflect.Value) int {
		return strings.Compare(fmt.Sprint(a.Interface()), fmt.Sprint(b.Interface()))
	})
	return keys
}

// scalar records the ID v, held by owner at position, unless the position is
// another check's: an entity's own ID and a TypeRef's target.
func (c *siteCollector) scalar(owner reflect.Value, position string, v reflect.Value, commit func()) {
	if v.String() == "" || strings.HasSuffix(position, ".ID") || position == "TypeRef.Target" {
		return
	}
	c.sites = append(c.sites, blankSite{position: position, class: v.Type(), emptyForm: hasEmptyForm(owner, position),
		blank: func() func() {
			was := v.String()
			v.SetString("")
			commit()
			return func() { v.SetString(was); commit() }
		}})
}

// mapValue records the ID map m holds under key k.
func (c *siteCollector) mapValue(position string, m, k reflect.Value, commit func()) {
	was := m.MapIndex(k)
	if was.String() == "" {
		return
	}
	c.sites = append(c.sites, blankSite{position: position, class: was.Type(), blank: func() func() {
		m.SetMapIndex(k, reflect.Zero(was.Type()))
		commit()
		return func() { m.SetMapIndex(k, was); commit() }
	}})
}

// mapKey records the ID map m holds as the key k.
func (c *siteCollector) mapKey(position string, m, k reflect.Value, commit func()) {
	if k.String() == "" {
		return
	}
	empty := reflect.Zero(k.Type())
	c.sites = append(c.sites, blankSite{position: position, class: k.Type(), blank: func() func() {
		val := m.MapIndex(k)
		m.SetMapIndex(k, reflect.Value{})
		m.SetMapIndex(empty, val)
		commit()
		return func() { m.SetMapIndex(empty, reflect.Value{}); m.SetMapIndex(k, val); commit() }
	}})
}

// hasEmptyForm reports whether docs/ir/identity.md#ids-as-references gives the
// position an empty form of its own, read off owner for the tag property.
func hasEmptyForm(owner reflect.Value, position string) bool {
	switch {
	case strings.HasPrefix(position, "EntityRef."), position == "Discriminator.Default",
		position == "Idempotency.TokenParam":
		return true
	case position == "Discriminator.Property":
		return owner.FieldByName("PropertyName").String() != "" || !owner.FieldByName("Index").IsNil()
	default:
		return false
	}
}

// emptyRefFindings returns the ir/empty-<noun>-ref findings of diags, each
// spelled with where it is, and the other errors, each with its message too.
func emptyRefFindings(diags []ir.Diagnostic) (empty, other []string) {
	for _, d := range diags {
		finding := d.Code + " @ " + d.Provenance.Node
		switch {
		case strings.HasPrefix(d.Code, "ir/empty-") && strings.HasSuffix(d.Code, "-ref"):
			empty = append(empty, finding)
		case d.Severity == ir.SeverityError:
			other = append(other, finding+": "+d.Message)
		}
	}
	return empty, other
}

// verifiedEmptyRefs returns irverify's ir/empty-<noun>-ref findings on doc,
// spelled as emptyRefFindings spells pass's, in pass's order.
func verifiedEmptyRefs(doc *ir.Document) []string {
	var out []string
	for _, v := range irverify.Verify(doc) {
		if strings.HasPrefix(v.Code, "ir/empty-") && strings.HasSuffix(v.Code, "-ref") {
			out = append(out, v.Code+" @ "+v.Path)
		}
	}
	slices.Sort(out)
	return out
}

// sweepDocument is one document the sweep blanks the references of.
type sweepDocument struct {
	name string
	doc  *ir.Document
}

// sweepDocuments returns every IR golden under testdata, in path order, and
// reachDocument after them.
func sweepDocuments(t *testing.T) []sweepDocument {
	t.Helper()
	var out []sweepDocument
	err := filepath.WalkDir("../testdata", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".golden.json") {
			return err
		}
		doc, err := irtest.ReadGolden(path)
		if err != nil {
			return err
		}
		out = append(out, sweepDocument{name: path, doc: doc})
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, out, "the sweep reads the IR goldens under testdata")
	return append(out, sweepDocument{name: "reachDocument", doc: reachDocument()})
}

// reachDocument holds an ID at each position the sweep names in reached that
// no golden under testdata holds: an overload, a server list, a message
// binding with its reply, a channel's messages and servers, and a scenario's
// keys.
func reachDocument() *ir.Document {
	doc := validDoc()
	overloaded, replies := ir.OpID("op/x/a"), ir.ChannelID("chan/x/r")
	sends := ir.Operation{
		ID: "op/x/b", OverloadOf: &overloaded,
		Params: []ir.Parameter{{ID: "param/x/b/q", Name: ir.Naming{Source: "q"}}},
		Bindings: ir.OpBindings{Message: &ir.MessageBinding{
			Channel: "chan/x/c", Direction: ir.MsgDirectionSend, Messages: []ir.MessageID{"msg/x/m"},
			Reply: &ir.Reply{Channel: &replies, Messages: []ir.MessageID{"msg/x/m"}},
		}},
		Examples: []ir.Example{{Name: "ex", Input: &ir.ScenarioInput{
			Params: map[ir.ParamID]ir.Value{"param/x/b/q": {Kind: ir.ValueString, Str: "v"}},
		}}},
	}
	doc.Services = []ir.Service{{ID: "s/x/S", Servers: []ir.ServerID{"server/x/s"}, Groups: []ir.OperationGroup{{
		ID: "g/x/G", Operations: []ir.Operation{{ID: "op/x/a"}, sends},
	}}}}
	doc.Servers = map[ir.ServerID]ir.Server{"server/x/s": {ID: "server/x/s"}}
	doc.Channels = map[ir.ChannelID]ir.Channel{
		"chan/x/c": {ID: "chan/x/c", Messages: []ir.MessageID{"msg/x/m"}, Servers: []ir.ServerID{"server/x/s"}},
		"chan/x/r": {ID: "chan/x/r", Messages: []ir.MessageID{"msg/x/m"}},
	}
	doc.Messages = map[ir.MessageID]ir.Message{"msg/x/m": {ID: "msg/x/m"}}
	return doc
}

// reached are positions the sweep must blank, so it is seen to reach each
// check that reads one and each position whose rule it holds: those with no
// empty form and those with one.
var reached = []string{
	"HTTPParamBinding.Param", "InputPath.Param", "LinkParam.Param", "Discriminator.Mapping", "Content.Encoding",
	"MessageBinding.Messages", "Reply.Messages", "Channel.Servers", "ScenarioInput.Params",
	"OperationLink.Operation", "Operation.OverloadOf", "Service.Servers",
	"Discriminator.Default", "Idempotency.TokenParam",
}

// TestValidate_BlankingAReferenceDrawsOneFindingFromBothCheckers blanks, one at a
// time, the IDs the sweep's documents hold at reference positions, a few of each
// position. Where docs/ir/identity.md#ids-as-references gives the position no
// empty form, pass.Validate must add exactly one ir/empty-<noun>-ref of its class
// and no other error naming the empty ID, which each check reading one quotes;
// where it gives one, none. An error the blank leaves at another reference, such
// as a message a channel no longer carries, is that reference's. irverify must
// report the same empty references at the same paths: both read one table.
func TestValidate_BlankingAReferenceDrawsOneFindingFromBothCheckers(t *testing.T) {
	t.Parallel()
	blanked := map[string]int{}
	for _, d := range sweepDocuments(t) {
		baseEmpty, baseOther := emptyRefFindings(validated(t, d.doc))
		require.Empty(t, baseEmpty, "%s holds no empty reference before the sweep", d.name)
		c := siteCollector{t: t}
		c.value(reflect.ValueOf(d.doc), func() {}, 0)
		for _, s := range c.sites {
			if blanked[s.position] >= sitesPerPosition {
				continue
			}
			blanked[s.position]++
			assertBlankedFindings(t, d, s, baseOther)
		}
	}
	for _, position := range reached {
		assert.Contains(t, blanked, position, "no sweep document holds an ID at %s", position)
	}
}

// assertBlankedFindings blanks s in d and holds what each checker reports to
// the position's form; baseOther are the errors d drew before.
func assertBlankedFindings(t *testing.T, d sweepDocument, s blankSite, baseOther []string) {
	t.Helper()
	restore := s.blank()
	empty, other := emptyRefFindings(validated(t, d.doc))
	verified := verifiedEmptyRefs(d.doc)
	restore()

	where := d.name + ": " + s.position
	if s.emptyForm {
		assert.Empty(t, empty, "%s has an empty form", where)
	} else if assert.Len(t, empty, 1, "%s has no empty form", where) {
		assert.True(t, strings.HasPrefix(empty[0], "ir/empty-"+ir.RefNoun(s.class)+"-ref @ "), "%s drew %s", where, empty[0])
		for _, finding := range other {
			if !slices.Contains(baseOther, finding) {
				assert.NotContains(t, finding, `""`, "%s: an empty reference draws no second error", where)
			}
		}
	}
	slices.Sort(empty)
	assert.Equal(t, verified, empty, "%s: irverify and pass.Validate report one set of empty references", where)
}
