// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package canary_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

// canaries returns the test for each entry docs/parser-library.md records, by
// the entry's key. Each asserts what the library does today, defects
// included, so it goes red when a library change alters the behaviour.
func canaries() map[string]func(t *testing.T) {
	return map[string]func(t *testing.T){
		"schema-cycle-overflow":  schemaCycleOverflow,
		"resolution-unbounded":   resolutionUnbounded,
		"defs-order":             defsOrder,
		"mapping-unresolved":     mappingUnresolved,
		"reference-object-cycle": referenceObjectCycle,
		"root-defs":              rootDefs,
		"alias-cycle":            aliasCycle,
		"alias-expansion":        aliasExpansion,
		"nesting-cost":           nestingCost,
		"goroutine-per-node":     goroutinePerNode,
		"tagged-mapping-panic":   taggedMappingPanic,
		"recoverable-panic":      recoverablePanic,
		"anchored-entry-dropped": anchoredEntryDropped,
		"embedded-map-keys":      embeddedMapKeys,
		"requirement-scopes":     requirementScopes,
		"duplicate-key":          duplicateKey,
		"merge-keys":             mergeKeys,
		"non-scalar-key":         nonScalarKey,
		"scalar-resolution":      scalarResolution,
		"undecoded-scalar":       undecodedScalar,
		"unmodeled-keywords":     unmodeledKeywords,
		"null-schema-is-false":   nullSchemaIsFalse,
		"overlay-alias-clone":    overlayAliasClone,
		"trailing-empty-token":   trailingEmptyToken,
		"context-unread":         contextUnread,
	}
}

// TestLibraryBehaviour runs each canary as a subtest named for its entry's
// key, which is the name the entry gives as its pin.
func TestLibraryBehaviour(t *testing.T) {
	t.Parallel()
	tests := canaries()
	for _, key := range slices.Sorted(maps.Keys(tests)) {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			tests[key](t)
		})
	}
}

// location is where every document here claims to live, which the resolver
// reads relative references against and quotes in what it reports.
const location = "/canary/spec.yaml"

// head opens every document here: a valid OpenAPI 3.1 header with no paths.
const head = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n"

// section returns a document declaring each entry, written as one line of
// YAML, under components.<name>, in the order given.
func section(name string, entries ...string) string {
	var b strings.Builder
	b.WriteString(head + "components:\n  " + name + ":\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "    %s\n", e)
	}
	return b.String()
}

// schemas is section for components.schemas.
func schemas(entries ...string) string {
	return section("schemas", entries...)
}

// refTo returns the components entry name, written as a $ref to the entry
// target in the same section.
func refTo(sectionName, name, target string) string {
	return fmt.Sprintf("%s: {$ref: '#/components/%s/%s'}", name, sectionName, target)
}

// revisit returns the message a canary fails with: the entry whose record the
// library no longer meets, and the start of what the library did instead.
func revisit(t *testing.T, r result) string {
	t.Helper()
	key := strings.Split(t.Name(), "/")[1]
	seen := r.report
	if r.outcome != "" {
		seen = fmt.Sprintf("the child %s: %s\n%.1500s", r.outcome, r.report, r.output)
	}
	return fmt.Sprintf("the library no longer behaves as docs/parser-library.md#%s records: "+
		"re-read the entry and the code it names\n%s", key, seen)
}
