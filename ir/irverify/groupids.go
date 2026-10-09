package irverify

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// maxGroupDepth bounds the descent through nested operation groups, matching the
// bound pass.Validate walks them to.
const maxGroupDepth = 128

// checkGroupIDs holds every operation group's ID to the grammar its space
// implies, walking the group trees to maxGroupDepth. It is a walking check, so
// the cap reaches the caller as a truncation flag and is reported once as
// ir/walk-truncated.
//
// checkIDs' generic pointer agreement cannot apply here: a declared group's
// pointer is /tags/<i> while its ID is derived from the tag's name, so the ID
// must agree with the name and the pointer need only be present.
func checkGroupIDs(doc *ir.Document, _ declarations) ([]Violation, bool) {
	w := &groupIDWalk{doc: doc}
	for i, svc := range doc.Services {
		w.walk(svc.Groups, ir.DocumentPath+".Services["+strconv.Itoa(i)+"].Groups", 0)
	}
	return w.vs, w.truncated
}

// groupIDWalk carries one bounded descent through the group trees.
type groupIDWalk struct {
	doc       *ir.Document
	vs        []Violation
	truncated bool
}

// walk checks groups and, recursively, their children, recording when the depth
// bound cut the descent short.
func (w *groupIDWalk) walk(groups []ir.OperationGroup, at string, depth int) {
	if depth > maxGroupDepth {
		w.truncated = w.truncated || len(groups) > 0
		return
	}
	for i, g := range groups {
		path := at + "[" + strconv.Itoa(i) + "]"
		w.vs = appendGroupIDViolation(w.vs, w.doc, g, path)
		w.walk(g.Groups, path+".Groups", depth+1)
	}
}

// appendGroupIDViolation checks one group's ID. An empty ID is left to
// checkDeclaredIDs, which reports it as ir/empty-group-id.
func appendGroupIDViolation(vs []Violation, doc *ir.Document, g ir.OperationGroup, path string) []Violation {
	id := string(g.ID)
	if id == "" {
		return vs
	}
	idPath, hasPath := ir.IDPath(ir.IDKindGroup, id)
	segments := strings.Split(idPath, ir.IDSeparator)
	if !ir.WellFormedID(ir.IDKindGroup, id) || !hasPath || slices.Contains(segments, "") {
		return append(vs, Violation{
			Code:    "ir/id-malformed",
			Message: "group id " + id + " is not g/<space>/<path>; every segment must be non-empty",
			Path:    path,
		})
	}
	space, _ := ir.IDSpace(ir.IDKindGroup, id)
	if space == ir.IDSpaceSynth {
		return appendSynthGroupViolation(vs, id, segments, path)
	}
	return appendDeclaredGroupViolation(vs, doc, g, segments, path)
}

// appendSynthGroupViolation holds a synthesized group's path to
// <format>/<rule>[/<key>], with a known rule and a key exactly when the rule
// takes one.
func appendSynthGroupViolation(vs []Violation, id string, segments []string, path string) []Violation {
	ok := len(segments) >= 2
	if ok {
		rule, keys := segments[1], segments[2:]
		switch rule {
		case ir.SynthRuleDefault, ir.SynthRuleWebhooks:
			ok = len(keys) == 0
		case ir.SynthRulePathPrefix:
			ok = len(keys) == 1
			if ok {
				_, ok = ir.UnescapeIDSegment(keys[0])
			}
		default:
			ok = false
		}
	}
	if ok {
		return vs
	}
	return append(vs, Violation{
		Code: "ir/group-id-synth-rule",
		Message: "group id " + id + " is in the synthesized space but is not " +
			"g/synth/<format>/<default|webhooks|path-prefix/<key>>",
		Path: path,
	})
}

// appendDeclaredGroupViolation holds a declared group's ID to a name chain, and
// a tag-declared one to a single escaped name that, when a TagDef carries it,
// leaves the group a pointer back to that declaration.
func appendDeclaredGroupViolation(vs []Violation, doc *ir.Document, g ir.OperationGroup, segments []string, path string) []Violation {
	if len(segments) < 2 || segments[0] != ir.IDTagsSegment {
		return vs // another format's name chain; the shape check above is all that is generic
	}
	name, ok := ir.UnescapeIDSegment(segments[1])
	if !ok || len(segments) != 2 {
		return append(vs, Violation{
			Code:    "ir/id-malformed",
			Message: "group id " + string(g.ID) + " is not g/<space>/tags/<escaped name>",
			Path:    path,
		})
	}
	if g.Provenance.Pointer != "" || !hasTagDef(doc, name) {
		return vs
	}
	return append(vs, Violation{
		Code:    "ir/group-id-tag-pointer",
		Message: "group id " + string(g.ID) + " names a declared tag but the group records no pointer to its declaration",
		Path:    path,
	})
}

// hasTagDef reports whether the document declares a tag called name.
func hasTagDef(doc *ir.Document, name string) bool {
	return slices.ContainsFunc(doc.TagDefs, func(t ir.TagDef) bool { return t.Name == name })
}
