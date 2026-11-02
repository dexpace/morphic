// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// usageNotRoots are the types under which a named type is no usage root, each
// with the reason. A field of one of them names a type without putting a value
// of it on the wire.
var usageNotRoots = map[reflect.Type]string{
	reflect.TypeFor[ir.Value]():        "a value names the type it was written against",
	reflect.TypeFor[ir.Example]():      "an example illustrates a value; it carries none",
	reflect.TypeFor[ir.Availability](): "a type changed from is one an older version used",
	reflect.TypeFor[ir.PropPath]():     "a path locates a member inside a value another position carries",
	reflect.TypeFor[ir.ResourceInfo](): "a resource's identifiers and properties restate what its operations carry",
	reflect.TypeFor[ir.DocRef]():       "prose naming a type puts no value of it on the wire",
}

// usageNotRootFields are fields that name types and are no usage root, with the
// reason, where their type alone does not say so.
var usageNotRootFields = map[string]string{
	"Property.Args":     "field arguments belong to a model's properties, which the type graph reads; a header holds none",
	"Service.Renames":   "a rename presents a type; it carries no value of it",
	"Encoding.WireType": "a wire type says how a value is written, not a value it holds",
}

// usageRootPositions are the positions outside the registry that name a usage
// root, each a path of Type.Field steps from the document, past its operation
// groups, with the flags a client-started operation gives it. A message is
// sent, which TestIndex_UsageRootsCoverEveryPositionOfTheIR arranges.
func usageRootPositions() map[string]irfacts.UsageFlags {
	in, out, failure := irfacts.UsageInput, irfacts.UsageOutput, irfacts.UsageOutput|irfacts.UsageError
	positions := map[string]irfacts.UsageFlags{
		"Operation.Params/Parameter.Type":                        in,
		"Operation.RequestStream/StreamDetail.Events":            in,
		"Operation.RequestStream/StreamDetail.Initial":           in,
		"Operation.Bindings/OpBindings.RPC/RPCBinding.InputType": in,
		"Operation.Responses/Response.Headers/Property.Type":     out,
		"Operation.Default/Response.Headers/Property.Type":       out,
		"Operation.ResponseStream/StreamDetail.Events":           out,
		"Operation.ResponseStream/StreamDetail.Initial":          out,
		"Operation.LongRunning/LongRunning.PollingType":          out,
		"Operation.LongRunning/LongRunning.FinalType":            out,
		"Operation.Errors/ErrorCase.Headers/Property.Type":       failure,
		"Service.CommonErrors/ErrorCase.Headers/Property.Type":   failure,
		"Document.Messages/Message.Headers":                      in,
		"Document.Channels/Channel.Params/Parameter.Type":        in,
		"Document.Auth/AuthScheme.Credentials":                   in,
	}
	for header, flags := range map[string]irfacts.UsageFlags{
		"Operation.Responses/Response.Headers":   out,
		"Operation.Default/Response.Headers":     out,
		"Operation.Errors/ErrorCase.Headers":     failure,
		"Service.CommonErrors/ErrorCase.Headers": failure,
	} {
		positions[header+"/Property.Encoding/Encoding.Schema"] = flags
	}
	for payload, flags := range map[string]irfacts.UsageFlags{
		"Operation.Request":                      in,
		"Operation.Responses/Response.Payload":   out,
		"Operation.Default/Response.Payload":     out,
		"Operation.Errors/ErrorCase.Payload":     failure,
		"Service.CommonErrors/ErrorCase.Payload": failure,
		"Document.Messages/Message.Payload":      in,
	} {
		for _, content := range []string{
			"Content.Type",
			"Content.Item",
			"Content.File/FileInfo.Contents",
			"Content.ItemEncoding/PartEncoding.Headers/Property.Type",
			"Content.ItemEncoding/PartEncoding.Headers/Property.Encoding/Encoding.Schema",
			"Content.Encoding/PartEncoding.Headers/Property.Type",
			"Content.Encoding/PartEncoding.Headers/Property.Encoding/Encoding.Schema",
		} {
			positions[payload+"/Payload.Contents/"+content] = flags
		}
	}
	return positions
}

// position is one place a filled value names a type: its path of Type.Field
// steps, and why it is out of the classification, if it is.
type position struct {
	path, excluded string
}

// positionWalk records where a filled value names each type ID. A position
// under a type in types, or a field in fields, is excluded with the reason
// given. Steps in skip are left out of the path, as are those into a TypeRef's
// Target and into an embedded struct.
type positionWalk struct {
	types  map[reflect.Type]string
	fields map[string]string
	skip   map[string]bool
	out    map[ir.TypeID]position
}

// walk records the type IDs v holds, under path.
func (w *positionWalk) walk(v reflect.Value, path []string, excluded string) {
	if reason, ok := w.types[v.Type()]; ok && excluded == "" {
		excluded = reason
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			w.walk(v.Elem(), path, excluded)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			w.walk(v.Index(i), path, excluded)
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			w.walk(key, path, excluded)
			w.walk(v.MapIndex(key), path, excluded)
		}
	case reflect.Struct:
		w.fieldsOf(v, path, excluded)
	case reflect.String:
		if id := ir.TypeID(v.String()); v.Type() == typeIDReflect && id != "" {
			w.out[id] = position{path: strings.Join(path, "/"), excluded: excluded}
		}
	default:
		// Numbers, bools, interfaces (only the registry holds TypeDefs) name no type.
	}
}

// fieldsOf walks the exported fields of the struct v.
func (w *positionWalk) fieldsOf(v reflect.Value, path []string, excluded string) {
	for i := range v.NumField() {
		f := v.Type().Field(i)
		if !f.IsExported() {
			continue
		}
		step := v.Type().Name() + "." + f.Name
		fieldExcluded := excluded
		if reason, ok := w.fields[step]; ok && fieldExcluded == "" {
			fieldExcluded = reason
		}
		next := path
		if !w.skip[step] && !f.Anonymous && step != "TypeRef.Target" {
			next = append(slices.Clip(path), step)
		}
		w.walk(v.Field(i), next, fieldExcluded)
	}
}

// usagePositions returns where the filled document doc names each type ID.
// Steps through operation groups are left out of the path, since an operation
// is one root wherever it nests.
func usagePositions(doc *ir.Document) map[ir.TypeID]position {
	w := &positionWalk{
		types:  usageNotRoots,
		fields: usageNotRootFields,
		skip: map[string]bool{
			"Document.Services": true, "Service.Groups": true, "OperationGroup.Groups": true, "OperationGroup.Operations": true,
		},
		out: map[ir.TypeID]position{},
	}
	w.walk(reflect.ValueOf(doc).Elem(), nil, "")
	return w.out
}

// usageFilledDocument fills every field of a document outside its registry,
// each type ID distinct, and declares every ID it filled. Each operation sends
// the document's one message and names no callback, so that the flags of every
// root are the ones usageRootPositions gives.
func usageFilledDocument(t *testing.T) *ir.Document {
	t.Helper()
	doc := &ir.Document{}
	irtest.Fill(t, doc)
	doc.Types = ir.TypeRegistry{}
	require.Len(t, doc.Messages, 1)
	var message ir.MessageID
	for id := range doc.Messages {
		message = id
	}
	usageEachOperation(doc.Services, func(op *ir.Operation) {
		for i := range op.Bindings.HTTP {
			op.Bindings.HTTP[i].Callbacks = nil
		}
		op.Bindings.Message.Direction = ir.MsgDirectionSend
		op.Bindings.Message.Messages = []ir.MessageID{message}
	})
	return doc
}

// usageEachOperation calls fn with every operation of services, at any depth.
func usageEachOperation(services []ir.Service, fn func(*ir.Operation)) {
	var groups func([]ir.OperationGroup)
	groups = func(gs []ir.OperationGroup) {
		for i := range gs {
			for j := range gs[i].Operations {
				fn(&gs[i].Operations[j])
			}
			groups(gs[i].Groups)
		}
	}
	for i := range services {
		groups(services[i].Groups)
	}
}

// TestIndex_UsageRootsCoverEveryPositionOfTheIR fills every field of a document
// and asks the usage of each type it names. A position under a type or field
// usageNotRoots or usageNotRootFields names must have none; every other must be
// in usageRootPositions and have the flags it lists there. A field added to the
// IR therefore fails here until it is classified, and a root the derivation
// stops reading fails here too.
func TestIndex_UsageRootsCoverEveryPositionOfTheIR(t *testing.T) {
	t.Parallel()
	doc := usageFilledDocument(t)
	positions := usagePositions(doc)
	for id := range positions {
		doc.Types[id] = useType(id)
	}
	facts := irfacts.New(doc)
	roots := usageRootPositions()
	seen := map[string]bool{}
	for id, at := range positions {
		got := facts.Usage(id)
		if at.excluded != "" {
			assert.Zero(t, got, "%s is no root (%s)", at.path, at.excluded)
			continue
		}
		want, classified := roots[at.path]
		if !assert.True(t, classified, "%s names a type: list it in usageRootPositions or say why it is no root", at.path) {
			continue
		}
		seen[at.path] = true
		assert.Equal(t, want, got, "%s", at.path)
	}
	for path := range roots {
		assert.True(t, seen[path], "%s is listed as a root, but the IR has no such position", path)
	}
}
