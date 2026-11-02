// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"reflect"
	"slices"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/openapi/core"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// MediaTypes holds the reusable Media Type Objects OpenAPI 3.2 adds, which the
// parser models neither as a section nor as a reference
// (docs/parser-library.md#media-type-references): each components/mediaTypes
// entry of the source, and each content entry written as a $ref, modelled as a
// refscope.ReferencedMediaType. Every pass of this phase meets them as it meets
// the source's own reference objects (walkSource), so their references resolve
// and their findings are reported alike (GitHub #856). The zero value holds
// none.
type MediaTypes struct {
	// declared holds each components/mediaTypes entry of the source, in the
	// order the section is read, with the pointer naming it.
	declared []declaredMediaType
	// uses holds the model of each content entry written as a $ref, by the
	// mapping it is written as, in whichever document, as a walk meets one
	// (within): nil for one the parser cannot build a reference from, and
	// faults why it faulted building one it can.
	uses   map[*yaml.Node]*refscope.ReferencedMediaType
	faults map[*yaml.Node]error
	// canonical holds the model of each Media Type Object of the source that is
	// no reference, by the mapping it is written as, so a resolution ending on
	// one is handed it (adopt) rather than the copy the resolver built, whose
	// references no walk meets.
	canonical map[*yaml.Node]*refscope.ReferencedMediaType
	// build builds a model, buildMediaType outside a test.
	build mediaTypeBuild
}

// mediaTypeBuild builds the model of a Media Type Object or a Reference Object
// from the mapping it is written as, or says why it could not.
type mediaTypeBuild func(ctx context.Context, node *yaml.Node) (*refscope.ReferencedMediaType, error)

// declaredMediaType is a components/mediaTypes entry and the pointer naming it.
type declaredMediaType struct {
	pointer jsontext.Pointer
	model   *refscope.ReferencedMediaType
}

// mediaFault is what a walk yields in place of the model of a content entry
// written as a $ref that the parser faulted building, for the resolution pass
// to report where the entry is written (resolution.faulted).
type mediaFault struct {
	node *yaml.Node
	err  error
}

// Use returns the model of media, a content entry, when its mapping writes a
// $ref of its own, which is what makes the library read an object as a
// reference, and whether it does. The model is nil for one whose $ref holds no
// value, which the rule table reports, one the parser faulted on, which the
// resolution pass reports, and one no walk of this phase met, inside an object
// a $ref reaches in the source's raw YAML (GitHub #1320).
func (m MediaTypes) Use(media *soa.MediaType) (*refscope.ReferencedMediaType, bool) {
	node := nodeview.Deref(media.GetRootNode())
	if _, writes := ownRef(node); !writes {
		return nil, false
	}
	return m.uses[node], true
}

// modelMediaTypes models every components/mediaTypes entry of doc, read as the
// parser reads a section of its own, through `<<` merges, and returns an error
// diagnostic for each entry the parser faulted on. An entry that is no mapping,
// or whose $ref holds no scalar, is not modelled, as the rule table reports it,
// nor is one past the merge bound, which document.retainUnreferencedComponents
// reports.
func modelMediaTypes(ctx context.Context, doc *soa.OpenAPI, locate scan.Locator) (MediaTypes, []ir.Diagnostic) {
	return mediaTypesFrom(ctx, doc, locate, buildMediaType)
}

// mediaTypesFrom is modelMediaTypes with a build the caller supplies, so a test
// can hand it one that faults.
func mediaTypesFrom(ctx context.Context, doc *soa.OpenAPI, locate scan.Locator, build mediaTypeBuild,
) (MediaTypes, []ir.Diagnostic) {
	m := MediaTypes{uses: map[*yaml.Node]*refscope.ReferencedMediaType{}, faults: map[*yaml.Node]error{},
		canonical: map[*yaml.Node]*refscope.ReferencedMediaType{}, build: build}
	view := nodeview.New()
	components := nodeview.Deref(view.ChildByToken(nodeview.DocumentRoot(doc.GetRootNode()), "components"))
	var faults []ir.Diagnostic
	for _, entry := range view.MappingPairs(view.ChildByToken(components, "mediaTypes")) {
		node := nodeview.Deref(entry.Val)
		if ctx.Err() != nil {
			break
		}
		if node == nil || node.Kind != yaml.MappingNode || unreadableRef(node) {
			continue
		}
		model, err := build(ctx, node)
		if err != nil {
			faults = append(faults, diag.Newf(ir.SeverityError, diag.Validation, locate(node),
				"this media type could not be read: %s", err.Error()))
			continue
		}
		m.declared = append(m.declared, declaredMediaType{model: model,
			pointer: jsontext.Pointer("/components/mediaTypes").AppendToken(entry.Key)})
		if !model.IsReference() {
			m.canonical[node] = model
		}
	}
	return m, faults
}

// buildMediaType builds node, a Media Type Object or a Reference Object, as the
// parser builds a reference object of its own kinds. Nothing is validated: the
// rule table judges the source, and what a reference reaches is held to it
// (validateReached).
func buildMediaType(ctx context.Context, node *yaml.Node) (*refscope.ReferencedMediaType, error) {
	registerMediaTypeReference()
	model := &refscope.ReferencedMediaType{}
	err := runRecovered("parser", func() error {
		_, err := marshaller.UnmarshalNode(ctx, "", node, model)
		return err
	})
	return model, err
}

// registerMediaTypeReference tells the parser's marshaller of the core model of
// a media type reference, as the library tells it of its own kinds' when it is
// loaded. Unregistered, the marshaller builds the same field cache on first use
// and, in a test binary, logs that it did
// (docs/parser-library.md#media-type-references). Registering twice stores the
// same factory, so concurrent compiles may both do it.
func registerMediaTypeReference() {
	if marshaller.IsRegistered(reflect.TypeFor[core.Reference[*core.MediaType]]()) {
		return
	}
	marshaller.RegisterType(func() *core.Reference[*core.MediaType] { return &core.Reference[*core.MediaType]{} })
}

// ownRef returns the value of the $ref node writes among its own pairs, as
// the library tells a reference object (isRefKey), and whether it writes one.
func ownRef(node *yaml.Node) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if isRefKey(node.Content[i]) {
			return nodeview.Deref(node.Content[i+1]), true
		}
	}
	return nil, false
}

// unreadableRef reports whether node writes a $ref the parser cannot build a
// reference from: one holding no scalar, or null, on which it panics
// (docs/parser-library.md#recoverable-panic). The rule table reports that
// $ref, so the entry is left unmodelled rather than reported twice.
func unreadableRef(node *yaml.Node) bool {
	ref, writes := ownRef(node)
	return writes && (ref == nil || ref.Kind != yaml.ScalarNode || ref.ShortTag() == "!!null")
}

// followedByDeclared yields items, then each declared entry's walk, located at
// its pointer.
func (m MediaTypes) followedByDeclared(ctx context.Context, items iter.Seq[soa.WalkItem]) iter.Seq[soa.WalkItem] {
	return func(yield func(soa.WalkItem) bool) {
		for item := range items {
			if !yield(item) {
				return
			}
		}
		for _, d := range m.declared {
			for item := range locatedWalk(walk(ctx, d.model), d.pointer) {
				if !yield(item) {
					return
				}
			}
		}
	}
}

// within yields items and, after each Media Type Object among them, its model,
// at the same location, modelling it the first time any walk meets its mapping:
// a content entry is no reference object to the library, so no walk of its own
// yields one. One written as a $ref is resolved as one, and the model of any
// other is what a resolution ending on it is handed (adopt), and what load
// holds where the resolver looks for it (external.hold,
// resolution.holdMediaType), since the resolver cannot read the library's own
// model there as a reference. The zero MediaTypes yields items alone.
func (m MediaTypes) within(ctx context.Context, items iter.Seq[soa.WalkItem]) iter.Seq[soa.WalkItem] {
	if m.uses == nil {
		return items
	}
	return func(yield func(soa.WalkItem) bool) {
		var media *soa.MediaType
		match := soa.Matcher{MediaType: func(mt *soa.MediaType) error { media = mt; return nil }}
		for item := range items {
			if !yield(item) {
				return
			}
			media = nil
			// Match returns only what its matcher does, and this one never fails.
			_ = item.Match(match)
			if model := m.met(ctx, media); model != nil && !yield(modelItem(model, item.Location)) {
				return
			}
		}
	}
}

// met returns what a walk yields after media: its model, built the first time
// its mapping is met, which is the reference it is written as, or for any other
// media type the model wrapping it, noted as canonical. One whose $ref the
// parser cannot build a reference from (unreadableRef) yields nothing, and one
// the parser faults on the fault (mediaFault).
func (m MediaTypes) met(ctx context.Context, media *soa.MediaType) any {
	if media == nil {
		return nil
	}
	node := nodeview.Deref(media.GetRootNode())
	if node == nil {
		return nil
	}
	if _, writes := ownRef(node); !writes {
		model, seen := m.canonical[node]
		if !seen {
			model = &refscope.ReferencedMediaType{Object: media}
			m.canonical[node] = model
		}
		return model
	}
	if _, seen := m.uses[node]; !seen {
		m.buildUse(ctx, node)
	}
	if err, faulted := m.faults[node]; faulted {
		return mediaFault{node: node, err: err}
	}
	if use := m.uses[node]; use != nil {
		return use
	}
	return nil
}

// buildUse builds the model of node, a content entry written as a $ref, noting
// a fault rather than a model where the parser faults. It runs once a node.
func (m MediaTypes) buildUse(ctx context.Context, node *yaml.Node) {
	m.uses[node] = nil
	if unreadableRef(node) {
		return
	}
	model, err := m.build(ctx, node)
	if err != nil {
		m.faults[node] = err
		return
	}
	m.uses[node] = model
}

// adopt hands the reference whose chain c is the model this phase holds of the
// Media Type Object the chain ended on in the source, in place of the copy the
// resolver built from the same mapping, and reports whether it did. Only the
// model's references are met by a walk, and one declaration is then one
// object however many references reach it.
func (m MediaTypes) adopt(c chain) bool {
	if c.cut || c.stopped != "" || len(c.records) == 0 {
		return false
	}
	last := c.records[len(c.records)-1]
	if !last.inSource() || last.node == nil {
		return false
	}
	model, ok := m.canonical[nodeview.Deref(last.node)]
	if !ok || model == last.object {
		return false
	}
	return last.replace(model)
}

// modelItem is the walk item of model, a reference object the library's walk
// does not know, at loc. Its Match hands model to a matcher's Any alone: the
// library has no kind-specific field for it, and panics building a match for a
// type it does not know (docs/parser-library.md#media-type-references).
func modelItem(model any, loc soa.Locations) soa.WalkItem {
	return soa.WalkItem{Location: slices.Clip(loc), Match: func(mt soa.Matcher) error {
		if mt.Any == nil {
			return nil
		}
		return mt.Any(model)
	}}
}

// mediaTypeWalk is the walk of model: a reference alone, as the library walks
// one of its own kinds, or the library's walk of the object model holds, whose
// model a walk through within yields after its first item, once.
func mediaTypeWalk(ctx context.Context, model *refscope.ReferencedMediaType) iter.Seq[soa.WalkItem] {
	return func(yield func(soa.WalkItem) bool) {
		if model.Object == nil {
			yield(modelItem(model, nil))
			return
		}
		for item := range walk(ctx, model.Object) {
			if !yield(item) {
				return
			}
		}
	}
}
