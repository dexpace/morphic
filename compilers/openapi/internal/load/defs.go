package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
)

// defsRef is one "#/$defs/..." reference and the definition the resolver's own
// rule names for it (defs.Target); target is nil when the rule names none.
type defsRef struct {
	js      *schemaRef
	written *references.Reference
	target  *schemaRef
	at      jsontext.Pointer
	site    jsontext.Pointer // where the reference is written
}

// defsRefs collects every schema reference whose pointer the resolver reads
// relative to the schema spelling it, with the rule's answer for each. A tree
// that spells no such pointer is not walked: that is every document without a
// $defs reference, and the walk is a second pass over the whole model.
func defsRefs(ctx context.Context, doc *soa.OpenAPI) []defsRef {
	if !anyScalar(doc.GetRootNode(), isDefsRef) {
		return nil
	}
	var out []defsRef
	for item := range soa.Walk(ctx, doc) {
		_ = item.Match(soa.Matcher{Schema: func(js *schemaRef) error {
			pointer, ok := heldDefsPointer(js)
			if !ok {
				return nil
			}
			r := defsRef{js: js, written: js.GetSchema().Ref, site: jsontext.Pointer(item.Location.ToJSONPointer())}
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
// The resolver caches the first definition it finds for a pointer and hands it
// to every later reference spelling it, and resolves a reference it reached
// through another against that other's definitions, so one schema's property
// could be typed with another's definition, in an order-dependent way (GitHub
// #557). Held out, they are resolved afterwards by resolveHeld, each to the
// definition the rule names in its own place.
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

// resolveHeld resolves each held reference the rule has an answer for, by
// handing the resolver the pointer of the definition itself, and puts every
// reference back as written. The references the rule has no answer for stay out
// of reach meanwhile, so a chain that meets one ends there, unresolved, rather
// than being read by the resolver's own order-dependent lookup.
//
// Each is resolved and reported as the walk's references are (see visit), the
// failure quoting the reference as written. It returns where a panic stopped it.
func (p *resolution) resolveHeld(refs []defsRef) (site jsontext.Pointer, err error) {
	defer recovered(&err)
	defer restoreDefs(refs)
	for _, r := range refs {
		s := r.js.GetSchema()
		s.Ref = nil
		if r.target != nil {
			ref := references.Reference("#" + fragmentOf(r.at))
			s.Ref = &ref
		}
	}
	for _, r := range refs {
		if r.target == nil {
			continue
		}
		site = r.site
		p.visit(site, r.js, *r.written)
	}
	return "", nil
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

// heldDefsPointer reports the "#/$defs/..." pointer of a schema reference load
// holds out of the resolver's own pass: one with no document part, decoded as
// the resolver decodes it. reach reads the same set, so the edges it checks
// are the ones resolveHeld hands the resolver.
func heldDefsPointer(js *schemaRef) (jsontext.Pointer, bool) {
	if js == nil || !js.IsReference() || js.GetRef().GetURI() != "" {
		return "", false
	}
	pointer := jsontext.Pointer(js.GetRef().GetJSONPointer())
	if !defs.IsPointer(pointer) {
		return "", false
	}
	return pointer, true
}
