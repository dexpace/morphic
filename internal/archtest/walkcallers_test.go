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

// walkNames are the traversals ir offers that stop at a bound and report that
// they did: a caller that ignores the report reads part of a document as whole.
var walkNames = []string{"WalkValues", "WalkValuesContext", "ForEachGroup", "ForEachOperation", "TypeEdges"}

// walkCaller is how one production function answers for a walk that stopped.
type walkCaller struct {
	// answer says who reports the stop, or why none need.
	answer string
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
	"ir/irverify.Verify":         {answer: "one ir/walk-truncated at the root"},
	"ir/irverify.VerifyFindings": {answer: "one ir/walk-truncated at the root"},

	"pass.Validate":                {answer: "checkDanglingRefs reports one ir/walk-truncated"},
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

	"ir/irtest.SpacesUsed":        {answer: "returns the stop beside the spaces"},
	"ir/irtest.AllocatingVisits":  {answer: "lists a stopped walk as a path"},
	"internal/census.add":         {answer: "reads the stop"},
	"engine/report.keptAtOrBelow": {answer: "reads the stop"},

	"compilers/openapi/internal/lowering.UnreadExtensions": {
		answer: "its visitor prunes at every Unmodeled, so it stops only where a visitor shown everything does, and validate reports",
	},
}

// walkCallerFindings returns, sorted, a line for each function of pkgs outside
// ir that names a walk and has no entry in table, and for each entry of table
// no such function matches unless it is optional.
func walkCallerFindings(pkgs []*decls.Package, table map[string]walkCaller) []string {
	var findings []string
	found := map[string]bool{}
	for _, p := range pkgs {
		rel := strings.TrimPrefix(p.Types.Path(), module+"/")
		if rel == "ir" {
			continue
		}
		// Only a direct import names a walk, and only its own copy of ir
		// holds the objects: one reached through another package may be a stub.
		i := slices.IndexFunc(p.Types.Imports(), func(imp *types.Package) bool { return imp.Path() == module+"/ir" })
		if i < 0 {
			continue
		}
		irPkg := p.Types.Imports()[i]
		for _, name := range walkNames {
			walk := irPkg.Scope().Lookup(name)
			if walk == nil {
				findings = append(findings, fmt.Sprintf("ir declares no %s, so no caller of it is held", name))
				continue
			}
			for _, fn := range p.FuncsUsing(walk) {
				key := rel + "." + fn
				found[key] = true
				if _, listed := table[key]; !listed {
					findings = append(findings, fmt.Sprintf("%s names ir.%s and has no answer in walkCallers for a walk that stopped", key, name))
				}
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(table)) {
		if !found[key] && !table[key].optional {
			findings = append(findings, fmt.Sprintf("walkCallers lists %s, which names no walk", key))
		}
	}
	slices.Sort(findings)
	return slices.Compact(findings)
}

// TestWalks_EveryCallerAnswersForAStop holds every production function that
// reads a document through one of ir's bounded walks to an answer for a walk
// that stopped, read off the code and not off a list kept beside it: a caller
// added without one, or one gone and left in the table, fails here.
func TestWalks_EveryCallerAnswersForAStop(t *testing.T) {
	t.Parallel()
	pkgs, err := productionLoad()
	require.NoError(t, err)

	for _, finding := range walkCallerFindings(pkgs, walkCallers) {
		t.Error(finding)
	}
	for key, c := range walkCallers {
		assert.NotEmpty(t, c.answer, "%s gives no answer", key)
	}
}

// TestWalkCallerFindings_ReportsWhatTheTableDoesNotHold plants the two failures
// the test above holds: a function added to pass that names a walk, whose
// result it drops, and a table entry no function matches.
func TestWalkCallerFindings_ReportsWhatTheTableDoesNotHold(t *testing.T) {
	// Not in parallel: go/types can race when two checks share an import
	// (golang/go#81122).
	base, err := decls.Load(module + "/pass")
	require.NoError(t, err)
	planted := checkPlanted(t, base, "zz_planted_walk.go", `package pass

import "github.com/dexpace/morphic/ir"

func plantedWalk(doc *ir.Document) { ir.WalkValues(doc, "") }
`)

	table := map[string]walkCaller{"pass.ghost": {answer: "none"}}
	for key, c := range walkCallers {
		if strings.HasPrefix(key, "pass.") {
			table[key] = c
		}
	}

	got := walkCallerFindings([]*decls.Package{planted}, table)

	assert.Equal(t, []string{
		"pass.plantedWalk names ir.WalkValues and has no answer in walkCallers for a walk that stopped",
		"walkCallers lists pass.ghost, which names no walk",
	}, got)
}
