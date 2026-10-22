package resolve

import (
	"encoding/json/jsontext"
)

// Reached records the positions in the source a lowering resolved a reference
// to, each hop of a chain included: the declarations the IR carries through
// their uses rather than on a node of their own (GitHub #616).
//
// No lowering reads it. The compiler asks it once the last lowering has
// returned, so what it holds cannot change a lowering's answer, and as a set it
// cannot depend on the order lowerings ran in. That is why sharing it between
// copies of a context is safe, as sharing a memo is.
type Reached struct {
	at map[jsontext.Pointer]bool
}

// NewReached returns an empty record.
func NewReached() *Reached {
	return &Reached{at: map[jsontext.Pointer]bool{}}
}

// Has reports whether a lowering resolved a reference to pointer. A nil record
// has recorded nothing.
func (r *Reached) Has(pointer jsontext.Pointer) bool {
	return r != nil && r.at[pointer]
}

// note records pointer, unless r is nil.
func (r *Reached) note(pointer jsontext.Pointer) {
	if r != nil {
		r.at[pointer] = true
	}
}
