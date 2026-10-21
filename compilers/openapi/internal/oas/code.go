package oas

import (
	"encoding/json/jsontext"
	"slices"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// code applies a rule written in Go. A name the engine does not know is a
// table error, reported as one.
func (r ruler) code(rule Rule) {
	switch rule.Name {
	case "tag-parents":
		tagParents(r, rule)
	case "path-template-parameters":
		templateParameters(r, rule)
	case "path-templates-distinct":
		distinctTemplates(r, rule)
	case "server-url-template":
		serverTemplate(r, rule)
	case "server-url-variables":
		serverVariables(r, rule)
	case "server-url-parts":
		serverURLParts(r, rule)
	case "additional-operations-methods":
		additionalMethods(r, rule)
	case "encoding-applies":
		encodingApplies(r, rule)
	default:
		r.w.add(diag.Newf(ir.SeverityError, diag.InternalInvariant, r.w.locate(r.f.at.ptr),
			"the %s names the rule %q, which the engine does not implement", r.f.obj.Name, rule.Name), r.f.at.ptr)
	}
}

// standardMethods are the methods a Path Item Object has a field for.
func standardMethods() []string {
	return []string{"get", "put", "post", "delete", "options", "head", "patch", "trace", "query"}
}

// tagParents reports a tag whose parent no tag declares, and a tag nested under
// itself through its parents. A name declared twice is nested as its first
// declaration says, as the lowering groups it.
func tagParents(r ruler, rule Rule) {
	list := nodeview.Deref(r.f.byKey["tags"])
	if list == nil || list.Kind != yaml.SequenceNode {
		return
	}
	var names []string
	parent := map[string]string{}
	declared := map[string]bool{}
	for _, item := range list.Content {
		kv := r.keysOf(item)
		name, ok := textOf(kv["name"])
		if !ok || declared[name] {
			continue
		}
		declared[name] = true
		names = append(names, name)
		if up, ok := textOf(kv["parent"]); ok {
			parent[name] = up
		}
	}
	onCycle := cycles(names, parent)
	for i, item := range list.Content {
		kv := r.keysOf(item)
		name, named := textOf(kv["name"])
		up, nested := textOf(kv["parent"])
		ptr := itemPointer(r.f.at.ptr.AppendToken("tags"), i).AppendToken("parent")
		switch {
		case !named || !nested:
		case !declared[up]:
			r.fail(rule, classReference, ptr, "tag "+quote(name)+" is nested under "+quote(up)+", which no tag declares")
		case parent[name] == up && onCycle[name]:
			r.fail(rule, classCircular, ptr, "tag "+quote(name)+" is nested under itself through its parents")
		}
	}
}

// cycles returns the names that following parents from leads back to. A walk
// stops at a name an earlier walk reached, so each name is stepped through
// once, and the work is linear in the names however long a chain or cycle is.
func cycles(names []string, parent map[string]string) map[string]bool {
	on := map[string]bool{}
	done := map[string]bool{}
	for _, start := range names {
		at := map[string]int{}
		var path []string
		name := start
		for range len(names) + 1 {
			if done[name] {
				break
			}
			if i, seen := at[name]; seen {
				for _, n := range path[i:] {
					on[n] = true
				}
				break
			}
			at[name] = len(path)
			path = append(path, name)
			up, ok := parent[name]
			if !ok {
				break
			}
			name = up
		}
		for _, n := range path {
			done[n] = true
		}
	}
	return on
}

// quote spells a name in the double quotes a message uses.
func quote(s string) string { return `"` + s + `"` }

// operationAt is one operation of a path item, where it is written.
type operationAt struct {
	method string
	node   *yaml.Node
	ptr    jsontext.Pointer
}

// operationsOf lists the operations a path item writes, its own methods and the
// entries of additionalOperations.
func operationsOf(r ruler, item map[string]*yaml.Node, ptr jsontext.Pointer) []operationAt {
	var out []operationAt
	for _, m := range standardMethods() {
		if op, ok := item[m]; ok {
			out = append(out, operationAt{method: m, node: op, ptr: ptr.AppendToken(m)})
		}
	}
	extra := ptr.AppendToken("additionalOperations")
	for _, p := range r.w.view.MappingPairs(item["additionalOperations"]) {
		out = append(out, operationAt{method: p.Key, node: p.Val, ptr: extra.AppendToken(p.Key)})
	}
	return out
}

// expressions returns the names of the template expressions of a path. A "#"
// ends the path, as it ends what the path-key check reads (GitHub #602).
func expressions(path string) []string {
	before, _, _ := strings.Cut(path, "#")
	return braced(before)
}

// braced returns the text of each {group} s writes, up to the first brace no
// group closes.
func braced(s string) []string {
	var out []string
	rest := s
	for {
		_, after, ok := strings.Cut(rest, "{")
		if !ok {
			return out
		}
		name, tail, closed := strings.Cut(after, "}")
		if !closed {
			return out
		}
		out = append(out, name)
		rest = tail
	}
}

// distinctTemplates reports a path that is the same as an earlier one once the
// names of its template expressions are set aside.
func distinctTemplates(r ruler, rule Rule) {
	first := map[string]string{}
	for _, p := range r.f.pairs {
		if strings.HasPrefix(p.Key, "x-") {
			continue
		}
		shape := p.Key
		for _, name := range expressions(p.Key) {
			shape = strings.Replace(shape, "{"+name+"}", "{}", 1)
		}
		if earlier, dup := first[shape]; dup {
			r.fail(rule, classDuplicate, r.f.at.ptr.AppendToken(p.Key),
				"path "+quote(p.Key)+" is identical to "+quote(earlier)+" once the template names are set aside")
			continue
		}
		first[shape] = p.Key
	}
}

// pathParam is a path parameter a parameter list writes.
type pathParam struct {
	name string
	ptr  jsontext.Pointer
}

// paramList is the path parameters a parameter list writes. A parameter the
// document does not let the check read, a $ref to another document or to
// nothing, may be one too, so it answers for any template expression.
type paramList struct {
	found  []pathParam
	unread bool
}

// answers reports whether the list holds a path parameter named name, or
// might.
func (l paramList) answers(name string) bool {
	return l.unread || slices.ContainsFunc(l.found, func(pp pathParam) bool { return pp.name == name })
}

// pathParams reads the path parameters a parameter list writes, a $ref into
// the components read there.
func pathParams(r ruler, list *yaml.Node, ptr jsontext.Pointer) paramList {
	l := nodeview.Deref(list)
	if l == nil || l.Kind != yaml.SequenceNode {
		return paramList{}
	}
	var out paramList
	for i, entry := range l.Content {
		kv := r.keysOf(entry)
		if ref, ok := textOf(kv["$ref"]); ok {
			kv = r.keysOf(localParameter(r, ref))
			if _, again := kv["$ref"]; again || len(kv) == 0 {
				out.unread = true
				continue
			}
		}
		in, _ := textOf(kv["in"])
		if name, ok := textOf(kv["name"]); ok && in == "path" {
			out.found = append(out.found, pathParam{name: name, ptr: itemPointer(ptr, i)})
		}
	}
	return out
}

// localParameter follows a $ref of the form #/components/parameters/name in the
// document being checked, one hop.
func localParameter(r ruler, ref string) *yaml.Node {
	name, ok := strings.CutPrefix(ref, "#/components/parameters/")
	if !ok || r.w.root == nil || strings.Contains(name, "/") {
		return nil
	}
	name = strings.NewReplacer("~1", "/", "~0", "~").Replace(name)
	node := r.w.view.ChildByToken(nodeview.Deref(r.w.root), "components")
	node = r.w.view.ChildByToken(nodeview.Deref(node), "parameters")
	return r.w.view.ChildByToken(nodeview.Deref(node), name)
}

// templateParameters reports a path parameter that names no template expression
// of its path, and a template expression that no path parameter answers: one in
// the path item itself, or one in each of its operations.
func templateParameters(r ruler, rule Rule) {
	for _, p := range r.f.pairs {
		item := r.keysOf(p.Val)
		if !strings.HasPrefix(p.Key, "x-") && len(item) > 0 {
			checkPathItem(r, rule, p.Key, item)
		}
	}
}

// checkPathItem judges one path item against the template expressions of its
// path. An item with no operation and no parameter has nothing to answer with,
// which the specification allows of an empty item. One that writes a $ref
// shares what the item it names holds with the operations written beside it,
// as the lowering does, so a path parameter there may answer for any of them.
func checkPathItem(r ruler, rule Rule, path string, item map[string]*yaml.Node) {
	ptr := r.f.at.ptr.AppendToken(path)
	exprs := expressions(path)
	shared := pathParams(r, item["parameters"], ptr.AppendToken("parameters"))
	if _, refers := item["$ref"]; refers {
		shared.unread = true
	}
	reportStrayParams(r, rule, path, exprs, shared)
	ops := operationsOf(r, item, ptr)
	perOperation := make([]paramList, 0, len(ops))
	for _, op := range ops {
		own := pathParams(r, r.keysOf(op.node)["parameters"], op.ptr.AppendToken("parameters"))
		reportStrayParams(r, rule, path, exprs, own)
		perOperation = append(perOperation, own)
	}
	for _, name := range exprs {
		if unanswered(name, shared, perOperation) {
			r.fail(rule, classReference, ptr,
				"template expression {"+name+"} of "+quote(path)+" has no path parameter in the path item or in each of its operations")
		}
	}
}

// unanswered reports whether the expression has no path parameter in the path
// item and some operation lacks one too.
func unanswered(name string, shared paramList, perOperation []paramList) bool {
	return !shared.answers(name) && slices.ContainsFunc(perOperation, func(own paramList) bool { return !own.answers(name) })
}

// reportStrayParams reports each path parameter that names none of exprs.
func reportStrayParams(r ruler, rule Rule, path string, exprs []string, params paramList) {
	for _, pp := range params.found {
		if !slices.Contains(exprs, pp.name) {
			r.fail(rule, classReference, pp.ptr,
				"path parameter "+quote(pp.name)+" matches no template expression of "+quote(path))
		}
	}
}

// serverTemplate reports a server URL that is no template: a brace with no
// partner, an empty variable name, or a variable written twice.
func serverTemplate(r ruler, rule Rule) {
	url, ok := textOf(r.f.byKey["url"])
	if !ok {
		return
	}
	if fault := templateFault(url); fault != "" {
		r.fail(rule, classSyntax, r.f.at.ptr.AppendToken("url"), "field \"url\" "+fault)
	}
}

// serverVariables reports a variable a server URL names in braces that its
// variables map does not define: the URL has nothing to substitute there. A
// group naming no usable variable is the template's fault, not a variable's.
func serverVariables(r ruler, rule Rule) {
	url, ok := textOf(r.f.byKey["url"])
	if !ok {
		return
	}
	defined := r.keysOf(r.f.byKey["variables"])
	for _, name := range braced(url) {
		if _, ok := defined[name]; !ok && name != "" && !strings.Contains(name, "{") {
			r.fail(rule, classReference, r.f.at.ptr.AppendToken("url"),
				"field \"url\" names the variable "+quote(name)+", which variables does not define")
		}
	}
}

// serverURLParts reports a server URL writing a query or a fragment outside
// its variables' braces.
func serverURLParts(r ruler, rule Rule) {
	url, ok := textOf(r.f.byKey["url"])
	if !ok {
		return
	}
	literal := url
	for _, name := range braced(url) {
		literal = strings.Replace(literal, "{"+name+"}", "", 1)
	}
	if i := strings.IndexAny(literal, "?#"); i >= 0 {
		part := map[byte]string{'?': "a query", '#': "a fragment"}[literal[i]]
		r.fail(rule, classSyntax, r.f.at.ptr.AppendToken("url"), "field \"url\" carries "+part)
	}
}

// templateFault says what is wrong with a server URL template, or returns "".
func templateFault(url string) string {
	var seen []string
	rest := url
	for {
		before, after, ok := strings.Cut(rest, "{")
		if strings.Contains(before, "}") {
			return "has a } with no { before it"
		}
		if !ok {
			return ""
		}
		name, tail, closed := strings.Cut(after, "}")
		switch {
		case !closed:
			return "has a { with no } after it"
		case name == "" || strings.Contains(name, "{"):
			return "has a variable with no usable name"
		case slices.Contains(seen, name):
			return "writes the variable " + quote(name) + " more than once"
		}
		seen = append(seen, name)
		rest = tail
	}
}

// additionalMethods reports an additionalOperations entry named for a method
// that has a field of its own.
func additionalMethods(r ruler, rule Rule) {
	base := r.f.at.ptr.AppendToken("additionalOperations")
	for _, p := range r.w.view.MappingPairs(r.f.byKey["additionalOperations"]) {
		if slices.Contains(standardMethods(), strings.ToLower(p.Key)) {
			r.fail(rule, classValues, base.AppendToken(p.Key),
				"additionalOperations holds an entry for "+quote(p.Key)+", which the field "+quote(strings.ToLower(p.Key))+" defines")
		}
	}
}

// contentOwners are the objects whose content map holds Media Type Objects
// keyed by a media type.
func contentOwners() []Kind { return []Kind{RequestBody, Response, Parameter, Header} }

// encodingApplies warns of an encoding a media type writes where the
// specification gives it no effect: encoding under a key that is no form media
// type, prefixEncoding or itemEncoding under one that is not multipart, and,
// before 3.2, encoding outside a request body. A Media Type Object held under
// any other key, a component's name, names no media type, so it is not judged.
func encodingApplies(r ruler, rule Rule) {
	if !slices.Contains(contentOwners(), r.f.at.in) {
		return
	}
	// RFC 6838 §4.2: names are case-insensitive, and parameters follow a ';'.
	name, _, _ := strings.Cut(r.f.at.held, ";")
	name = strings.ToLower(strings.TrimSpace(name))
	multipart := strings.HasPrefix(name, "multipart/")
	for _, field := range []string{"encoding", "prefixEncoding", "itemEncoding"} {
		if !r.f.writes(field, r.w.minor) {
			continue
		}
		scope := ""
		switch {
		case field == "encoding" && !multipart && name != "application/x-www-form-urlencoded":
			scope = "to multipart and application/x-www-form-urlencoded media types alone"
		case field != "encoding" && !multipart:
			scope = "to multipart media types alone"
		case field == "encoding" && r.w.minor < V32 && r.f.at.in != RequestBody:
			scope = "to request bodies alone"
		}
		if scope != "" {
			r.fail(rule, classNoEffect, r.f.at.ptr.AppendToken(field),
				"field "+quote(field)+" has no effect under "+quote(r.f.at.held)+", which the specification applies "+scope)
		}
	}
}
