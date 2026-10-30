// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"

	"github.com/speakeasy-api/openapi/extensions"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// ExtensionsFrom lowers an x-* extension map into namespaced ir.Unmodeled, keys
// prefixed "openapi:" and values serialized to raw JSON. owner is the pointer of
// the object the extensions were written on; each entry is located at its own
// key beneath it and marked ReasonVendorExtension, since the format assigns an
// x-* key no semantics at all.
//
// A caller records the diagnostics whether or not it keeps the entries: an
// object whose every extension failed to serialize returns warnings and no
// entries, so gating them on the entries drops each one exactly when it matters
// (TestAuth_UnserializableExtensionStillWarns and its twins).
func ExtensionsFrom(ext *extensions.Extensions, locate Locator, owner jsontext.Pointer) (ir.Unmodeled, []ir.Diagnostic) {
	return ExtensionsUnder(ext, locate, owner, "")
}

// ExtensionsUnder is ExtensionsFrom with every entry keyed beneath scope, for
// objects whose extensions have no Unmodeled map of their own.
//
// Those objects ride on the nearest node that has one, where "openapi:x-id"
// from two objects would be one key, so scope names the writer: the source path
// from the carrier down to it, or its own keyword where it is not beneath the
// carrier.
//
// Keys cannot collide: an extension name begins with "x-", and a scope extending
// another on its carrier first adds a keyword the compiler writes, which never
// does. The same gap separates them from non-extension keys.
func ExtensionsUnder(ext *extensions.Extensions, locate Locator, owner jsontext.Pointer, scope string) (ir.Unmodeled, []ir.Diagnostic) {
	if ext == nil || ext.Len() == 0 {
		return nil, nil
	}
	prefix := "openapi:"
	if scope != "" {
		prefix += scope + "/"
	}
	out := ir.Unmodeled{}
	var diags []ir.Diagnostic
	for name, node := range ext.All() {
		raw, err := RawFromNode(node)
		if err != nil || raw == nil {
			diags = append(diags, diag.Newf(ir.SeverityWarning, diag.DegradedConstruct,
				locate(owner), "extension %q could not be serialized", name))
			continue
		}
		out[prefix+name] = ir.UnmodeledEntry{
			Reason:     ir.ReasonVendorExtension,
			Value:      raw,
			Provenance: locate(owner + ids.Ptr(name)),
		}
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// ExtensionSite is one object's x-* map paired with where it was written: Owner
// is the object's own source pointer, and Scope is what its entries key under on
// the carrier that ends up holding them (see ExtensionsUnder).
type ExtensionSite struct {
	Scope string
	Owner jsontext.Pointer
	Ext   *extensions.Extensions
}

// ExtensionsAt folds every site into one Unmodeled map, for the carriers that
// hold more than one object's extensions. Sites are applied in the order given,
// which orders their diagnostics; distinct scopes cannot collide, so no entry
// depends on it.
func ExtensionsAt(locate Locator, sites ...ExtensionSite) (ir.Unmodeled, []ir.Diagnostic) {
	var out ir.Unmodeled
	var diags []ir.Diagnostic
	for _, site := range sites {
		ext, extDiags := ExtensionsUnder(site.Ext, locate, site.Owner, site.Scope)
		out = MergeUnmodeled(out, ext)
		diags = append(diags, extDiags...)
	}
	return out, diags
}
