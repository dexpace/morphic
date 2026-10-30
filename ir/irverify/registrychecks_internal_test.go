package irverify

import (
	"fmt"
	"go/types"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// registryChecks are the checks that range over a document's registries and
// tables themselves rather than through the walk.
var registryChecks = []func(*ir.Document) []Violation{
	checkRegistryKeys, checkIDs, checkMemberIDs, checkPrimIDs, checkPrimKinds, checkAuthKinds,
	checkOAuthScopes, checkUnions, checkMaps, checkMapKeys, checkRequires, checkVersion, checkDocument,
}

// registryCheckName is a registryChecks entry's function name, without its
// package path.
func registryCheckName(check func(*ir.Document) []Violation) string {
	full := runtime.FuncForPC(reflect.ValueOf(check).Pointer()).Name()
	_, name, _ := strings.CutLast(full, ".")
	return name
}

// TestRegistryChecks_ListsEveryRegistryCheck holds registryChecks to every
// function shaped like one, Verify aside, so a check written later is held to
// the cost the test below pins.
func TestRegistryChecks_ListsEveryRegistryCheck(t *testing.T) {
	t.Parallel()
	listed := map[string]bool{}
	for _, check := range registryChecks {
		listed[registryCheckName(check)] = true
	}
	shape, err := verifyDecls(t).Named("Violation")
	require.NoError(t, err)
	var shaped []string
	for _, fn := range packageFuncs(t) {
		params, results := fn.sig.Params(), fn.sig.Results()
		if fn.name == "Verify" || fn.sig.Recv() != nil || params.Len() != 1 || results.Len() != 1 {
			continue
		}
		doc, isPtr := params.At(0).Type().(*types.Pointer)
		list, isSlice := results.At(0).Type().(*types.Slice)
		if isPtr && types.TypeString(doc.Elem(), nil) == irPath+".Document" &&
			isSlice && types.Identical(list.Elem(), shape) {
			shaped = append(shaped, fn.name)
		}
	}
	for _, name := range shaped {
		assert.True(t, listed[name], "%s is shaped like a registry check but is not in registryChecks", name)
	}
	assert.Len(t, listed, len(shaped), "registryChecks lists each check once, and only functions shaped like one")
}

// TestRegistryChecks_ACleanEntryCostsNothing holds every registry check to a
// cost that does not grow with the entries it ranges over when none has
// anything to report: no path is spelled for an entry before there is a
// violation to place there, and a model's or a flow's collection is read into
// one map the check empties for each rather than one of its own. Twice the
// entries must cost no more, past the index of 100 from which strconv builds
// the string an index spells. It is not run in parallel: AllocsPerRun measures
// the process.
func TestRegistryChecks_ACleanEntryCostsNothing(t *testing.T) {
	few, many := cleanRegistry(100), cleanRegistry(200)
	checks := map[string]func(*ir.Document) []Violation{
		"declarationViolations": func(doc *ir.Document) []Violation { return declarationViolations(doc.IDSpaces) },
	}
	for _, check := range registryChecks {
		checks[registryCheckName(check)] = check
	}
	for name, check := range checks {
		require.Empty(t, check(many), "%s: the fixture is clean, or what is measured is a report", name)
		assert.Equal(t,
			testing.AllocsPerRun(10, func() { _ = check(few) }),
			testing.AllocsPerRun(10, func() { _ = check(many) }),
			"%s: twice the entries cost more", name)
	}
}

// cleanRegistry returns a sound document holding n of each entry a registry
// check ranges over, beside a primitive of each of a few kinds: a model with
// more properties and Requires entries than a map holds unallocated, a union, a
// map, an enum whose members sit beneath a path longer than a string built on
// the stack holds, an OAuth scheme with more scopes than a map holds
// unallocated, a server, a declared namespace and a source.
func cleanRegistry(n int) *ir.Document {
	doc := &ir.Document{
		IRVersion: ir.IRVersion,
		Types:     ir.TypeRegistry{},
		Auth:      map[ir.AuthID]ir.AuthScheme{},
		Servers:   map[ir.ServerID]ir.Server{},
		IDSpaces:  map[string][]string{},
		Services:  []ir.Service{{ID: "s/x"}},
	}
	for _, kind := range []ir.PrimKind{ir.PrimString, ir.PrimInt32, ir.PrimBool} {
		doc.Types[ir.PrimTypeID(kind)] = &ir.Primitive{ID: ir.PrimTypeID(kind), Prim: kind}
	}
	for i := range n {
		suffix := fmt.Sprintf("%04d", i)
		model := &ir.Model{ID: ir.TypeID("t/x/M" + suffix)}
		for j := range 12 {
			model.Properties = append(model.Properties, ir.Property{WireName: fmt.Sprintf("own%d", j)})
			model.Requires = append(model.Requires, fmt.Sprintf("inherited%d", j))
		}
		enumPath := "components/schemas/Enumeration" + suffix
		enum := &ir.Enum{ID: ir.TypeID("t/x/" + enumPath), Members: []ir.EnumMember{
			{ID: ir.EnumMemberID("e/x/" + enumPath + "/a")}, {ID: ir.EnumMemberID("e/x/" + enumPath + "/b")},
		}}
		for _, td := range []ir.TypeDef{
			model, enum,
			&ir.Union{ID: ir.TypeID("t/x/U" + suffix), Variants: []ir.Variant{{}}},
			&ir.MapT{ID: ir.TypeID("t/x/Map" + suffix)},
		} {
			doc.Types[td.Common().ID] = td
		}
		flow := ir.OAuthFlow{Kind: "client_credentials"}
		for j := range 12 {
			flow.Scopes = append(flow.Scopes, ir.OAuthScope{Name: fmt.Sprintf("scope%d", j)})
		}
		scheme := ir.AuthID("auth/x/S" + suffix)
		doc.Auth[scheme] = ir.AuthScheme{ID: scheme, Kind: ir.AuthKindOAuth2, Flows: []ir.OAuthFlow{flow}}
		server := ir.ServerID("server/x/S" + suffix)
		doc.Servers[server] = ir.Server{ID: server}
		doc.IDSpaces[ir.IDKindType] = append(doc.IDSpaces[ir.IDKindType], "s"+suffix)
		doc.Sources = append(doc.Sources, ir.SourceInfo{Format: "openapi@3.1", Hash: strings.Repeat("0", sha256HexLen)})
	}
	return doc
}
