// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"errors"
	"iter"

	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// builtObject is an object landing no object could be built for, and why.
type builtObject struct {
	landing bind.ObjectLanding
	err     error
}

// errNothingThere is why no object is built at a landing whose document holds
// nothing at its position, which no table reports.
var errNothingThere = errors.New("the document holds nothing there")

// refKindOf returns the kind of object model, one of the parser's
// reference-or-inline wrappers or a ReferencedMediaType, stands for, and false
// for any other value, a schema included. It is the one place the load phase
// lists the wrappers.
func refKindOf(model any) (bind.RefKind, bool) {
	switch model.(type) {
	case *soa.ReferencedPathItem:
		return bind.RefPathItem, true
	case *soa.ReferencedParameter:
		return bind.RefParameter, true
	case *soa.ReferencedHeader:
		return bind.RefHeader, true
	case *soa.ReferencedRequestBody:
		return bind.RefRequestBody, true
	case *soa.ReferencedResponse:
		return bind.RefResponse, true
	case *soa.ReferencedCallback:
		return bind.RefCallback, true
	case *soa.ReferencedExample:
		return bind.RefExample, true
	case *soa.ReferencedLink:
		return bind.RefLink, true
	case *soa.ReferencedSecurityScheme:
		return bind.RefSecurityScheme, true
	case *refscope.ReferencedMediaType:
		return bind.RefMediaType, true
	default:
		return 0, false
	}
}

// buildObjects builds an object of its kind for each of the table's object
// landings, then for each position a reference object is written at or its
// chain ends on where bd holds no wrapper of that kind, as where a YAML alias
// places the fields beside a path item's $ref twice, which the walk models
// once (Siblings). It records each, and all a walk finds in it, where it is
// read (indexWalk), noting in faults each media type the parser faulted on. It
// returns each position it could build nothing for, with why, and stops once
// ctx is done.
func buildObjects(ctx context.Context, bd *typed.Builder, table *bind.Table, media MediaTypes, faults mediaFaults,
) []builtObject {
	o := objectBuild{ctx: ctx, bd: bd, table: table, media: media, faults: faults, view: nodeview.New(),
		tried: map[objectSite]bool{}}
	for _, l := range table.ObjectLandings() {
		if !o.build(l) {
			return o.failed
		}
	}
	for _, r := range table.References() {
		at := []bind.Position{r.Site}
		if r.Chain.Status == bind.Resolved {
			at = append(at, r.Chain.End)
		}
		for _, p := range at {
			if !o.build(bind.ObjectLanding{At: p, Kind: r.Kind, Name: table.Document(p.Doc).Name, Site: r.Site,
				Reached: true}) {
				return o.failed
			}
		}
	}
	return o.failed
}

// objectBuild is what buildObjects reads and records, and the positions it
// could build nothing for.
type objectBuild struct {
	ctx    context.Context
	bd     *typed.Builder
	table  *bind.Table
	media  MediaTypes
	faults mediaFaults
	view   *nodeview.View
	// tried holds each position and kind a build was tried at, so one that
	// failed is reported once, at the first site it was tried for.
	tried  map[objectSite]bool
	failed []builtObject
}

// objectSite is a position read as a kind of object.
type objectSite struct {
	at   bind.Position
	kind bind.RefKind
}

// build builds the object l stands for, unless one is held there or a build
// was tried there before, and reports whether the building goes on: false
// once ctx is done.
func (o *objectBuild) build(l bind.ObjectLanding) bool {
	key := objectSite{at: l.At, kind: l.Kind}
	if o.tried[key] || o.bd.HoldsObject(l.At, l.Kind) {
		return true
	}
	if o.ctx.Err() != nil {
		return false
	}
	o.tried[key] = true
	model, items, err := decodeObject(o.ctx, l.Kind, writtenIn(o.view, o.table, l.At))
	if err != nil {
		o.failed = append(o.failed, builtObject{landing: l, err: err})
		return true
	}
	o.bd.Object(model, l.Kind, l.At)
	indexWalk(o.bd, o.media.within(o.ctx, items), l.At, o.faults)
	return true
}

// decodeObject decodes node as a wrapper of kind, as the parser decodes one
// of its model, behind runRecovered: the parser panics on some shapes it
// accepts (docs/parser-library.md#recoverable-panic). It returns the wrapper
// and the walk over it, started at its own kind. What decoding finds wrong with
// the object is not kept: the load phase holds it to the rule table
// (validateReached).
func decodeObject(ctx context.Context, kind bind.RefKind, node *yaml.Node,
) (any, iter.Seq[soa.WalkItem], error) {
	if node == nil {
		return nil, nil, errNothingThere
	}
	switch kind {
	case bind.RefPathItem:
		m := &soa.ReferencedPathItem{}
		return m, walk(ctx, m), decoded(ctx, node, m)
	case bind.RefParameter:
		m := &soa.ReferencedParameter{}
		return m, walk(ctx, m), decoded(ctx, node, m)
	case bind.RefHeader:
		m := &soa.ReferencedHeader{}
		return m, walk(ctx, m), decoded(ctx, node, m)
	case bind.RefRequestBody:
		m := &soa.ReferencedRequestBody{}
		return m, walk(ctx, m), decoded(ctx, node, m)
	case bind.RefResponse:
		m := &soa.ReferencedResponse{}
		return m, walk(ctx, m), decoded(ctx, node, m)
	case bind.RefCallback:
		m := &soa.ReferencedCallback{}
		return m, walk(ctx, m), decoded(ctx, node, m)
	case bind.RefExample:
		m := &soa.ReferencedExample{}
		return m, walk(ctx, m), decoded(ctx, node, m)
	case bind.RefLink:
		m := &soa.ReferencedLink{}
		return m, walk(ctx, m), decoded(ctx, node, m)
	case bind.RefSecurityScheme:
		m := &soa.ReferencedSecurityScheme{}
		return m, walk(ctx, m), decoded(ctx, node, m)
	default:
		m, err := buildMediaType(ctx, node)
		return m, walk(ctx, m), err
	}
}

// decoded decodes node into m as the parser's resolver decodes what a
// reference names, behind runRecovered.
func decoded[C any](ctx context.Context, node *yaml.Node, m marshaller.CoreAccessor[C]) error {
	return runRecovered("parser", func() error {
		_, err := marshaller.UnmarshalNode(ctx, "reference", node, m)
		return err
	})
}
