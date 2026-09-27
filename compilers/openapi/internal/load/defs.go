package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// defsRef is one "#/$defs/..." reference and the definition the resolver's own
// rule names for it (defs.Target); target is nil when the rule names none.
type defsRef struct {
	js      *schemaRef
	written *references.Reference
	target  *schemaRef
	at      jsontext.Pointer
}

// defsRefs collects every schema reference whose pointer the resolver reads
// relative to the schema spelling it, with the rule's answer for each. A tree
// that spells no such pointer is not walked: that is every document without a
// $defs reference, and the walk is a second pass over the whole model.
func defsRefs(ctx context.Context, root *yaml.Node, doc *soa.OpenAPI) []defsRef {
	if !spellsDefsPointer(root) {
		return nil
	}
	var out []defsRef
	for item := range soa.Walk(ctx, doc) {
		_ = item.Match(soa.Matcher{Schema: func(js *schemaRef) error {
			pointer, ok := heldDefsPointer(js)
			if !ok {
				return nil
			}
			r := defsRef{js: js, written: js.GetSchema().Ref}
			r.target, r.at, _ = defs.Target(doc, js, pointer)
			out = append(out, r)
			return nil
		}})
	}
	return out
}

// withDefsHeld runs f with every "#/$defs/..." reference taken out of the
// resolver's reach, then puts each back as written.
//
// The resolver's own reading of such a pointer depends on what it resolved
// before: it caches the first definition it finds for a pointer and hands it to
// every later reference spelling that pointer anywhere in the document, and
// resolves a reference it reached through another against that other's
// definitions. One schema's property could be typed with another schema's
// definition, which of the two depending on declaration order (GitHub #557).
// Held out, those references are resolved afterwards by resolveDefs, to the
// definition the resolver's rule names for each in its own place.
func withDefsHeld(refs []defsRef, f func()) {
	for _, r := range refs {
		r.js.GetSchema().Ref = nil
	}
	defer restoreDefs(refs)
	f()
}

func restoreDefs(refs []defsRef) {
	for _, r := range refs {
		r.js.GetSchema().Ref = r.written
	}
}

// resolveDefs resolves each held reference the rule has an answer for, by
// handing the resolver the pointer of the definition itself, and puts every
// reference back as written. The references the rule has no answer for stay out
// of reach meanwhile, so a chain that meets one ends there, unresolved, rather
// than being read by the resolver's own order-dependent lookup.
func resolveDefs(ctx context.Context, locate scan.Locator, doc *soa.OpenAPI, path string, opts Options, refs []defsRef) (diags []ir.Diagnostic) {
	defer func() {
		if r := recover(); r != nil {
			diags = append(diags, diag.Newf(ir.SeverityError, diag.UnresolvedRef, locate(nil),
				"reference resolver panicked (%v)", r))
		}
	}()
	defer restoreDefs(refs)
	for _, r := range refs {
		s := r.js.GetSchema()
		if r.target == nil {
			s.Ref = nil
			continue
		}
		ref := references.Reference("#" + fragmentOf(r.at))
		s.Ref = &ref
	}
	for _, r := range refs {
		if r.target == nil {
			continue
		}
		_, err := r.js.Resolve(ctx, oas3.ResolveOptions{
			TargetLocation: path, RootDocument: doc, TargetDocument: doc,
			DisableExternalRefs: !opts.AllowExternalRefs,
		})
		if err != nil {
			diags = append(diags, diag.Newf(ir.SeverityError, diag.UnresolvedRef,
				locate(r.js.GetSchema().GetRootNode()), "%s", err.Error()))
		}
	}
	return diags
}

// fragmentOf spells pointer as a URI fragment the resolver decodes back to
// exactly pointer (references.Reference.GetJSONPointer trims and
// query-unescapes): every byte outside a plain set is percent-encoded, '+' and
// '%' and '#' and whitespace included.
func fragmentOf(pointer jsontext.Pointer) string {
	const plain = "/~$-_.!*'(),;:@&=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, 0, len(pointer))
	for i := 0; i < len(pointer); i++ {
		b := pointer[i]
		if containsByte(plain, b) {
			out = append(out, b)
			continue
		}
		out = append(out, fmt.Sprintf("%%%02X", b)...)
	}
	return string(out)
}

func containsByte(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}

// spellsDefsPointer reports whether any $ref in the tree is a same-document
// "#/$defs/..." pointer as the resolver decodes one. It visits each node once,
// never through an alias, so its cost is the node count the budget bounds.
func spellsDefsPointer(root *yaml.Node) bool {
	stack := []*yaml.Node{root}
	for visited := 0; len(stack) > 0 && visited < sourceindex.MaxIndexedNodes; visited++ {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil || n.Kind == yaml.AliasNode {
			continue
		}
		if n.Kind == yaml.MappingNode && mapsDefsPointer(n) {
			return true
		}
		stack = append(stack, n.Content...)
	}
	return false
}

func mapsDefsPointer(n *yaml.Node) bool {
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Value != "$ref" || v.Kind != yaml.ScalarNode {
			continue
		}
		ref := references.Reference(v.Value)
		if ref.GetURI() == "" && defs.IsPointer(jsontext.Pointer(ref.GetJSONPointer())) {
			return true
		}
	}
	return false
}

// heldDefsPointer reports the "#/$defs/..." pointer of a schema reference load
// holds out of the resolver's own pass: one with no document part, decoded as
// the resolver decodes it. reach reads the same set, so the edges it checks
// are the ones resolveDefs hands the resolver.
func heldDefsPointer(js *schemaRef) (jsontext.Pointer, bool) {
	if js == nil || !js.IsReference() || js.GetRef().GetURI() != "" {
		return "", false
	}
	pointer := jsontext.Pointer(js.GetRef().GetJSONPointer())
	return pointer, defs.IsPointer(pointer)
}
