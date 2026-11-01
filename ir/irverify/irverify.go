// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"cmp"
	"reflect"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// Violation is one structural defect found in an ir.Document: an internal
// compiler bug, never a spec-author problem (that is ir.Diagnostic). Code is a
// stable, slash-namespaced string; Path locates the offending node.
type Violation struct {
	// Code is a stable, slash-namespaced identifier for the defect class.
	Code string
	// Message is a human-readable description of the specific defect. Text it
	// takes from the document is strconv.Quote'd, so it is valid UTF-8 whatever
	// the document holds (GitHub #400); Path is not yet (GitHub #739).
	Message string
	// Path locates the offending node within the document.
	Path string
}

// Verify runs every structural invariant check over doc and returns the
// violations, sorted by (Code, Path) for deterministic output. A structurally
// sound document yields nil.
//
// A check builds a path, or any other string, only for a violation, so a value
// with nothing wrong costs it no allocation but a step a map or a log it keeps
// may grow by, beyond what its readers make: the set ir.Inferred.IsCanonical
// makes of a long list, and the names a jsontext decoder keeps of a large
// object to refuse a duplicate.
func Verify(doc *ir.Document) []Violation {
	return verify(doc, ir.WalkValues)
}

// walkFunc is how verify walks the document: ir.WalkValues, unless a test is
// counting the walks.
type walkFunc func(root any, path string, visitors ...ir.Visitor) bool

// verify is Verify walking the document with walk.
func verify(doc *ir.Document, walk walkFunc) []Violation {
	if doc == nil {
		return []Violation{{Code: "ir/nil-document", Message: "document is nil"}}
	}

	vs := checkRegistryKeys(doc)
	vs = append(vs, checkIDs(doc)...)
	vs = append(vs, checkPrimIDs(doc)...)
	vs = append(vs, checkPrimKinds(doc)...)
	vs = append(vs, checkAuthKinds(doc)...)
	vs = append(vs, checkUnresolvedSchemes(doc)...)
	vs = append(vs, checkOAuthScopes(doc)...)
	vs = append(vs, checkUnions(doc)...)
	vs = append(vs, checkMaps(doc)...)
	vs = append(vs, checkMapKeys(doc)...)
	vs = append(vs, checkRequires(doc)...)
	vs = append(vs, checkVersion(doc)...)
	vs = append(vs, checkDocument(doc)...)
	vs = append(vs, runWalkChecks(doc, walk)...)

	// Stable: two violations can share a (Code, Path) — an embedded field
	// contributes no path segment, so two promoted fields of the same name would
	// collide — and a stable sort leaves those in the walk's deterministic order
	// rather than an unspecified one.
	slices.SortStableFunc(vs, func(a, b Violation) int {
		return cmp.Or(cmp.Compare(a.Code, b.Code), cmp.Compare(a.Path, b.Path))
	})
	return vs
}

// declarations is the identities a document's nodes declare, each with its
// path kept unspelled for pathsAt, plus whether the walk that read them was cut
// short.
//
// The walk every check shares reads them, and two checks need them once it is
// over: checkDuplicateIDs to hold each to being declared once, and
// checkIDSpaces to place each in a declared namespace. checkReferentialIntegrity
// reads only the truncation, since ir.IDReferences reads the identities a
// reference resolves against itself.
//
// The zero value says the document declares none and the walk ran to its end.
type declarations struct {
	collected *ir.IDDeclarations
	truncated bool
}

// list returns the declarations, in walk order.
func (d declarations) list() []ir.IDDeclaration {
	if d.collected == nil {
		return nil
	}
	return d.collected.List()
}

// pathsAt spells the path of each declaration at a position in at, keyed by
// that position, so a check spells only the paths it reports.
func (d declarations) pathsAt(at map[int]bool) map[int]string {
	if d.collected == nil {
		return nil
	}
	paths := make(map[int]string, len(at))
	for i, path := range d.collected.Paths() {
		if at[i] {
			paths[i] = path.String()
		}
	}
	return paths
}

// walkCheck is one check's part in the walk every check shares: visit is shown
// each value the walk reaches, as an ir.Visitor's Visit is, and report returns
// what the check found once the walk is over, given the declarations the same
// walk read. A nil visit is shown nothing, and a nil cycle is told of no cycle.
type walkCheck struct {
	visit  func(v reflect.Value, at ir.WalkPath) bool
	cycle  func(at, entered ir.WalkPath, ref reflect.Type)
	report func(decls declarations) []Violation
}

// Visit calls c.visit, so a walkCheck is an ir.Visitor.
func (c walkCheck) Visit(v reflect.Value, at ir.WalkPath) bool {
	return c.visit != nil && c.visit(v, at)
}

// Cycle calls c.cycle, so a walkCheck is an ir.CycleVisitor.
func (c walkCheck) Cycle(at, entered ir.WalkPath, ref reflect.Type) {
	if c.cycle != nil {
		c.cycle(at, entered, ref)
	}
}

// reporting is the report of a check whose visits find everything it reports:
// it hands back *vs as the walk left it.
func reporting(vs *[]Violation) func(declarations) []Violation {
	return func(declarations) []Violation { return *vs }
}

// walkChecks are the checks that reach their subject through the walk. Each is
// built afresh for the document it checks, since it gathers what it finds as the
// walk goes.
func walkChecks() []func(*ir.Document) walkCheck {
	return []func(*ir.Document) walkCheck{
		checkReferentialIntegrity,
		checkTypeRefs,
		checkDuplicateIDs,
		checkDeclaredIDs,
		checkIDSpaces,
		checkNaming,
		checkStringLists,
		checkRawPayloads,
		checkProvenance,
		checkIndices,
		checkBigVals,
		checkInferred,
		checkUTF8,
		checkValues,
		checkCycles,
		checkEmptyRefs,
		checkIdempotency,
		checkStreamingModes,
		checkWebhookRoutes,
	}
}

// runWalkChecks runs every walking check over doc in one walk, and folds the
// walk's truncation into one ir/walk-truncated violation.
//
// Truncation is a fact about the document, not about a check, so it is reported
// once. Not reporting it left the pruning walks silently under-checking a
// too-deep document (GitHub #55); with one walk there is one flag, and nothing a
// check can drop.
func runWalkChecks(doc *ir.Document, walk walkFunc) []Violation {
	vs, truncated := walkWith(doc, walk, walkChecks())
	if !truncated {
		return vs
	}
	return append(vs, Violation{
		Code:    "ir/walk-truncated",
		Message: "document nests deeper than the bounded verifier walk; part of it went unchecked",
		Path:    ir.DocumentPath,
	})
}

// walkWith builds each of checks for doc and walks doc once for all of them,
// reading the declarations in the same walk. It returns what they report, in
// the order given, and whether the walk was cut short.
func walkWith(doc *ir.Document, walk walkFunc, checks []func(*ir.Document) walkCheck) ([]Violation, bool) {
	ids, built, visitors := walkVisitors(doc, checks)
	decls := declarations{collected: ids, truncated: walk(doc, ir.DocumentPath, visitors...)}

	reports := make([][]Violation, len(built))
	for i, c := range built {
		reports[i] = c.report(decls)
	}
	return slices.Concat(reports...), decls.truncated
}

// walkVisitors builds each of checks for doc and returns them, the collector
// of the document's declarations, and the visitors one walk carries for all of
// them, the collector first.
func walkVisitors(doc *ir.Document, checks []func(*ir.Document) walkCheck) (*ir.IDDeclarations, []walkCheck, []ir.Visitor) {
	ids := &ir.IDDeclarations{}
	built := make([]walkCheck, len(checks))
	visitors := make([]ir.Visitor, 0, 1+len(checks))
	visitors = append(visitors, ids)
	for i, newCheck := range checks {
		built[i] = newCheck(doc)
		visitors = append(visitors, built[i])
	}
	return ids, built, visitors
}

// checkRegistryKeys asserts every entry of each flat, ID-keyed registry
// (Types, Channels, Messages, Auth, Servers) is keyed by its own node ID and
// that the key is non-empty (invariant #3). Each registry contributes
// symmetric empty-*-id and *-id-mismatch violations.
//
// A nil type definition is reported rather than dereferenced — Common() panics
// on one — which is what keeps Verify a report-only oracle that never crashes on
// a malformed document. The walk-based checks already tolerate nil entries.
func checkRegistryKeys(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		if ir.IsNilTypeDef(td) {
			vs = append(vs, Violation{
				Code:    "ir/nil-type",
				Message: "types registry has a nil type definition",
				Path:    typePath(id),
			})
			continue
		}
		vs = registryKey(vs, "type", "types", string(id), string(td.Common().ID))
	}
	for id, ch := range doc.Channels {
		vs = registryKey(vs, "channel", "channels", string(id), string(ch.ID))
	}
	for id, msg := range doc.Messages {
		vs = registryKey(vs, "message", "messages", string(id), string(msg.ID))
	}
	for id, scheme := range doc.Auth {
		vs = registryKey(vs, "auth", "auth", string(id), string(scheme.ID))
	}
	for id, server := range doc.Servers {
		vs = registryKey(vs, "server", "servers", string(id), string(server.ID))
	}
	return vs
}

// typePath spells where a check that ranges over the type registry reports the
// entry id. A check spells it only once it has a violation to place there.
func typePath(id ir.TypeID) string {
	return "types[" + string(id) + "]"
}

// registryKey checks one registry entry: key must be non-empty and equal the
// node's own ID. noun is the diagnostic-code singular ("type", "channel", …) and
// reg is the path/message registry label ("types", "channels", …).
func registryKey(vs []Violation, noun, reg, key, nodeID string) []Violation {
	if key == "" {
		return append(vs, Violation{
			Code:    "ir/empty-" + noun + "-id",
			Message: reg + " registry has an empty key",
			Path:    reg + `[""]`,
		})
	}
	if key != nodeID {
		return append(vs, Violation{
			Code:    "ir/" + noun + "-id-mismatch",
			Message: "registry key " + strconv.Quote(key) + " disagrees with node ID " + strconv.Quote(nodeID),
			Path:    reg + "[" + key + "]",
		})
	}
	return vs
}
