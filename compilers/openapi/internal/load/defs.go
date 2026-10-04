package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"

	"github.com/dexpace/morphic/compilers/openapi/internal/defs"
)

// heldRef is one "#/$defs/..." reference and the definition the resolver's own
// rule names for it (defs.Reader.Target); target is nil when the rule names none.
type heldRef struct {
	js      *schemaRef
	written *references.Reference // the $ref as written, which is put back
	target  *schemaRef            // the definition the rule names
	at      jsontext.Pointer      // where that definition is written
	site    jsontext.Pointer      // where the reference is written
}

// heldRefs collects every schema reference whose pointer the resolver reads
// relative to the schema spelling it, with the answer rule gives for each. One
// reader over doc serves them all, which keeps the work proportional to the
// document. A tree that spells no such pointer is not walked: that is every
// document without a $defs reference, and the walk is a second pass over the
// whole model.
func heldRefs(ctx context.Context, doc *soa.OpenAPI, rule *defs.Reader) []heldRef {
	if !anyScalar(doc.GetRootNode(), isDefsRef) {
		return nil
	}
	var out []heldRef
	for item := range soa.Walk(ctx, doc) {
		_ = item.Match(soa.Matcher{Schema: func(js *schemaRef) error {
			pointer, ok := heldDefsPointer(js)
			if !ok {
				return nil
			}
			r := heldRef{js: js, written: js.GetSchema().Ref, site: jsontext.Pointer(item.Location.ToJSONPointer())}
			r.target, r.at, _ = rule.Target(js, pointer)
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
func withDefsHeld(refs []heldRef, f func()) {
	for _, r := range refs {
		r.js.GetSchema().Ref = nil
	}
	defer restoreDefs(refs)
	f()
}

func restoreDefs(refs []heldRef) {
	for _, r := range refs {
		r.js.GetSchema().Ref = r.written
	}
}

// resolveHeld resolves each held reference the rule answers for by handing the
// resolver the pointer of the definition itself, reports each as the walk's
// references are (see visit), and puts every reference back as written. One the
// rule has no answer for stays out of reach meanwhile, so a chain that meets it
// ends there rather than reading the resolver's order-dependent lookup, and is
// reported as a definition the resolver cannot find: the lowering reports only
// the positions it models. It returns where a panic stopped it.
func (p *resolution) resolveHeld(refs []heldRef) (site jsontext.Pointer, err error) {
	defer recovered(&err, resolverPanics)
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
		site = r.site
		if r.target == nil {
			p.failures = append(p.failures, failureDiag(p.at(site), *r.written, trail{},
				fmt.Errorf("definition not found: %s", *r.written)))
			continue
		}
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
		if strings.IndexByte(plain, b) >= 0 {
			out = append(out, b)
			continue
		}
		out = append(out, fmt.Sprintf("%%%02X", b)...)
	}
	return string(out)
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
