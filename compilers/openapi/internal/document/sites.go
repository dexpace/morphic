package document

import (
	"encoding/json/jsontext"
	"strconv"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// documentExtensions collects the x-* of every object that lowers to no IR node
// of its own: the document root, the info block and the contact and license
// inside it, the root externalDocs, the components object, and each declared
// tag with its own externalDocs. ir.Document is the nearest node with an
// Unmodeled map for all of them, so each object's entries are keyed under the
// source path it was written at — see annotation.ExtensionsUnder for why one
// unscoped key for all of them would not do.
func documentExtensions(c lowering.Ctx) (ir.Unmodeled, []ir.Diagnostic) {
	return annotation.ExtensionsAt(c.ProvenanceAt, append(rootExtensions(c), tagExtensions(c)...)...)
}

// rootExtensions returns the extension sites a document has exactly one of. The
// root's own take no scope, since ir.Document stands for the OpenAPI Object
// itself; the rest are keyed by the path from it down to the object that wrote
// them.
func rootExtensions(c lowering.Ctx) []annotation.ExtensionSite {
	info := c.Doc.GetInfo()
	infoPtr := ids.Ptr("info")
	return []annotation.ExtensionSite{
		{Scope: "", Owner: "", Ext: c.Doc.GetExtensions()},
		{Scope: "info", Owner: infoPtr, Ext: info.GetExtensions()},
		{Scope: "info/contact", Owner: infoPtr + ids.Ptr("contact"), Ext: info.GetContact().GetExtensions()},
		{Scope: "info/license", Owner: infoPtr + ids.Ptr("license"), Ext: info.GetLicense().GetExtensions()},
		{Scope: "externalDocs", Owner: ids.Ptr("externalDocs"), Ext: c.Doc.GetExternalDocs().GetExtensions()},
		{Scope: "components", Owner: ids.Ptr("components"), Ext: c.Doc.GetComponents().GetExtensions()},
	}
}

// tagExtensions returns the extension sites each declared tag contributes: its
// own, and its externalDocs object's. ir.TagDef holds no Unmodeled map and
// neither does ir.Link, so both ride on the document.
//
// Keyed by the tag's index rather than its name, which is the pointer the tag
// is written at. A name would read better and is what TagDefs are found by, but
// OpenAPI's requirement that tag names be unique is the document's to keep, not
// this compiler's to rely on: two tags spelled alike would silently leave one
// entry.
func tagExtensions(c lowering.Ctx) []annotation.ExtensionSite {
	tags := c.Doc.GetTags()
	out := make([]annotation.ExtensionSite, 0, 2*len(tags))
	for i, t := range tags {
		if t == nil {
			continue
		}
		scope := "tags/" + strconv.Itoa(i)
		ptr := ids.Ptr("tags", strconv.Itoa(i))
		out = append(out,
			annotation.ExtensionSite{Scope: scope, Owner: ptr, Ext: t.GetExtensions()},
			annotation.ExtensionSite{Scope: scope + "/externalDocs", Owner: ptr + ids.Ptr("externalDocs"),
				Ext: t.GetExternalDocs().GetExtensions()})
	}
	return out
}

// documentUnknownKeys collects the keys the OpenAPI model names no field for
// from every object around the document metadata that lowers to no node of its
// own: the root, info with its contact and license, the root externalDocs, the
// components object, and each tag with its own externalDocs.
//
// ir.Document is the nearest node with an Unmodeled map for all of them, so each
// object's keys are scoped by the source path they were written at. One unscoped
// "openapi:status" would be a single key for every object, and the entry that
// survived would be whichever site ran last.
func documentUnknownKeys(c lowering.Ctx, p *ir.Unmodeled) []ir.Diagnostic {
	sites := append(rootUnknownSites(c), tagUnknownSites(c)...)
	diags := make([]ir.Diagnostic, 0, len(sites))
	for _, site := range sites {
		diags = append(diags,
			annotation.UnknownKeysUnder(p, site.model, c.ProvenanceAt, site.owner, site.scope)...)
	}
	return diags
}

// unknownSite is one object's census: what it keys under on the carrier holding
// it, the object's own source pointer, and the parsed object itself.
type unknownSite struct {
	scope string
	owner jsontext.Pointer
	model any
}

// rootUnknownSites returns the census sites a document has exactly one of. The
// root's keys take no scope, since ir.Document stands for the OpenAPI Object
// itself; the rest are keyed by the path from it down to the object that wrote
// them.
//
// The components object is one of them, unlike the maps beneath it: `paths`,
// `responses` and a callback each embed a sequenced map, so every key they hold
// is an entry of it (docs/parser-library.md#embedded-map-keys), while the
// Components Object is a fixed-field struct and a key it does not define is as
// undeclared as one on any other object here.
func rootUnknownSites(c lowering.Ctx) []unknownSite {
	info := c.Doc.GetInfo()
	infoPtr := ids.Ptr("info")
	return []unknownSite{
		{"", "", c.Doc},
		{"info", infoPtr, info},
		{"info/contact", infoPtr + ids.Ptr("contact"), info.GetContact()},
		{"info/license", infoPtr + ids.Ptr("license"), info.GetLicense()},
		{"externalDocs", ids.Ptr("externalDocs"), c.Doc.GetExternalDocs()},
		{"components", ids.Ptr("components"), c.Doc.GetComponents()},
	}
}

// tagUnknownSites returns the census sites each declared tag contributes: its
// own, and its externalDocs object's. Neither ir.TagDef nor ir.Link holds an
// Unmodeled map, so both ride on the document.
//
// Scoped by index rather than name, which is the pointer a tag is written at. A
// name would read better, but OpenAPI's requirement that tag names be unique is
// the document's to keep and not this compiler's to rely on: two tags spelled
// alike would silently leave one entry.
func tagUnknownSites(c lowering.Ctx) []unknownSite {
	tags := c.Doc.GetTags()
	out := make([]unknownSite, 0, 2*len(tags))
	for i, t := range tags {
		if t == nil {
			continue
		}
		index := strconv.Itoa(i)
		ptr := ids.Ptr("tags", index)
		out = append(out,
			unknownSite{"tags/" + index, ptr, t},
			unknownSite{"tags/" + index + "/externalDocs", ptr + ids.Ptr("externalDocs"), t.GetExternalDocs()})
	}
	return out
}
