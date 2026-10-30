// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir

// descent records the references a typed traversal is inside, outermost first,
// so that it can decline to enter one a second time. A reference has the
// identity [WalkValues] and the JSON encoder give it: a pointer, or the address
// of a slice's first element together with its length, so a prefix of a slice
// is another reference.
//
// The zero value is ready to use. The scan in enter is as long as the references
// enclosing the traversal, which its depth bound caps.
type descent struct {
	inside []descentRef
}

// descentRef is one entry of a descent: a pointer, and the length of the slice
// whose first element it addresses, or zero when it is a pointer to a value.
type descentRef struct {
	ptr any
	n   int
}

// enter records that the traversal is inside ptr, with the slice length n, and
// reports true. It reports false without recording when the traversal already
// is: the reference contains itself, and entering it again would never end.
func (d *descent) enter(ptr any, n int) bool {
	for _, r := range d.inside {
		if r.ptr == ptr && r.n == n {
			return false
		}
	}
	d.inside = append(d.inside, descentRef{ptr: ptr, n: n})
	return true
}

// leave records that the traversal has left the reference it entered last.
func (d *descent) leave() {
	d.inside = d.inside[:len(d.inside)-1]
}
