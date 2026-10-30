// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package document

import (
	"encoding/json/jsontext"
	"strconv"

	"github.com/speakeasy-api/openapi/extensions"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// documentExtensions collects the x-* of every site. ir.Document is the nearest
// node with an Unmodeled map for all of them, so each object's entries are keyed
// under the source path it was written at — see annotation.ExtensionsUnder for
// why one unscoped key for all of them would not do.
func documentExtensions(c lowering.Ctx) (ir.Unmodeled, []ir.Diagnostic) {
	all := sites(c)
	ext := make([]annotation.ExtensionSite, 0, len(all))
	for _, s := range all {
		ext = append(ext, annotation.ExtensionSite{Scope: s.scope, Owner: s.owner, Ext: s.model.GetExtensions()})
	}
	return annotation.ExtensionsAt(c.ProvenanceAt, ext...)
}

// documentUnknownKeys collects the keys the OpenAPI model names no field for
// from every site.
//
// ir.Document is the nearest node with an Unmodeled map for all of them, so each
// object's keys are scoped by the source path they were written at. One unscoped
// "openapi:status" would be a single key for every object, and the entry that
// survived would be whichever site ran last.
func documentUnknownKeys(c lowering.Ctx, p *ir.Unmodeled) []ir.Diagnostic {
	all := sites(c)
	diags := make([]ir.Diagnostic, 0, len(all))
	for _, s := range all {
		diags = append(diags, annotation.UnknownKeysUnder(p, s.model, c.ProvenanceAt, s.owner, s.scope)...)
	}
	return diags
}

// site is one object around the document metadata that lowers to no node of
// its own, so its x-* and its undeclared keys ride on ir.Document, keyed under
// scope: the path from the root down to the object, empty for the root itself,
// which ir.Document stands for. A tag's path holds its index rather than its
// name, which is the pointer it is written at: OpenAPI's requirement that tag
// names be unique is the document's to keep, not this compiler's to rely on,
// and two tags spelled alike would silently leave one entry.
type site struct {
	scope string
	owner jsontext.Pointer
	model extensible
}

// extensible is a parsed object that carries x-* extensions.
type extensible interface {
	GetExtensions() *extensions.Extensions
}

// sites returns every site: the root, info with its contact and license, the
// root externalDocs, the components object, and each declared tag with its own
// externalDocs, since neither ir.TagDef nor ir.Link holds an Unmodeled map.
//
// The components object is a site, unlike the maps beneath it: `paths`,
// `responses` and a callback each embed a sequenced map, so every key they hold
// is an entry of it (docs/parser-library.md#embedded-map-keys), while the
// Components Object is a fixed-field struct and a key it does not define is as
// undeclared as one on any other site.
func sites(c lowering.Ctx) []site {
	info := c.Doc.GetInfo()
	infoPtr := ids.Ptr("info")
	out := []site{
		{"", "", c.Doc},
		{"info", infoPtr, info},
		{"info/contact", infoPtr + ids.Ptr("contact"), info.GetContact()},
		{"info/license", infoPtr + ids.Ptr("license"), info.GetLicense()},
		{"externalDocs", ids.Ptr("externalDocs"), c.Doc.GetExternalDocs()},
		{"components", ids.Ptr("components"), c.Doc.GetComponents()},
	}
	for i, t := range c.Doc.GetTags() {
		if t == nil {
			continue
		}
		index := strconv.Itoa(i)
		ptr := ids.Ptr("tags", index)
		out = append(out,
			site{"tags/" + index, ptr, t},
			site{"tags/" + index + "/externalDocs", ptr + ids.Ptr("externalDocs"), t.GetExternalDocs()})
	}
	return out
}
