// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// Flattening is the shape of a model across its composition, as
// [FlattenedProperties] computes it (docs/ir/models.md#the-flattening), and
// [NarrowedShapes] for each shape over a union: its properties, and which of
// them are required, redacted or deprecated, while each model's catch-all
// stays its own. It points into the registry it was computed from and copies
// no property, so it describes that registry as it stood; a caller that
// changes the registry flattens again.
type Flattening struct {
	// Properties are the members, one per wire name, each at the place the walk
	// first reads its wire name.
	Properties []FlatProperty
	// Undeclared are the names a Requires along the walk lists that no member
	// carries, once each, in the order the walk reads them: members only a
	// catch-all governs.
	Undeclared []string
	// Unflattened are the targets of the composition entries along the walk
	// that name no model once read through alias scalars, once each, in the
	// order the walk reads them, a variant read in a union's place as its
	// union writes it. What such a conjunct contributes is no property, so its
	// consumer lowers it.
	Unflattened []TypeID
}

// FlatProperty is one member of a [Flattening]: the declaration it takes, the
// other declarations of its wire name, whether an instance must carry it,
// whether its value is redacted, and whether it is deprecated.
type FlatProperty struct {
	// PropertyDecl is the declaration the member takes: the last of its wire
	// name the walk reads, so a model's declaration overrides that of every
	// model it composes that does not compose it in turn.
	PropertyDecl
	// Overridden are the member's other declarations, once each, in the order
	// the walk first reads them.
	Overridden []PropertyDecl
	// Deprecation is the member's deprecation, nil where none of its
	// declarations states one. Like Required, it accumulates over every
	// declaration; it points at the Deprecation of the last declaration the
	// walk reads that states one, the taken one's where it does.
	Deprecation *Deprecation
	// Required reports that an instance must carry the member: one of its
	// declarations is Required, or a model along the walk lists its wire name
	// in Requires.
	Required bool
	// Secret reports that the member's value is redacted: one of its
	// declarations is Secret, or is of a type that is Sensitive or whose Base
	// reaches one. Like Required, it accumulates over every declaration.
	Secret bool
}

// PropertyDecl is one property declaration and the model declaring it.
type PropertyDecl struct {
	// Owner is the model whose Properties hold Property.
	Owner TypeID
	// Property points at the declaration in Owner's Properties.
	Property *Property
}

// FlattenedProperties returns the shape of the model root names across its
// composition (docs/ir/models.md#the-flattening). Root and each composition
// entry are read through alias scalars. The walk goes depth first, following a
// model's Base, then Implements, then Mixins, and reads each model's own
// properties after those of every model it composes that does not compose it
// too: layout order. It reads the declarations [ExposedProps] reaches, one
// member per wire name, a later declaration overriding an earlier one in its
// place. A root naming no model has an empty shape; a cyclic composition is
// read to its fixpoint. It writes nothing to types.
func FlattenedProperties(types TypeRegistry, root TypeID) Flattening {
	f := flattener{types: types}
	return f.flatten(root)
}

// What a flattener's met holds for a type that is not a model on the walk's
// way: a model it has read, and an entry's target that names no model.
const (
	metRead        = -1
	metUnflattened = -2
)

// aliasOnChain is what a flattener's aliasOf holds for an alias scalar on the
// chain being read.
const aliasOnChain = -1

// flattener is the state of one [FlattenedProperties] walk.
type flattener struct {
	types TypeRegistry
	// met numbers each model the walk has met in the order it met them, until
	// it reads the model, and marks each entry target naming no model.
	met map[TypeID]int
	// finished are the models the walk is done composing but has yet to read,
	// in the order it finished them.
	finished []frame
	// members are the wire names read so far, in layout order, and index
	// places each name among them.
	members []member
	index   map[string]int
	// requires are the names a Requires lists, once each, in layout order.
	requires []string
	required map[string]bool
	// unflattened are the entry targets naming no model, in walk order.
	unflattened []TypeID
	// aliasOf places each alias scalar read through in aliasEnds, which holds
	// the type its chain stands for, so a chain is read once however many
	// entries name it.
	aliasOf   map[TypeID]int
	aliasEnds []TypeID
	// sensitivity holds, for each type a redaction is read through, whether
	// its Base reaches a Sensitive type, false while the type is on the chain
	// being read, so a chain is read once however many declarations name it.
	sensitivity map[TypeID]bool
	// steps counts what the walk reads: each entry and model end, alias
	// scalar, property and Requires name, and each type a redaction is read
	// off.
	steps int
	// narrow is what a [NarrowedShapes] walk reads beside the flattening, nil
	// in a [FlattenedProperties] walk.
	narrow *narrowState
	// query is the one member a [MemberReader] walk reads in place of every
	// member, nil in any other walk.
	query *memberQuery
}

// member is one wire name's declarations, in the order the walk reads them.
type member struct {
	reads []PropertyDecl
}

// frame is a model the walk has met: the next of its composition entries to
// follow, the number met gives it, its low-link as Tarjan's algorithm keeps
// it, equal to num only for the first model of a component the walk met, and
// how many models were finished when it was met.
type frame struct {
	id    TypeID
	m     *Model
	next  int
	num   int
	low   int
	since int
}

// flatten is [FlattenedProperties] over f's registry.
func (f *flattener) flatten(root TypeID) Flattening {
	if !f.read(root) {
		return Flattening{}
	}
	return f.result()
}

// read walks the model root names, read through alias scalars, and reports
// false where it names no model, which has no member to read.
func (f *flattener) read(root TypeID) bool {
	id := f.resolve(root)
	m, ok := f.types[id].(*Model)
	if !ok || m == nil {
		return false
	}
	if f.narrow != nil {
		f.narrow.root = id
	}
	f.walk(id, m)
	return true
}

// walk reads the model id names and every model it composes in layout order.
// It finds the composition's strongly connected components as Tarjan's
// algorithm does, and reads each when its first model finishes, so a model is
// read after every model it composes, unless that one composes it too; the
// models of one component are read in the order they finished. Each model is
// met once, so the walk follows at most every entry of the models it meets.
func (f *flattener) walk(id TypeID, m *Model) {
	f.met = map[TypeID]int{}
	f.index = map[string]int{}
	stack := []frame{f.meet(id, m)}
	for len(stack) > 0 {
		f.steps++
		top := &stack[len(stack)-1]
		if target, more := entry(top.m, top.next); more {
			top.next++
			if pid, pm, fresh := f.parent(target, top); fresh {
				stack = append(stack, f.meet(pid, pm))
			}
			continue
		}
		done := *top
		stack = stack[:len(stack)-1]
		if len(stack) > 0 {
			below := &stack[len(stack)-1]
			below.low = min(below.low, done.low)
		}
		f.finish(done)
	}
}

// meet numbers the model id names as the next the walk meets.
func (f *flattener) meet(id TypeID, m *Model) frame {
	num := len(f.met)
	f.met[id] = num
	return frame{id: id, m: m, num: num, low: num, since: len(f.finished)}
}

// finish records that the walk is done composing fr. When fr is the first
// model of its component the walk met, the component is every model finished
// since then and not yet read, and they are read now, in the order they
// finished.
func (f *flattener) finish(fr frame) {
	f.finished = append(f.finished, fr)
	if fr.low < fr.num {
		return
	}
	if f.narrow != nil && fr.num == 0 {
		f.narrow.readRoot(f.finished[fr.since:])
	}
	for _, c := range f.finished[fr.since:] {
		f.met[c.id] = metRead
		f.emit(c.id, c.m)
	}
	f.finished = f.finished[:fr.since]
}

// entry returns the target of m's i-th composition entry, counting its Base
// where it has one, then its Implements, then its Mixins, and false past the
// last.
func entry(m *Model, i int) (TypeID, bool) {
	if m.Base != nil {
		if i == 0 {
			return m.Base.Target, true
		}
		i--
	}
	if i < len(m.Implements) {
		return m.Implements[i].Target, true
	}
	i -= len(m.Implements)
	if i < len(m.Mixins) {
		return m.Mixins[i].Target, true
	}
	return "", false
}

// parent returns the model an entry of from naming target composes in, read
// through alias scalars and, in a [NarrowedShapes] walk, through the variant
// it reads in a union's place, and whether the walk is yet to meet it. One met
// and not yet read shares a component with from, which from's low then says.
// An entry naming no model is recorded as unflattened, as written or as the
// variant standing in its place writes it, the first time the walk reads it,
// and an unset one names nothing at all.
func (f *flattener) parent(target TypeID, from *frame) (TypeID, *Model, bool) {
	if target == "" {
		return "", nil, false
	}
	id := f.resolve(target)
	if f.narrow != nil {
		target, id = f.substitute(target, id)
	}
	m, ok := f.types[id].(*Model)
	if !ok || m == nil {
		if _, marked := f.met[target]; !marked {
			f.met[target] = metUnflattened
			f.unflattened = append(f.unflattened, target)
		}
		if f.narrow != nil {
			f.narrow.meet(f.types, id)
		}
		return "", nil, false
	}
	num, met := f.met[id]
	if !met {
		return id, m, true
	}
	if num != metRead {
		from.low = min(from.low, num)
	}
	return "", nil, false
}

// resolve returns the type id stands for: id itself unless it names an alias
// scalar, else the type at the end of its chain, or on a cyclic chain one of
// the cycle's scalars, which is no model either way. Each scalar is read once
// per walk.
func (f *flattener) resolve(id TypeID) TypeID {
	var path []TypeID
	cur := id
	for {
		f.steps++
		if at, seen := f.aliasOf[cur]; seen {
			if at != aliasOnChain {
				cur = f.aliasEnds[at]
			}
			break
		}
		base, alias := AliasBase(f.types, cur)
		if !alias {
			break
		}
		if f.aliasOf == nil {
			f.aliasOf = map[TypeID]int{}
		}
		f.aliasOf[cur] = aliasOnChain
		path = append(path, cur)
		cur = base
	}
	if len(path) > 0 {
		for _, s := range path {
			f.aliasOf[s] = len(f.aliasEnds)
		}
		f.aliasEnds = append(f.aliasEnds, cur)
	}
	return cur
}

// emit reads the model id names: each of its own properties, in source order,
// as a declaration of its wire name, and each name its Requires lists. A
// [MemberReader] walk reads the declaration of its one wire name alone.
func (f *flattener) emit(id TypeID, m *Model) {
	if f.query != nil {
		f.steps++
		f.query.emit(id, m)
		return
	}
	for i := range m.Properties {
		f.steps++
		d := PropertyDecl{Owner: id, Property: &m.Properties[i]}
		wire := d.Property.EffectiveWireName()
		at, known := f.index[wire]
		if !known {
			at = len(f.members)
			f.index[wire] = at
			f.members = append(f.members, member{})
		}
		f.members[at].reads = append(f.members[at].reads, d)
	}
	for _, name := range m.Requires {
		f.steps++
		if f.required[name] {
			continue
		}
		if f.required == nil {
			f.required = map[string]bool{}
		}
		f.required[name] = true
		f.requires = append(f.requires, name)
	}
}

// result assembles the walk's members, the Requires names no member declares,
// and the entries naming no model.
func (f *flattener) result() Flattening {
	out := Flattening{Unflattened: f.unflattened}
	if len(f.members) > 0 {
		out.Properties = make([]FlatProperty, len(f.members))
	}
	for i, mb := range f.members {
		out.Properties[i] = f.flat(mb)
	}
	for _, name := range f.requires {
		if _, declared := f.index[name]; !declared {
			out.Undeclared = append(out.Undeclared, name)
		}
	}
	return out
}

// flat returns the member as a FlatProperty: the declaration taken takes it,
// and every other is overridden, listed at its first reading.
func (f *flattener) flat(mb member) FlatProperty {
	last := taken(mb.reads)
	fp := FlatProperty{PropertyDecl: last, Required: f.required[last.Property.EffectiveWireName()]}
	var listed map[PropID]bool
	for _, d := range mb.reads {
		fp.Required = fp.Required || d.Property.Required
		fp.Secret = fp.Secret || d.Property.Secret || f.sensitive(d.Property.Type.Target)
		if d.Property.Deprecation != nil {
			fp.Deprecation = d.Property.Deprecation
		}
		if d.Property.ID == last.Property.ID || listed[d.Property.ID] {
			continue
		}
		if listed == nil {
			listed = map[PropID]bool{}
		}
		listed[d.Property.ID] = true
		fp.Overridden = append(fp.Overridden, d)
	}
	return fp
}

// taken returns the declaration a member takes of reads, its wire name's
// declarations in the order the walk reads them, which holds one at least: the
// last. It is the one rule [FlattenedProperties] and [MemberReader] take a
// declaration by, so a change to it changes both.
func taken(reads []PropertyDecl) PropertyDecl {
	return reads[len(reads)-1]
}

// sensitive reports whether id names a type that is Sensitive, or whose Base
// reaches one, a scalar's or a model's (docs/ir/type-graph.md). Each type
// whose Base it follows is read once per walk, so a chain many declarations
// name is read once, and a cycle the IR permits ends at a type already on the
// chain.
func (f *flattener) sensitive(id TypeID) bool {
	var chain []TypeID
	var found bool
	for {
		f.steps++
		if known, read := f.sensitivity[id]; read {
			found = known
			break
		}

		td := f.types[id]
		if IsNilTypeDef(td) {
			break
		}
		if td.Common().Sensitive {
			found = true
			break
		}

		var base *TypeRef
		switch t := td.(type) {
		case *Scalar:
			base = t.Base
		case *Model:
			base = t.Base
		default:
			// No other kind has a Base to inherit through.
		}
		if base == nil {
			break
		}

		if f.sensitivity == nil {
			f.sensitivity = map[TypeID]bool{}
		}
		f.sensitivity[id] = false
		chain = append(chain, id)
		id = base.Target
	}

	for _, c := range chain {
		f.sensitivity[c] = found
	}

	return found
}
