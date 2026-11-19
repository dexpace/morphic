// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// Infer fills, under the replacement policy, what the message of each
// deprecated operation, parameter and model member names as its replacement,
// where its own keys name none (message), and reports each message naming one
// that it cannot settle. It runs after Resolve, as an inference reads after
// every promotion (docs/compilers/openapi.md#policies), and reads nothing
// while policy is off. promoter is the run's promoter, and compiledDocumentView the document, which is
// whole.
func Infer(policy Inference, promoter promotion.Promoter, compiledDocumentView *compileddoc.CompiledDocumentView) []ir.Diagnostic {
	if !policy.Honours() {
		return nil
	}
	resolverValue := resolver{p: promoter, w: compiledDocumentView}
	diags := resolverValue.inferService()
	for _, id := range slices.Sorted(maps.Keys(compiledDocumentView.Types)) {
		if model, ok := compiledDocumentView.Types[id].(*ir.Model); ok && model != nil {
			diags = resolverValue.inferMembers(diags, model)
		}
	}
	return diags
}

// inferService reads the message of every operation of the document, naming
// an operation by its route, and of each of its parameters, naming a parameter
// beside it in code.
func (r *resolver) inferService() []ir.Diagnostic {
	ops := r.w.Ops
	routes := indexRoutes(ops)
	var diags []ir.Diagnostic
	for _, op := range ops {
		diags = append(diags, r.message(deprecated{op.Unmodeled, op.Deprecation, &op.Provenance,
			ir.EntityRef{Operation: op.ID}}, scope{"operation", routes.named, routes.find})...)
		param := r.paramFinder(op)
		for i := range op.Params {
			parameter := &op.Params[i]
			diags = append(diags, r.message(deprecated{parameter.Unmodeled, parameter.Deprecation, &parameter.Provenance,
				ir.EntityRef{Param: parameter.ID}}, scope{"parameter", codeSpans, param})...)
		}
	}
	return diags
}

// inferMembers reads the message of each of model's members, naming a member of
// model in code, and returns diags with what it reports appended, as every
// model's are, so a model reporting nothing allocates nothing.
func (r *resolver) inferMembers(diags []ir.Diagnostic, model *ir.Model) []ir.Diagnostic {
	member := r.memberFinder(model.ID)
	for i := range model.Properties {
		property := &model.Properties[i]
		diags = append(diags, r.message(deprecated{property.Unmodeled, property.Deprecation, &property.Provenance,
			ir.EntityRef{Property: property.ID}}, scope{"property", codeSpans, member})...)
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
// names it, the names a message gives, each with where it is written, and what
// finds the entity a name names, returning how many it matches and the entity,
// read only where that is one.
type scope struct {
	class string
	named func(string) []named
	find  func(string) (ir.EntityRef, int)
}

// message fills, under the replacement policy, the replacements deprecatedValue's message
// names, where deprecatedValue is deprecated and its own keys write none, so that nothing
// the promotion fills is read over. Only a name a phrase introduces is read
// (introducedNames). A message naming no entity of deprecatedValue's class, or deprecatedValue alone,
// names no replacement. One naming another fills the field all or
// none, deprecatedValue passed over and a repeat kept once: a name naming nothing, or
// several entities, leaves it unset, reported, the prose keeping it. deprecatedValue is
// marked where they fill it. The rule is
// docs/compilers/openapi.md#replacement-inference.
func (r *resolver) message(deprecatedValue deprecated, scopeValue scope) []ir.Diagnostic {
	if deprecatedValue.dep == nil || r.p.Writes(deprecatedValue.own, promotion.TargetDeprecationReplacedBy) {
		return nil
	}
	names := introducedNames(deprecatedValue.dep.Message, scopeValue.named(deprecatedValue.dep.Message))
	refs := make([]ir.EntityRef, 0, len(names))
	named, why := false, ""
	for _, name := range names {
		ref, matched := scopeValue.find(name)
		if matched == 1 && ref == deprecatedValue.self {
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
			why = fmt.Sprintf("%q names %s %s", name, quantity, scopeValue.class)
		}
	}
	switch {
	case !named:
		return nil
	case why != "":
		at := *deprecatedValue.prov
		at.Inferred = ""
		return []ir.Diagnostic{diag.Newf(ir.SeverityInfo, diag.UndecidedReplacement, at,
			"no replacement is inferred: %s", why)}
	}
	deprecatedValue.dep.ReplacedBy = refs
	deprecatedValue.prov.Inferred = ir.JoinInferred(deprecatedValue.prov.Inferred, InferenceHeuristic)
	return nil
}

// codeSpans returns the content of each code span in text, in order, and
// where the span is written, its backticks included, as CommonMark reads one
// (§6.1): a run of backticks opens a span that the next
// run of as many closes, a line ending in it reads as a space, and a space at
// each end is stripped once from content that is not all spaces. A run nothing
// closes is literal text. The runs are found in one pass, and the next run of
// each length by a cursor that only moves forward.
func codeSpans(text string) []named {
	runs := backtickRuns(text)
	byLength := map[int][]int{}
	for i, backtickRun := range runs {
		byLength[backtickRun.length] = append(byLength[backtickRun.length], i)
	}
	cursor := map[int]int{}
	var spans []named
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
		spans = append(spans, named{text: spanContent(text[open.start+open.length : closing.start]),
			start: open.start, end: closing.start + closing.length})
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
	byRoute map[string][]ir.OperationID
	methods map[string]bool
}

// indexRoutes indexes ops by route. A binding with no path template, a
// webhook's, is no route a message names.
func indexRoutes(ops []*ir.Operation) routeIndex {
	ix := routeIndex{byRoute: map[string][]ir.OperationID{}, methods: map[string]bool{}}
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
// Each is `<method> <path>`, the path as written, punctuation after it kept,
// written from its method, a backtick before it included, to its path's end,
// a backtick closing it included.
func (ix routeIndex) named(text string) []named {
	words := fields(text)
	var routes []named
	for i := 0; i+1 < len(words); i++ {
		method := strings.TrimLeft(words[i].text, "`(")
		if !ix.methods[method] {
			continue
		}
		next := words[i+1]
		rest := strings.TrimLeft(next.text, "`")
		path, _, closed := strings.Cut(rest, "`")
		if !strings.HasPrefix(path, "/") {
			continue
		}
		end := next.start + len(next.text) - len(rest) + len(path)
		if closed {
			end++
		}
		start := words[i].start + len(words[i].text) - len(strings.TrimLeft(words[i].text, "("))
		routes = append(routes, named{text: method + " " + path, start: start, end: end})
	}
	return routes
}

// fields returns the runs of text between white space, each with where it
// starts, as strings.Fields splits text.
func fields(text string) []named {
	var out []named
	start := -1
	for i, c := range text {
		switch {
		case !unicode.IsSpace(c) && start < 0:
			start = i
		case unicode.IsSpace(c) && start >= 0:
			out = append(out, named{text: text[start:i], start: start, end: i})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, named{text: text[start:], start: start, end: len(text)})
	}
	return out
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
