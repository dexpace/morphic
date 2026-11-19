// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"slices"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/ir"
)

// partUnions remembers what a run's parts and items read of the types they
// name (lowering.Settled), so a union many name is read once a run (GitHub
// #1460): its members (unionMembers), the flags they give a part
// (partTypeFlags), the values an encoding applies to (partValues) and the
// default content type (defaultContentType). The deferred settlements of part
// keys and item defaults (partKeys.Settle, itemDefault.Settle) alone fill it.
// It reads a type's alias chain and its kind, a union's variants and a list's
// or tuple's elements, which no settlement rewrites: a fold, a completion,
// rewrites a model's members before every deferred one
// (compile.Types.QueueSettlementBeforeDeferredOnes).
type partUnions struct {
	members  map[ir.TypeID]unionRead
	flags    map[unionFlagsKey]unionFlags
	values   map[ir.TypeID]unionRead
	defaults map[defaultKey]defaultRead
}

// defaultKey is a default content type the store keeps: for a part declared
// of type of, or, where item is set, for the items of type of a sequential
// body holds, under rules, encodedBy saying what the declarations write of
// contentEncoding.
type defaultKey struct {
	rules     specrules.EncodingRules
	of        ir.TypeID
	encodedBy contentEncoding
	item      bool
}

// defaultRead is a default content type the store keeps, and whether the
// values it applies to were read.
type defaultRead struct {
	types []string
	ok    bool
}

// unionRead is a list of types read off a union, and whether it was read.
type unionRead struct {
	refs []ir.TypeRef
	ok   bool
}

// unionFlagsKey is a union whose flags a part reads, in a multipart body or
// not.
type unionFlagsKey struct {
	id        ir.TypeID
	multipart bool
}

// unionFlags is what partTypeFlags reads off a union's members.
type unionFlags struct {
	multi, file, decided bool
}

// newPartUnions returns a partUnions that has read nothing.
func newPartUnions() *partUnions {
	return &partUnions{
		members:  map[ir.TypeID]unionRead{},
		flags:    map[unionFlagsKey]unionFlags{},
		values:   map[ir.TypeID]unionRead{},
		defaults: map[defaultKey]defaultRead{},
	}
}

// partDefault returns the default content type of a part decl declares
// (defaultContentType over partValues), read once for each type, rules and
// encodedBy, and false where the values it applies to are not read. The list
// is shared, so a caller copies it before keeping it.
func (u *partUnions) partDefault(node func(ir.TypeID) (ir.TypeDef, bool), rules specrules.EncodingRules, decl *ir.Property, encodedBy contentEncoding) ([]string, bool) {
	key := defaultKey{rules: rules, of: decl.Type.Target, encodedBy: encodedBy}
	if defaultRead, read := u.defaults[key]; read {
		return defaultRead.types, defaultRead.ok
	}
	var defaultReadValue defaultRead
	if values, ok := partValues(u, node, []*ir.Property{decl}); ok {
		defaultReadValue = defaultRead{types: defaultContentType(u, node, rules, values, encodedBy), ok: true}
	}
	u.defaults[key] = defaultReadValue
	return defaultReadValue.types, defaultReadValue.ok
}

// itemDefault returns the default content type of the items of type item a
// sequential body holds, read once for each type, rules and encodedBy. The
// list is shared, so a caller copies it before keeping it.
func (u *partUnions) itemDefault(node func(ir.TypeID) (ir.TypeDef, bool), rules specrules.EncodingRules, item ir.TypeRef, encodedBy contentEncoding) []string {
	key := defaultKey{rules: rules, of: item.Target, encodedBy: encodedBy, item: true}
	if defaultRead, read := u.defaults[key]; read {
		return defaultRead.types
	}
	types := defaultContentType(u, node, rules, []ir.TypeRef{item}, encodedBy)
	u.defaults[key] = defaultRead{types: types, ok: true}
	return types
}

// partUnionsOf returns the run's partUnions, which every copy of serviceContext
// shares.
func partUnionsOf(serviceContext service.Ctx) *partUnions {
	return lowering.Settled(serviceContext.Ctx, newPartUnions)
}

// membersOf returns the members of union (unionMembers), read once.
func (u *partUnions) membersOf(node func(ir.TypeID) (ir.TypeDef, bool), union *ir.Union) ([]ir.TypeRef, bool) {
	if unionRead, read := u.members[union.ID]; read {
		return unionRead.refs, unionRead.ok
	}
	refs, ok := unionMembers(node, union)
	refs = slices.Clip(refs)
	u.members[union.ID] = unionRead{refs: refs, ok: ok}
	return refs, ok
}

// flagsOf returns the structural flags union's members give a part
// (partTypeFlags), read once for a multipart body and once for any other.
func (u *partUnions) flagsOf(node func(ir.TypeID) (ir.TypeDef, bool), union *ir.Union, multipart bool) (multi, file, decided bool) {
	key := unionFlagsKey{id: union.ID, multipart: multipart}
	if unionFlags, read := u.flags[key]; read {
		return unionFlags.multi, unionFlags.file, unionFlags.decided
	}
	members, ok := u.membersOf(node, union)
	if ok {
		file = multipart && len(members) > 0
		for _, member := range members {
			memberMulti, memberFile, _ := memberFlags(node, member, multipart)
			multi, file = multi || memberMulti, file && memberFile
		}
	}
	u.flags[key] = unionFlags{multi: multi, file: file, decided: ok}
	return multi, file, ok
}

// valuesOf returns the values an encoding of a part of type union applies to
// (partValues): each member's items, read once. The list is shared, so a
// caller reads it and never writes it.
func (u *partUnions) valuesOf(node func(ir.TypeID) (ir.TypeDef, bool), union *ir.Union) ([]ir.TypeRef, bool) {
	if unionRead, read := u.values[union.ID]; read {
		return unionRead.refs, unionRead.ok
	}
	members, ok := u.membersOf(node, union)
	var values []ir.TypeRef
	if ok {
		for _, typeRef := range members {
			values = append(values, itemsOf(node, typeRef)...)
		}
	}
	values = slices.Clip(values)
	u.values[union.ID] = unionRead{refs: values, ok: ok}
	return values, ok
}
