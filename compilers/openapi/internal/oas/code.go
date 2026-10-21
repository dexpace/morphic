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
	case "additional-operations-methods":
		additionalMethods(r, rule)
	default:
		r.w.diags = append(r.w.diags, diag.Newf(ir.SeverityError, diag.InternalInvariant, r.w.locate(r.f.at.ptr),
			"the %s names the rule %q, which the engine does not implement", r.f.obj.Name, rule.Name))
	}
}

// standardMethods are the methods a Path Item Object has a field for.
func standardMethods() []string {
	return []string{"get", "put", "post", "delete", "options", "head", "patch", "trace", "query"}
}

// tagParents reports a tag whose parent no tag declares, and a tag nested under
// itself through its parents.
func tagParents(r ruler, rule Rule) {
	list := nodeview.Deref(r.f.byKey["tags"])
	if list == nil || list.Kind != yaml.SequenceNode {
		return
	}
	parent := map[string]string{}
	declared := map[string]bool{}
	for _, item := range list.Content {
		kv := r.keysOf(item)
		if name, ok := textOf(kv["name"]); ok {
			declared[name] = true
			if up, ok := textOf(kv["parent"]); ok {
				parent[name] = up
			}
		}
	}
	for i, item := range list.Content {
		name, _ := textOf(r.keysOf(item)["name"])
		up, nested := parent[name]
		ptr := itemPointer(r.f.at.ptr.AppendToken("tags"), i).AppendToken("parent")
		switch {
		case !nested:
		case !declared[up]:
			r.fail(rule, classReference, ptr, "tag "+quote(name)+" is nested under "+quote(up)+", which no tag declares")
		case nestedInItself(name, parent):
			r.fail(rule, classReference, ptr, "tag "+quote(name)+" is nested under itself through its parents")
		}
	}
}

// nestedInItself reports whether following parents from name leads back to it.
// The walk takes at most one step per tag.
func nestedInItself(name string, parent map[string]string) bool {
	at := name
	for range len(parent) {
		up, ok := parent[at]
		if !ok {
			return false
		}
		if up == name {
			return true
		}
		at = up
	}
	return false
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

// expressions returns the names of the template expressions of a path.
func expressions(path string) []string {
	var out []string
	rest := path
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
// which the specification allows of an empty item.
func checkPathItem(r ruler, rule Rule, path string, item map[string]*yaml.Node) {
	ptr := r.f.at.ptr.AppendToken(path)
	exprs := expressions(path)
	shared := pathParams(r, item["parameters"], ptr.AppendToken("parameters"))
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
