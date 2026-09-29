package irverify

import (
	"fmt"
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// idTypes are the IR's classes of identity. TestIDTypes_MatchTheIdentityClasses
// holds this set to identityClasses, which classifies every named string type
// the ir sources declare.
var idTypes = map[reflect.Type]bool{
	reflect.TypeFor[ir.TypeID]():    true,
	reflect.TypeFor[ir.OpID]():      true,
	reflect.TypeFor[ir.ChannelID](): true,
	reflect.TypeFor[ir.MessageID](): true,
	reflect.TypeFor[ir.AuthID]():    true,
	reflect.TypeFor[ir.ServiceID](): true,
	reflect.TypeFor[ir.PropID]():    true,
}

// idRule is what an empty value means at one ID-typed position.
type idRule int

const (
	// idRequired is the rule for a reference: it names an entity, so an empty
	// one names nothing and was left by a lowering that dropped what it meant
	// to add. It is the zero value, so a position idPositions does not list is
	// held to it rather than let through.
	idRequired idRule = iota
	// idOptional is a position whose comment documents an empty value as its
	// own spelling of "none".
	idOptional
	// idLocator is Discriminator.Property: empty is legitimate when
	// PropertyName or Index locates the tag instead.
	idLocator
	// idElsewhere is a position another check holds: an entity's own ID and a
	// registry's keys (checkRegistryKeys, checkDeclaredIDs), and TypeRef.Target
	// (checkTypeRefs).
	idElsewhere
)

// idPositions records the decision for every ID-typed field the ir sources
// declare, keyed "Type.Field". TestIDPositions_AreAllClassified fails when a
// field is added without one, and the rule a field's comment states is the one
// listed here.
var idPositions = map[string]idRule{
	"AuthScheme.ID":     idElsewhere,
	"Channel.ID":        idElsewhere,
	"Message.ID":        idElsewhere,
	"Operation.ID":      idElsewhere,
	"Property.ID":       idElsewhere,
	"Service.ID":        idElsewhere,
	"TypeCommon.ID":     idElsewhere,
	"Document.Types":    idElsewhere,
	"Document.Channels": idElsewhere,
	"Document.Messages": idElsewhere,
	"Document.Auth":     idElsewhere,
	"TypeRef.Target":    idElsewhere,

	"Discriminator.Default":  idOptional,
	"Discriminator.Property": idLocator,

	"Callback.Operations":          idRequired,
	"Channel.Messages":             idRequired,
	"Content.Encoding":             idRequired,
	"CtorValue.Scalar":             idRequired,
	"Discriminator.Mapping":        idRequired,
	"HTTPParamBinding.BodyPath":    idRequired,
	"HTTPParamBinding.ParamPath":   idRequired,
	"LongRunning.FinalOperation":   idRequired,
	"LongRunning.PollingOperation": idRequired,
	"MessageBinding.Channel":       idRequired,
	"MessageBinding.Messages":      idRequired,
	"OTPBinding.Process":           idRequired,
	"Operation.OverloadOf":         idRequired,
	"ParamPath.Segments":           idRequired,
	"PropPath.Segments":            idRequired,
	"Reply.Channel":                idRequired,
	"Reply.Messages":               idRequired,
	"ResourceInfo.CollectionOps":   idRequired,
	"ResourceInfo.InstanceOps":     idRequired,
	"ResourceInfo.Lifecycle":       idRequired,
	"SchemeUse.Scheme":             idRequired,
	"Service.Extends":              idRequired,
	"Service.Renames":              idRequired,
	"ValueRef.Type":                idRequired,
}

// idShape is how a field holds its IDs.
type idShape int

const (
	idScalar   idShape = iota // an ID held directly
	idPointer                 // *ID: nil is the absent reference
	idSlice                   // []ID: every element a reference
	idMapKey                  // map[ID]V: every key a reference
	idMapValue                // map[K]ID: every value a reference
)

// idField is one ID-typed field of a struct type, with its rule.
type idField struct {
	index int
	name  string
	shape idShape
	noun  string
	rule  idRule
}

// checkEmptyRefs asserts no reference is empty where its position requires one.
//
// collectRefs skips an empty ID because it is no reference, and that is right;
// but it left the positions that require one reported by nothing (GitHub #473),
// the state the empty union variant was in before checkTypeRefs (GitHub #397).
// Unlike TypeRef, the answer cannot be keyed by type: Discriminator.Default is
// a TypeID whose empty value means "none". So it is per position, recorded in
// idPositions and on each field's comment, and every position not documented
// as admitting an empty value is held to naming something.
//
// Positions are found by reflection over each struct type the walk reaches,
// once per type per run, so every shape is covered — a field, a pointer, a slice
// element, a map key and a map value. Paths spell each one as the walk would.
func checkEmptyRefs(doc *ir.Document, _ declarations) ([]Violation, bool) {
	var vs []Violation
	fields := map[reflect.Type][]idField{}
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct {
			return true
		}
		byType, seen := fields[v.Type()]
		if !seen {
			byType = idFieldsOf(v.Type())
			fields[v.Type()] = byType
		}
		for _, f := range byType {
			vs = appendEmptyRefs(vs, v, f, path)
		}
		return true
	})
	return vs, truncated
}

// idFieldsOf returns t's ID-typed fields that some rule here holds.
func idFieldsOf(t reflect.Type) []idField {
	var out []idField
	for i := range t.NumField() {
		sf := t.Field(i)
		shape, idType, isID := idShapeOf(sf.Type)
		rule := idPositions[t.Name()+"."+sf.Name]
		if !isID || rule == idElsewhere {
			continue
		}
		out = append(out, idField{index: i, name: sf.Name, shape: shape, noun: ir.RefNoun(idType), rule: rule})
	}
	return out
}

// idShapeOf classifies a field type as one of the shapes that hold IDs.
func idShapeOf(t reflect.Type) (idShape, reflect.Type, bool) {
	switch {
	case idTypes[t]:
		return idScalar, t, true
	case t.Kind() == reflect.Pointer && idTypes[t.Elem()]:
		return idPointer, t.Elem(), true
	case t.Kind() == reflect.Slice && idTypes[t.Elem()]:
		return idSlice, t.Elem(), true
	case t.Kind() == reflect.Map && idTypes[t.Key()]:
		return idMapKey, t.Key(), true
	case t.Kind() == reflect.Map && idTypes[t.Elem()]:
		return idMapValue, t.Elem(), true
	default:
		return 0, nil, false
	}
}

// appendEmptyRefs reports each empty ID one field of owner holds.
func appendEmptyRefs(vs []Violation, owner reflect.Value, f idField, path string) []Violation {
	at := path + "." + f.name
	v := owner.Field(f.index)
	switch f.shape {
	case idScalar:
		if v.String() == "" && !emptyAllowed(owner, f.rule) {
			vs = append(vs, emptyRef(f, at))
		}
	case idPointer:
		if !v.IsNil() && v.Elem().String() == "" {
			vs = append(vs, emptyRef(f, at))
		}
	case idSlice:
		for i := range v.Len() {
			if v.Index(i).String() == "" {
				vs = append(vs, emptyRef(f, fmt.Sprintf("%s[%d]", at, i)))
			}
		}
	case idMapKey:
		if v.MapIndex(reflect.Zero(v.Type().Key())).IsValid() {
			vs = append(vs, emptyRef(f, at+"[]"+ir.MapKeySuffix))
		}
	case idMapValue:
		for iter := v.MapRange(); iter.Next(); {
			if iter.Value().String() == "" {
				vs = append(vs, emptyRef(f, fmt.Sprintf("%s[%v]", at, iter.Key())))
			}
		}
	}
	return vs
}

// emptyAllowed reports whether an empty scalar at a position under rule is the
// position's documented "none".
func emptyAllowed(owner reflect.Value, rule idRule) bool {
	switch rule {
	case idOptional:
		return true
	case idLocator:
		return owner.FieldByName("PropertyName").String() != "" || !owner.FieldByName("Index").IsNil()
	default:
		return false
	}
}

// emptyRef is the violation for one empty reference. The locator position gets
// its own message, because what it lacks is a way to find the tag rather than
// a property in particular.
func emptyRef(f idField, at string) Violation {
	msg := f.noun + " reference is empty, and this position has no empty form"
	if f.rule == idLocator {
		msg = "discriminator names no tag property, and neither PropertyName nor Index locates the tag instead"
	}
	return Violation{Code: "ir/empty-" + f.noun + "-ref", Message: msg, Path: at}
}
