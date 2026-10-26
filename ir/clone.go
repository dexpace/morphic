package ir

import "reflect"

// cloneTypeDef returns a deep copy of td, which must not be nil, that shares no
// pointer, slice or map with it, so changing the copy leaves td as it was. It
// reports false for a td nesting past [MaxWalkDepth] levels, where the copy
// stops. Map entries are read into ranging.
//
// The copy is by reflection, as [WalkValues] is, so a field added to the IR is
// copied the moment it exists. Aliasing is kept: a pointer, a map, or a slice
// of one length td reaches twice is copied once, and a cycle comes out as the
// same cycle.
func cloneTypeDef(td TypeDef, ranging *entryStack) (TypeDef, bool) {
	c := cloner{copies: map[refKey]reflect.Value{}, ranging: ranging}
	v := reflect.ValueOf(td)
	out := reflect.New(v.Type()).Elem()
	c.fill(out, v, 0)
	if c.truncated {
		return nil, false
	}
	var cloned TypeDef
	reflect.ValueOf(&cloned).Elem().Set(out) // out has td's concrete type
	return cloned, true
}

// cloner is one deep copy: the copy made of each reference it has met, keyed
// as the walk keys one, the stack holding the entries of the maps it is
// copying, and whether the depth cap cut it short.
//
// The stack is held by pointer: escape analysis does not tell fields apart, so
// a stack inside the cloner would move copies onto the heap with it.
type cloner struct {
	copies    map[refKey]reflect.Value
	ranging   *entryStack
	truncated bool
}

// fill sets dst, a settable value of v's type, to a copy of v sharing no
// reference with it. depth counts the steps from the root as [WalkValues]
// counts them, and maps are copied in the walk's key order, so which path
// copies a shared reference, and so whether the cap cuts a copy, is fixed.
//
// No interface sits below a TypeDef, which TestEditor_CopiesEveryIRTypeWhole
// holds the IR to, so one would be shared rather than copied.
func (c *cloner) fill(dst, v reflect.Value, depth int) {
	if depth > MaxWalkDepth {
		c.truncated = true
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		c.pointer(dst, v, depth)
	case reflect.Struct:
		c.fields(dst, v, depth)
	case reflect.Slice:
		c.slice(dst, v, depth)
	case reflect.Map:
		c.entries(dst, v, depth)
	default:
		dst.Set(v) // a value of any other kind the IR declares holds no reference
	}
}

// pointer copies what a non-nil pointer points at, once per pointer. The copy
// is recorded before its target is filled in, so a cycle back to it ends.
func (c *cloner) pointer(dst, v reflect.Value, depth int) {
	if v.IsNil() {
		dst.Set(v)
		return
	}
	key := refKey{typ: v.Type(), ptr: v.Pointer()}
	if made, met := c.copies[key]; met {
		dst.Set(made)
		return
	}
	out := reflect.New(v.Type().Elem())
	c.copies[key] = out
	dst.Set(out)
	c.fill(out.Elem(), v.Elem(), depth+1)
}

// fields copies a struct. Assigning it whole copies every field holding no
// reference, so only the others are filled again. Every field can be set,
// since TestEditor_CopiesEveryIRTypeWhole holds the IR to exporting each.
func (c *cloner) fields(dst, v reflect.Value, depth int) {
	dst.Set(v)
	// Indexed: ranging over v.Fields() puts the loop body on the heap for every
	// struct copied.
	for i := range v.NumField() {
		if f := dst.Field(i); holdsReference(f.Kind()) {
			c.fill(f, v.Field(i), depth+1)
		}
	}
}

// slice copies a non-nil slice into a new backing array of its length, once
// per slice the walk would key as one.
func (c *cloner) slice(dst, v reflect.Value, depth int) {
	if v.IsNil() {
		dst.Set(v)
		return
	}
	key := refKey{typ: v.Type(), ptr: v.Pointer(), n: v.Len()}
	if made, met := c.copies[key]; met {
		dst.Set(made)
		return
	}
	out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
	c.copies[key] = out
	dst.Set(out)
	if !holdsReference(v.Type().Elem().Kind()) {
		reflect.Copy(out, v)
		return
	}
	for i := range v.Len() {
		c.fill(out.Index(i), v.Index(i), depth+1)
	}
}

// entries copies a non-nil map, once per map. A key is kept as it is, since
// every key type the IR declares is a string or an integer. Each value is
// filled into one holder, which SetMapIndex copies into the map: fill
// overwrites it whole, unless the depth cap cuts the copy, which is then
// discarded.
func (c *cloner) entries(dst, v reflect.Value, depth int) {
	if v.IsNil() {
		dst.Set(v)
		return
	}
	key := refKey{typ: v.Type(), ptr: v.Pointer()}
	if made, met := c.copies[key]; met {
		dst.Set(made)
		return
	}
	out := reflect.MakeMapWithSize(v.Type(), v.Len())
	c.copies[key] = out
	dst.Set(out)
	f := c.ranging.push(v)
	value := reflect.New(v.Type().Elem()).Elem()
	for i := f.base; i < f.end; i++ {
		// Indexed afresh: a nested map can move the stack while stacking its own.
		e := c.ranging.entries[i]
		c.fill(value, e.value, depth+1)
		out.SetMapIndex(e.key, value)
	}
	c.ranging.pop(f)
}

// holdsReference reports whether a value of kind k can hold a pointer, slice or
// map, and so has to be filled rather than assigned.
func holdsReference(k reflect.Kind) bool {
	switch k {
	case reflect.Pointer, reflect.Struct, reflect.Slice, reflect.Map:
		return true
	default:
		return false
	}
}
