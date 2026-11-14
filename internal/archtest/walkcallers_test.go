// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package archtest_test

import (
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/decls"
)

// walkLimits are the bounds of ir's walks. An exported function of ir naming
// one sets the bound a walk stops at, and so is a walk: a caller that ignores
// its stop reads part of a document as whole.
var walkLimits = []string{"MaxWalkValues", "MaxWalkSteps"}

// walkCaller is how one production function answers for a walk that stopped.
type walkCaller struct {
	// answer says who reports the stop, or why none need.
	answer string
	// readers are the functions, keyed as the table is, that ask a collector
	// of ir's what the caller's walk collected, each answering for the stop as
	// answer says.
	readers []string
	// optional marks a function that may no longer name a walk because its
	// answer is read from another, so the table need not find it.
	optional bool
}

// validateReports is the answer of every function whose walk Validate's own
// run reports for.
const validateReports = "the Validate run reports a stopped walk once: ir/walk-truncated"

// walkCallers is every production function outside ir that names one of
// walkNames, keyed "<package from the module root>.<function>", with the
// answer it gives. ir owns the walks and answers for itself.
var walkCallers = map[string]walkCaller{
	"ir/irverify.Verify": {
		answer: "one ir/walk-truncated at the root; a reader claims no absence after it",
		readers: []string{"ir/irverify.checkReferentialIntegrity", "ir/irverify.checkEmptyRefs",
			"ir/irverify.list", "ir/irverify.pathsAt"},
	},
	"ir/irverify.VerifyFindings": {answer: "one ir/walk-truncated at the root, through the walk Verify takes"},

	"pass.Validate": {
		answer: "checkDanglingRefs reports one ir/walk-truncated; a reader claims no absence after it",
		readers: []string{"pass.checkDanglingRefs", "pass.checkEmptyRefs", "pass.reportedHere",
			"pass.Visit", "pass.dangling"},
	},
	"pass.Run":                     {answer: "reads the stop, as before"},
	"pass.checkGroupWalkTruncated": {answer: "reads the stop through the facts' Partial, silent where the reference walk reported", optional: true},
	"pass.forEachPayload":          {answer: validateReports},
	"pass.checkIdempotency":        {answer: validateReports},
	"pass.checkResponseIndices":    {answer: validateReports},
	"pass.checkLinkTargets":        {answer: validateReports},
	"pass.checkMessageBindings":    {answer: validateReports},
	"pass.checkOneWay":             {answer: validateReports},
	"pass.checkDefaultResponses":   {answer: validateReports},
	"pass.checkParamBindings":      {answer: validateReports},
	"pass.checkParamReferences":    {answer: validateReports},
	"pass.groups":                  {answer: "validate, last in every run, reports for the name reader"},
	"pass.graphqlReachableTypes":   {answer: "reads the stop; its type walks are cut only where the run's document walk is"},
	"pass.appendTypeIDs":           {answer: "cut only where the run's document walk is"},

	"ir/irfacts.document":           {answer: "Index.Partial says the facts read from operations answer for part"},
	"ir/irfacts.callbackOperations": {answer: "Index.Partial says the facts read from operations answer for part"},
	"ir/irfacts.seedGroups":         {answer: "Index.Partial says the facts read from operations answer for part"},
	"ir/irfacts.Partial":            {answer: "returns the stop, for the callers above and for Validate's group check"},

	"ir/irtest.SpacesUsed":        {answer: "returns the stop beside the spaces", readers: []string{"ir/irtest.SpacesUsed"}},
	"ir/irtest.AllocatingVisits":  {answer: "lists a stopped walk as a path"},
	"internal/census.add":         {answer: "reads the stop"},
	"engine/report.keptAtOrBelow": {answer: "reads the stop"},

	"compilers/openapi/internal/lowering.UnreadExtensions": {
		answer: "its visitor prunes at every Unmodeled, so it stops only where a visitor shown everything does, and validate reports",
	},
}

// walkNames returns ir's walks, read off ir: its exported functions that name
// one of walkLimits.
func walkNames(ir *decls.Package) []string {
	var names []string
	for _, limit := range walkLimits {
		for _, fn := range ir.FuncsUsing(ir.Types.Scope().Lookup(limit)) {
			if f, ok := ir.Types.Scope().Lookup(fn).(*types.Func); ok && f.Exported() {
				names = append(names, fn)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// collectorQueries returns, as "Type.Method", each exported method of an
// exported ir type that collects as a walk's visitor, bar Visit and Cycle: the
// questions a reader asks of what a walk collected.
func collectorQueries(ir *decls.Package) []string {
	visitor, ok := ir.Types.Scope().Lookup("Visitor").Type().Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	var queries []string
	for tn := range ir.Objects[*types.TypeName]() {
		named, isNamed := tn.Type().(*types.Named)
		if !tn.Exported() || !isNamed || !types.Implements(types.NewPointer(named), visitor) {
			continue
		}
		for m := range named.Methods() {
			if m.Exported() && m.Name() != "Visit" && m.Name() != "Cycle" {
				queries = append(queries, tn.Name()+"."+m.Name())
			}
		}
	}
	slices.Sort(queries)
	return queries
}

// walkUses returns, for one package p outside ir, the functions naming each of
// names in p's own copy of ir, keyed as the table is: a name with no dot is a
// package-level object, and "Type.Method" a method.
func walkUses(p *decls.Package, names []string) map[string][]string {
	rel := strings.TrimPrefix(p.Types.Path(), module+"/")
	// Only a direct import names a walk, and only its own copy of ir holds the
	// objects: one reached through another package may be a stub.
	i := slices.IndexFunc(p.Types.Imports(), func(imp *types.Package) bool { return imp.Path() == module+"/ir" })
	if rel == "ir" || i < 0 {
		return nil
	}
	scope := p.Types.Imports()[i].Scope()
	uses := map[string][]string{}
	for _, name := range names {
		typ, method, isMethod := strings.Cut(name, ".")
		obj := scope.Lookup(typ)
		if isMethod && obj != nil {
			obj, _, _ = types.LookupFieldOrMethod(types.NewPointer(obj.Type()), true, obj.Pkg(), method)
		}
		if obj == nil {
			continue
		}
		for _, fn := range p.FuncsUsing(obj) {
			uses[name] = append(uses[name], rel+"."+fn)
		}
	}
	return uses
}

// walkCallerFindings returns, sorted, a line for each function of pkgs that
// names one of walks and has no entry in table, each entry no such function
// matches unless it is optional, each function asking one of queries that no
// entry lists among its readers, and each listed reader asking none.
func walkCallerFindings(pkgs []*decls.Package, walks, queries []string, table map[string]walkCaller) []string {
	var findings []string
	found, read := map[string]bool{}, map[string]bool{}
	for _, p := range pkgs {
		for name, fns := range walkUses(p, walks) {
			for _, key := range fns {
				found[key] = true
				if _, listed := table[key]; !listed {
					findings = append(findings, fmt.Sprintf("%s names ir.%s and has no answer in walkCallers for a walk that stopped", key, name))
				}
			}
		}
		for name, fns := range walkUses(p, queries) {
			for _, key := range fns {
				read[key] = true
				if !listsReader(table, key) {
					findings = append(findings, fmt.Sprintf("%s asks ir.%s of what a walk collected and is no caller's reader in walkCallers", key, name))
				}
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(table)) {
		if !found[key] && !table[key].optional {
			findings = append(findings, fmt.Sprintf("walkCallers lists %s, which names no walk", key))
		}
		for _, reader := range table[key].readers {
			if !read[reader] {
				findings = append(findings, fmt.Sprintf("walkCallers lists %s as a reader for %s, which asks no collector", reader, key))
			}
		}
	}
	slices.Sort(findings)
	return slices.Compact(findings)
}

// listsReader reports whether an entry of table lists key among its readers.
func listsReader(table map[string]walkCaller, key string) bool {
	for _, c := range table {
		if slices.Contains(c.readers, key) {
			return true
		}
	}
	return false
}

// irDecls returns ir's own load among pkgs.
func irDecls(t *testing.T, pkgs []*decls.Package) *decls.Package {
	t.Helper()
	i := slices.IndexFunc(pkgs, func(p *decls.Package) bool { return p.Types.Path() == module+"/ir" })
	require.GreaterOrEqual(t, i, 0, "the load holds ir")
	return pkgs[i]
}

// TestWalks_EveryCallerAnswersForAStop holds every production function that
// reads a document through one of ir's bounded walks to an answer for a walk
// that stopped, and every function asking an ir collector what a walk
// collected to a caller whose answer covers it, read off the code and not off
// a list kept beside it: one added without an entry, or one gone and left in
// the table, fails here. It cannot see a reader of a collector a package keeps
// itself, such as a check's own maps, nor one handed a stop as a bool.
func TestWalks_EveryCallerAnswersForAStop(t *testing.T) {
	t.Parallel()
	pkgs, err := productionLoad()
	require.NoError(t, err)
	ir := irDecls(t, pkgs)
	walks, queries := walkNames(ir), collectorQueries(ir)
	require.Equal(t, []string{"ForEachGroup", "ForEachOperation", "TypeEdges", "WalkValues", "WalkValuesContext"}, walks,
		"the walks ir offers; a new one is held here once its callers are")
	require.NotEmpty(t, queries, "ir's collectors answer questions")

	for _, finding := range walkCallerFindings(pkgs, walks, queries, walkCallers) {
		t.Error(finding)
	}
	for key, c := range walkCallers {
		assert.NotEmpty(t, c.answer, "%s gives no answer", key)
	}
}

// TestWalkCallerFindings_ReportsWhatTheTableDoesNotHold plants the failures the
// test above holds: a function added to pass that names a walk, whose result
// it drops, one asking a collector that no entry lists as a reader, and a
// table entry, and a reader, that no function matches.
func TestWalkCallerFindings_ReportsWhatTheTableDoesNotHold(t *testing.T) {
	// Not in parallel: go/types can race when two checks share an import
	// (golang/go#81122).
	base, err := decls.Load(module + "/pass")
	require.NoError(t, err)
	irPkg, err := decls.Load(module + "/ir")
	require.NoError(t, err)
	planted := checkPlanted(t, base, "zz_planted_walk.go", `package pass

import "github.com/dexpace/morphic/ir"

func plantedWalk(doc *ir.Document) { ir.WalkValues(doc, "") }

func plantedReader(refs *ir.IDReferences) bool { return refs.Declares(nil, "") }
`)

	table := map[string]walkCaller{"pass.ghost": {answer: "none", readers: []string{"pass.ghostReader"}}}
	for key, c := range walkCallers {
		if strings.HasPrefix(key, "pass.") {
			table[key] = c
		}
	}

	got := walkCallerFindings([]*decls.Package{planted}, walkNames(irPkg), collectorQueries(irPkg), table)

	assert.Equal(t, []string{
		"pass.plantedReader asks ir.IDReferences.Declares of what a walk collected and is no caller's reader in walkCallers",
		"pass.plantedWalk names ir.WalkValues and has no answer in walkCallers for a walk that stopped",
		"walkCallers lists pass.ghost, which names no walk",
		"walkCallers lists pass.ghostReader as a reader for pass.ghost, which asks no collector",
	}, got)
}
