// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// message fills, under the replacement policy, the replacements dep's message
// names, on a deprecated node, self, whose own keys write none, so that
// nothing the promotion fills is read over: each name named reads in the
// message, resolved by find. They fill
// the field all or none, a name resolving to self passed over and a repeat kept
// once, so a name naming nothing, or several entities, leaves the field unset
// in silence, the prose keeping it. prov is marked where they fill it. The rule
// is docs/compilers/openapi.md#replacement-inference.
func (r *resolver) message(own ir.Unmodeled, dep *ir.Deprecation, prov *ir.Provenance, self ir.EntityRef,
	named func(string) []string, find func(string) (ir.EntityRef, int),
) {
	if !r.c.ReplacementInference.Honours() || dep == nil || r.p.Writes(own, lowering.TargetDeprecationReplacedBy) {
		return
	}
	names := named(dep.Message)
	refs := make([]ir.EntityRef, 0, len(names))
	for _, name := range names {
		ref, matched := find(name)
		if matched != 1 {
			return
		}
		if ref != self && !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return
	}
	dep.ReplacedBy = refs
	prov.Inferred = ir.JoinInferred(prov.Inferred, lowering.ReplacementInferenceHeuristic)
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
