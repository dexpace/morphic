// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// Infer fills, under the replacement policy, what the message of each
// deprecated operation, parameter and model member names as its replacement,
// where its own keys name none (message), and reports each message naming one
// that it cannot settle. It runs after Resolve, as an inference reads after
// every promotion (docs/compilers/openapi.md#policies). c is the source's
// context.
func Infer(c lowering.Ctx, doc *ir.Document) []ir.Diagnostic {
	if !c.ReplacementInference.Honours() {
		return nil
	}
	r := resolver{c: c, p: c.Promoter(), doc: doc}
	var diags []ir.Diagnostic
	for i := range doc.Services {
		diags = append(diags, r.inferService(&doc.Services[i])...)
	}
	for _, id := range slices.Sorted(maps.Keys(doc.Types)) {
		if m, ok := doc.Types[id].(*ir.Model); ok && m != nil {
			diags = r.inferMembers(diags, m)
		}
	}
	return diags
}

// inferService reads the message of every operation svc holds, nested groups
// included to ir.MaxGroupDepth, naming an operation by its route, and of each
// of its parameters, naming a parameter beside it in code.
func (r *resolver) inferService(svc *ir.Service) []ir.Diagnostic {
	ops := lowering.Operations(svc)
	routes := indexRoutes(ops)
	var diags []ir.Diagnostic
	for _, op := range ops {
		diags = append(diags, r.message(deprecated{op.Unmodeled, op.Deprecation, &op.Provenance,
			ir.EntityRef{Operation: op.ID}}, scope{"operation", routes.named, routes.find})...)
		param := paramFinder(op)
		for i := range op.Params {
			p := &op.Params[i]
			diags = append(diags, r.message(deprecated{p.Unmodeled, p.Deprecation, &p.Provenance,
				ir.EntityRef{Param: p.ID}}, scope{"parameter", codeSpans, param})...)
		}
	}
	return diags
}

// inferMembers reads the message of each of m's members, naming a member of
// m in code, and returns diags with what it reports appended, as every
// model's are, so a model reporting nothing allocates nothing.
func (r *resolver) inferMembers(diags []ir.Diagnostic, m *ir.Model) []ir.Diagnostic {
	member := memberFinder(r.doc.Types, m.ID)
	for i := range m.Properties {
		p := &m.Properties[i]
		diags = append(diags, r.message(deprecated{p.Unmodeled, p.Deprecation, &p.Provenance,
			ir.EntityRef{Property: p.ID}}, scope{"property", codeSpans, member})...)
	}
	return diags
}

// deprecated is a node whose deprecation's message Infer reads: the keys it
// keeps, its deprecation, the provenance a replacement read marks, and the
// entity it is.
type deprecated struct {
	own  ir.Unmodeled
	dep  *ir.Deprecation
	prov *ir.Provenance
	self ir.EntityRef
}

// scope is how a message names an entity of one class: the class, as a report
// names it, the names a message gives, and what finds the entity a name names,
// returning how many it matches and the entity, read only where that is one.
type scope struct {
	class string
	named func(string) []string
	find  func(string) (ir.EntityRef, int)
}

// message fills, under the replacement policy, the replacements n's message
// names, where n is deprecated and its own keys write none, so that nothing
// the promotion fills is read over. A message naming no entity of n's class,
// or n alone, names no replacement. One naming another fills the field all or
// none, n passed over and a repeat kept once: a name naming nothing, or
// several entities, leaves it unset, reported, the prose keeping it. n is
// marked where they fill it. The rule is
// docs/compilers/openapi.md#replacement-inference.
func (r *resolver) message(n deprecated, s scope) []ir.Diagnostic {
	if n.dep == nil || r.p.Writes(n.own, lowering.TargetDeprecationReplacedBy) {
		return nil
	}
	names := s.named(n.dep.Message)
	refs := make([]ir.EntityRef, 0, len(names))
	named, why := false, ""
	for _, name := range names {
		ref, matched := s.find(name)
		if matched == 1 && ref == n.self {
			continue
		}
		named = named || matched > 0
		if matched == 1 {
			if !slices.Contains(refs, ref) {
				refs = append(refs, ref)
			}
			continue
		}
		if why == "" {
			quantity := "no"
			if matched > 1 {
				quantity = "more than one"
			}
			why = fmt.Sprintf("%q names %s %s", name, quantity, s.class)
		}
	}
	switch {
	case !named:
		return nil
	case why != "":
		at := *n.prov
		at.Inferred = ""
		return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.UndecidedReplacement, at,
			"no replacement is inferred: %s", why)}
	}
	n.dep.ReplacedBy = refs
	n.prov.Inferred = ir.JoinInferred(n.prov.Inferred, lowering.ReplacementInferenceHeuristic)
	return nil
}

// codeSpans returns the content of each code span in text, in order, as
// CommonMark reads one (§6.1): a run of backticks opens a span that the next
// run of as many closes, a line ending in it reads as a space, and a space at
// each end is stripped once from content that is not all spaces. A run nothing
// closes is literal text. The runs are found in one pass, and the next run of
// each length by a cursor that only moves forward.
func codeSpans(text string) []string {
	runs := backtickRuns(text)
	byLength := map[int][]int{}
	for i, r := range runs {
		byLength[r.length] = append(byLength[r.length], i)
	}
	cursor := map[int]int{}
	var spans []string
	for i := 0; i < len(runs); {
		open := runs[i]
		same := byLength[open.length]
		k := cursor[open.length]
		for k < len(same) && same[k] <= i {
			k++
		}
		cursor[open.length] = k
		if k == len(same) {
			i++
			continue
		}
		closing := runs[same[k]]
		spans = append(spans, spanContent(text[open.start+open.length:closing.start]))
		i = same[k] + 1
	}
	return spans
}

// backtickRun is a run of backticks in a text: where it starts and how many
// it holds.
type backtickRun struct {
	start, length int
}

// backtickRuns returns the runs of backticks in text, in order.
func backtickRuns(text string) []backtickRun {
	var runs []backtickRun
	for i := 0; i < len(text); {
		k := strings.IndexByte(text[i:], '`')
		if k < 0 {
			break
		}
		start := i + k
		end := start
		for end < len(text) && text[end] == '`' {
			end++
		}
		runs = append(runs, backtickRun{start: start, length: end - start})
		i = end
	}
	return runs
}

// spanContent returns a code span's content as CommonMark reads it.
func spanContent(raw string) string {
	content := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(raw)
	if len(content) > 1 && content[0] == ' ' && content[len(content)-1] == ' ' && strings.Trim(content, " ") != "" {
		return content[1 : len(content)-1]
	}
	return content
}

// routeIndex is a service's operations by the method and path template their
// HTTP bindings send them by, which a message names an operation by, and the
// methods those bindings use.
type routeIndex struct {
	byRoute map[string][]ir.OpID
	methods map[string]bool
}

// indexRoutes indexes ops by route. A binding with no path template, a
// webhook's, is no route a message names.
func indexRoutes(ops []*ir.Operation) routeIndex {
	ix := routeIndex{byRoute: map[string][]ir.OpID{}, methods: map[string]bool{}}
	for _, op := range ops {
		for _, b := range op.Bindings.HTTP {
			if !strings.HasPrefix(b.URITemplate, "/") {
				continue
			}
			key := b.Method + " " + b.URITemplate
			if !slices.Contains(ix.byRoute[key], op.ID) {
				ix.byRoute[key] = append(ix.byRoute[key], op.ID)
			}
			ix.methods[b.Method] = true
		}
	}
	return ix
}

// named returns each route text names: a word that is a method ix holds, a
// backtick or a parenthesis before it aside, and after white space a path,
// inside a code span or out of one, up to the next white space or backtick.
// Each is `<method> <path>`, the path as written, punctuation after it kept.
func (ix routeIndex) named(text string) []string {
	words := strings.Fields(text)
	var routes []string
	for i := 0; i+1 < len(words); i++ {
		method := strings.TrimLeft(words[i], "`(")
		if !ix.methods[method] {
			continue
		}
		path, _, _ := strings.Cut(strings.TrimLeft(words[i+1], "`"), "`")
		if strings.HasPrefix(path, "/") {
			routes = append(routes, method+" "+path)
		}
	}
	return routes
}

// find returns the operation route names, and how many it names: the route as
// written, or, naming none, with the punctuation a sentence puts after a path
// cut from its end.
func (ix routeIndex) find(route string) (ir.EntityRef, int) {
	found := ix.byRoute[route]
	if len(found) == 0 {
		found = ix.byRoute[strings.TrimRight(route, ".,;:!?)")]
	}
	if len(found) != 1 {
		return ir.EntityRef{}, len(found)
	}
	return ir.EntityRef{Operation: found[0]}, 1
}
