// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"maps"
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// maxComposingSchemas bounds the schemas declaredAgain reads at and above the
// one listing a property (styleguide bounded-everything rule): that one, and
// each composing it through the allOf lists a statement flattens, the outermost
// listing its entries past statement.MaxFlattenDepth but one.
const maxComposingSchemas = statement.MaxFlattenDepth + 2

// declaredAgain reports whether a model declares the property at at more than
// once itself: the schema listing it, or one composing that schema as an inline
// allOf entry, nested or not, lists its name more than once among its own
// properties and those of its inline entries (ownDeclarations). An entry past
// the depth the outermost flattens to is within an inner one's, so each up to
// the outermost is read.
//
// Such a member has one property for several declarations' notes, so each one's
// stay on the node its schema builds (notesOnCarrier).
func declaredAgain(loweringContext lowering.Ctx, at reftable.DocumentPointer) bool {
	if at.Pointer.Parent().LastToken() != "properties" {
		return false
	}
	name := at.Pointer.LastToken()
	lister := reftable.DocumentPointer{Doc: at.Doc, Pointer: at.Pointer.Parent().Parent()}
	composing := lister
	for range maxComposingSchemas {
		if slices.Contains(ownDeclarations(loweringContext, composing)[name], lister) {
			return true
		}
		if composing.Pointer.Parent().LastToken() != "allOf" {
			return false
		}
		composing.Pointer = composing.Pointer.Parent().Parent()
	}
	return false
}

// ownDeclarationsOf keys ownDeclarations' reading of the schema at a position.
type ownDeclarationsOf struct {
	at reftable.DocumentPointer
}

// ownDeclarations returns, for each name the model written at at declares more
// than once itself, where each schema listing it is: the model's own, and each
// inline entry its statement flattens, the keywords beside an entry's $ref
// among them. A $ref entry's target is another model's, and an allOf that
// refines composes no model, so it lowers one schema's properties alone
// (allOfComposes). Every property asks, so it is read once a run for each
// model.
func ownDeclarations(loweringContext lowering.Ctx, at reftable.DocumentPointer) map[string][]reftable.DocumentPointer {
	return lowering.ReadOncePerRun(loweringContext, ownDeclarationsOf{at}, func() map[string][]reftable.DocumentPointer {
		model := loweringContext.Statements().At(at)
		if !allOfComposes(loweringContext, model.Schema) {
			return nil
		}
		listing := []*statement.Schema{model.Schema}
		for _, entry := range model.Entries {
			if entry.Ref == nil {
				listing = append(listing, entry.Schema)
			}
		}
		listers := map[string][]reftable.DocumentPointer{}
		for _, schema := range listing {
			for _, property := range schema.ObjectKeywords().Properties {
				listers[property.Name] = append(listers[property.Name], schema.At)
			}
		}
		maps.DeleteFunc(listers, func(_ string, at []reftable.DocumentPointer) bool { return len(at) < 2 })
		return listers
	})
}
