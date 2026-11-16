// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"iter"
	"slices"

	"github.com/speakeasy-api/openapi/extensions"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
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
func ExtensionsFrom(ext *extensions.Extensions, doc provenance.Doc, owner jsontext.Pointer) (ir.Unmodeled, []ir.Diagnostic) {
	return ExtensionsUnder(ext, doc, owner, "")
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
func ExtensionsUnder(ext *extensions.Extensions, doc provenance.Doc, owner jsontext.Pointer, scope string) (ir.Unmodeled, []ir.Diagnostic) {
	if ext == nil || ext.Len() == 0 {
		return nil, nil
	}
	return lowerExtensions(ext.All(), doc, owner, scope)
}

// SchemaExtensions is ExtensionsUnder for the extensions a schema's statement
// lists, or one of the objects the schema holds.
func SchemaExtensions(list []statement.Extension, doc provenance.Doc, owner jsontext.Pointer, scope string) (ir.Unmodeled, []ir.Diagnostic) {
	if len(list) == 0 {
		return nil, nil
	}
	return lowerExtensions(extensionPairs(list), doc, owner, scope)
}

// extensionPairs yields each extension of list, its name and its value, in
// order.
func extensionPairs(list []statement.Extension) iter.Seq2[string, *yaml.Node] {
	return func(yield func(string, *yaml.Node) bool) {
		for _, e := range list {
			if !yield(e.Name, e.Node) {
				return
			}
		}
	}
}

// lowerExtensions lowers the extensions all yields, as ExtensionsUnder gives
// them, a diagnostic for each that does not serialize in the order all yields
// it.
func lowerExtensions(all iter.Seq2[string, *yaml.Node], doc provenance.Doc, owner jsontext.Pointer, scope string) (ir.Unmodeled, []ir.Diagnostic) {
	prefix := "openapi:"
	if scope != "" {
		prefix += scope + "/"
	}
	out := ir.Unmodeled{}
	var diags []ir.Diagnostic
	for name, node := range all {
		raw, err := RawFromNode(node)
		if err != nil || raw == nil {
			diags = append(diags, diag.Newf(ir.SeverityWarning, diag.DegradedConstruct,
				doc.At(owner), "extension %q could not be serialized", name))
			continue
		}
		out[prefix+name] = ir.UnmodeledEntry{
			Reason:     ir.ReasonVendorExtension,
			Value:      raw,
			Provenance: doc.At(owner + ids.Ptr(name)),
		}
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// ExtensionsNamed lowers the extensions names lists out of ext, keyed and valued
// as ExtensionsFrom gives them, for a reader of another object's keys: a carrier
// reading its $ref's referent. Such a reader reports nothing, so an entry has no
// provenance. Looking up each name leaves the rest of ext unserialized, which is
// every key of nearly every referent. A value that does not serialize is left
// out, as ExtensionsFrom reports it where it is written.
func ExtensionsNamed(ext *extensions.Extensions, names iter.Seq[string]) ir.Unmodeled {
	if ext == nil || ext.Len() == 0 {
		return nil
	}
	return extensionsNamed(ext.Get, names)
}

// SchemaExtensionsNamed is ExtensionsNamed for the extensions a schema's
// statement lists, which it scans once for each name: a policy reads a handful
// of keys, once for each schema (promotion.Promoter.Referent).
func SchemaExtensionsNamed(list []statement.Extension, names iter.Seq[string]) ir.Unmodeled {
	if len(list) == 0 {
		return nil
	}
	return extensionsNamed(func(name string) (*yaml.Node, bool) {
		i := slices.IndexFunc(list, func(e statement.Extension) bool { return e.Name == name })
		if i < 0 {
			return nil, false
		}
		return list[i].Node, true
	}, names)
}

// extensionsNamed lowers the extensions names lists that get finds, as
// ExtensionsNamed gives them.
func extensionsNamed(get func(name string) (*yaml.Node, bool), names iter.Seq[string]) ir.Unmodeled {
	var out ir.Unmodeled
	for name := range names {
		node, written := get(name)
		if !written {
			continue
		}
		raw, err := RawFromNode(node)
		if err != nil {
			continue
		}
		if out == nil {
			out = ir.Unmodeled{}
		}
		out["openapi:"+name] = ir.UnmodeledEntry{Reason: ir.ReasonVendorExtension, Value: raw}
	}
	return out
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
func ExtensionsAt(doc provenance.Doc, sites ...ExtensionSite) (ir.Unmodeled, []ir.Diagnostic) {
	var out ir.Unmodeled
	var diags []ir.Diagnostic
	for _, site := range sites {
		ext, extDiags := ExtensionsUnder(site.Ext, doc, site.Owner, site.Scope)
		out = MergeUnmodeled(out, ext)
		diags = append(diags, extDiags...)
	}
	return out, diags
}
