// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"slices"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/scratchstate"
)

// position is a place where a type, bounds and a value meet: a property, a
// parameter or a header. Each check of what a source says about a value reads
// all of them alike, so they are read through one list.
type position struct {
	// node is the ID a finding at the position is placed at.
	node string
	// from is the provenance of the position's entity.
	from ir.Provenance
	typ  ir.TypeRef
	own  *ir.Constraints
	// encoded says the value is written in an encoding of its own, so its kind
	// says nothing of what the type holds.
	encoded bool
	// mediaTyped says a media type serializes the position, so an example is in
	// its format. Its default is a value of the type all the same.
	mediaTyped bool
	// writtenShapes are the structured shapes the position's style can write
	// as text, so a scalar example of one of them may be that value written
	// out, as writtenOut judges.
	// Its default is a value of the type all the same. An example does not say
	// which form it is in, so a string written as the value's data goes
	// unjudged (docs/ir/values.md#values-held-to-their-type).
	writtenShapes shapeSet
	// nullExampleLeftOut says the position is a parameter or a header that is
	// not required and that HTTP alone writes in a style, which has no null to
	// write, so a null example is the value left out, not a null sent. A media
	// type, the body and another protocol may write a null, so it is judged
	// there, every media type alike. Its default is a value of the type all the
	// same (docs/ir/values.md#values-held-to-their-type).
	nullExampleLeftOut bool
	def                *ir.Value
	examples           []ir.Example
}

// positionScratch is what eachPosition reads a document through, emptied
// between operations: a content's part keys in sorted order, and how the HTTP
// bindings of one operation write each parameter they write whole.
type positionScratch struct {
	keys  []ir.PropertyID
	whole scratchstate.Map[ir.ParameterID, wholeWriting]
}

// wholeWriting is how an operation's HTTP bindings write one parameter whole:
// the structured shapes their styles can write as text, whether a content
// type serializes it, and whether a binding writes it into the body.
type wholeWriting struct {
	shapes     shapeSet
	mediaTyped bool
	inBody     bool
}

// eachPosition calls visit with each property of a live model, in liveTypeIDs
// order, then each header of a service's common errors, then each parameter and
// header of each operation as ir.ForEachOperation visits them: its parameters,
// the headers of its responses, its default and its errors, and last the
// headers of a content's parts, the parts in sorted order, read through
// scratch, which each operation empties. visit must not keep its argument.
//
// Not read: Property.Args, which is GraphQL's, and a resource's properties,
// which no compiler writes (#1413). A stop of ir.ForEachOperation is the one
// Validate reports for each check that visits operations.
func eachPosition(doc *ir.Document, live []ir.TypeID, scratch *positionScratch, visit func(*position)) {
	var positionValue position
	for _, id := range live {
		model, ok := doc.Types[id].(*ir.Model)
		if !ok {
			continue
		}
		for i := range model.Properties {
			visit(propertyPosition(&positionValue, &model.Properties[i], string(id)))
		}
	}
	for i := range doc.Services {
		for j := range doc.Services[i].CommonErrors {
			headerPositions(&positionValue, doc.Services[i].CommonErrors[j].Headers, string(doc.Services[i].ID), visit)
		}
	}
	ir.ForEachOperation(doc, func(operation ir.Operation) { operationPositions(&positionValue, &operation, scratch, visit) })
}

// propertyPosition fills positionValue from pr, a property of owner, which
// stands in for the property's own ID where that is empty.
func propertyPosition(positionValue *position, pr *ir.Property, owner string) *position {
	node := string(pr.ID)
	if node == "" {
		node = owner
	}
	mediaTyped := mediaTypeAlone(pr.Encoding)
	*positionValue = position{
		node: node, from: pr.Provenance, typ: pr.Type, own: pr.Constraints,
		encoded: pr.Encoding != nil && !mediaTyped, mediaTyped: mediaTyped, def: pr.Default, examples: pr.Examples,
	}
	return positionValue
}

// mediaTypeAlone reports whether enc names a media type and nothing else, as
// the Encoding of a header declared by content does: a media type serializes
// the value, so its examples are in that format, while its default is a value
// of the type, which an encoding of another kind may write another way.
func mediaTypeAlone(enc *ir.Encoding) bool {
	return enc != nil && enc.MediaType != "" && *enc == ir.Encoding{MediaType: enc.MediaType}
}

// headerPositions visits each of headers, which belong to owner. A header
// stating no style is written in simple's, unless an encoding or a media type
// serializes it, which encoded or mediaTyped says already.
func headerPositions(positionValue *position, headers []ir.Header, owner string, visit func(*position)) {
	for i := range headers {
		header := &headers[i]
		propertyPosition(positionValue, &header.Property, owner)
		style := header.Style
		if style == "" {
			style = "simple"
		}
		positionValue.writtenShapes = shapesWritten(style, header.Explode)
		positionValue.nullExampleLeftOut = !header.Required && !positionValue.mediaTyped
		visit(positionValue)
	}
}

// shapeSet is a set of the structured shapes, shapeList and shapeObject.
type shapeSet uint8

const (
	shapeSetList shapeSet = 1 << iota
	shapeSetObject
)

// has reports whether sh is a shape of the set.
func (set shapeSet) has(sh shape) bool {
	switch sh {
	case shapeList:
		return set&shapeSetList != 0
	case shapeObject:
		return set&shapeSetObject != 0
	default:
		return false
	}
}

// shapesWritten returns the structured shapes a value written in style,
// exploded as explode says, may be: OpenAPI's style table gives a list and an
// object a text under every style it defines, but a list none under
// deepObject, whatever explode says, and neither under a delimited style that
// explodes, which it does not by default. A style the table does not define
// writes neither (docs/ir/values.md#values-held-to-their-type).
func shapesWritten(style string, explode *bool) shapeSet {
	switch style {
	case "simple", "form", "label", "matrix", "cookie":
		return shapeSetList | shapeSetObject
	case "spaceDelimited", "pipeDelimited":
		if explode == nil || !*explode {
			return shapeSetList | shapeSetObject
		}
		return 0
	case "deepObject":
		return shapeSetObject
	default:
		return 0
	}
}

// wholeParameters returns how the HTTP bindings of operation write each
// parameter a binding writes whole, rather than a member of, in the emptied map
// whole holds. What any binding writes counts: the shapes of every style, and a
// content type or the body whichever binding states it.
func wholeParameters(operation *ir.Operation, whole *scratchstate.Map[ir.ParameterID, wholeWriting]) map[ir.ParameterID]wholeWriting {
	written := whole.Emptied()
	for i := range operation.Bindings.HTTP {
		for j := range operation.Bindings.HTTP[i].ParamBindings {
			pb := &operation.Bindings.HTTP[i].ParamBindings[j]
			if len(pb.ParamPath) != 0 {
				continue
			}
			writing := written[pb.Param]
			writing.shapes |= shapesWritten(pb.Style, pb.Explode)
			writing.mediaTyped = writing.mediaTyped || pb.ContentType != ""
			writing.inBody = writing.inBody || pb.Location == ir.HTTPLocationBody || pb.Location == ir.HTTPLocationBodyProperty
			written[pb.Param] = writing
		}
	}
	return written
}

// operationPositions visits the positions of operation. A parameter is
// serialized by a media type where its own Encoding names one alone or a
// binding's content type writes it whole, as a header declared by content is.
// Its own Encoding naming anything else leaves its values judged as an
// unencoded parameter's are, until #2178 decides which checks an encoding
// excuses (docs/ir/values.md#values-held-to-their-type).
// A binding of another protocol than HTTP may write a null, so it keeps a null
// example of a parameter judged.
func operationPositions(positionValue *position, operation *ir.Operation, scratch *positionScratch, visit func(*position)) {
	whole := wholeParameters(operation, &scratch.whole)
	httpAlone := operation.Bindings.RPC == nil && operation.Bindings.Message == nil &&
		operation.Bindings.GraphQL == nil && operation.Bindings.OTP == nil
	for i := range operation.Params {
		pm := &operation.Params[i]
		writing := whole[pm.ID]
		mediaTyped := mediaTypeAlone(pm.Encoding) || writing.mediaTyped
		*positionValue = position{
			node: string(pm.ID), from: pm.Provenance, typ: pm.Type, own: pm.Constraints,
			mediaTyped: mediaTyped, writtenShapes: writing.shapes, def: pm.Default, examples: pm.Examples,
			nullExampleLeftOut: !pm.Required && !mediaTyped && !writing.inBody && httpAlone,
		}
		visit(positionValue)
	}
	owner := string(operation.ID)
	for i := range operation.Responses {
		headerPositions(positionValue, operation.Responses[i].Headers, owner, visit)
	}
	if operation.Default != nil {
		headerPositions(positionValue, operation.Default.Headers, owner, visit)
	}
	for i := range operation.Errors {
		headerPositions(positionValue, operation.Errors[i].Headers, owner, visit)
	}
	partPositions(positionValue, operation.Request, owner, &scratch.keys, visit)
	for i := range operation.Responses {
		partPositions(positionValue, operation.Responses[i].Payload, owner, &scratch.keys, visit)
	}
	if operation.Default != nil {
		partPositions(positionValue, operation.Default.Payload, owner, &scratch.keys, visit)
	}
	for i := range operation.Errors {
		partPositions(positionValue, operation.Errors[i].Payload, owner, &scratch.keys, visit)
	}
}

// partPositions visits the headers of the parts of each content of payload: the
// item's, then each part's, in the order of its sorted keys.
func partPositions(positionValue *position, payload *ir.Payload, owner string, keys *[]ir.PropertyID, visit func(*position)) {
	if payload == nil {
		return
	}
	for i := range payload.Contents {
		content := &payload.Contents[i]
		if content.ItemEncoding != nil {
			headerPositions(positionValue, content.ItemEncoding.Headers, owner, visit)
		}
		*keys = (*keys)[:0]
		for propertyID := range content.Encoding {
			*keys = append(*keys, propertyID)
		}
		slices.Sort(*keys)
		for _, propertyID := range *keys {
			part := content.Encoding[propertyID]
			headerPositions(positionValue, part.Headers, owner, visit)
		}
	}
}
