// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
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
	composing := reftable.DocumentPointer{Doc: at.Doc, Pointer: at.Pointer.Parent().Parent()}
	for range maxComposingSchemas {
		if ownDeclarations(loweringContext, composing)[name] > 1 {
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

// ownDeclarations returns how many times the model written at at declares each
// name itself, in its own properties and those of each inline entry its
// statement flattens, the keywords beside an entry's $ref among them. A $ref
// entry's target is another model's, and an allOf that refines composes no
// model, so it lowers one schema's properties alone (allOfComposes). Every
// property asks, so it is read once a run for each model.
func ownDeclarations(loweringContext lowering.Ctx, at reftable.DocumentPointer) map[string]int {
	return lowering.ReadOncePerRun(loweringContext, ownDeclarationsOf{at}, func() map[string]int {
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
		declared := map[string]int{}
		for _, schema := range listing {
			for _, property := range schema.ObjectKeywords().Properties {
				declared[property.Name]++
			}
		}
		return declared
	})
}
