package census

import (
	"cmp"
	"fmt"
	"go/constant"
	"go/types"
	"maps"
	"math/bits"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/internal/decls"
)

// enumeration is a named type that declares constants: a closed set of values,
// each recorded as its raw spelling, so a string's own text and an integer's
// decimal digits.
type enumeration struct {
	name string
	// numeric reports an integer type, whose values order by magnitude.
	numeric bool
	// declared holds the raw spelling of every value a constant gives the type.
	declared map[string]bool
	// flags lists the bits of a type whose constants are all single bits, which
	// a value holds in any combination; nil for any other type.
	flags []flag
	// mask is the union of the bits in flags.
	mask uint64
}

// flag is one bit a bit set declares.
type flag struct {
	bit      uint64
	spelling string
}

// enumerations returns the enumerations the package declares, sorted by name:
// every named type of its own that has at least one constant. It refuses a
// constant that is neither a string nor an integer, since no value of it could
// be counted.
func enumerations(p *decls.Package) ([]*enumeration, error) {
	byName := map[string]*enumeration{}
	for c := range p.Objects[*types.Const]() {
		named, isNamed := types.Unalias(c.Type()).(*types.Named)
		if !isNamed || named.Obj().Pkg() != p.Types {
			continue
		}
		spelling, numeric, err := spell(c)
		if err != nil {
			return nil, err
		}
		e, seen := byName[named.Obj().Name()]
		if !seen {
			e = &enumeration{name: named.Obj().Name(), numeric: numeric, declared: map[string]bool{}}
			byName[e.name] = e
		}
		e.declared[spelling] = true
	}
	for _, e := range byName {
		e.readFlags()
	}
	return slices.SortedFunc(maps.Values(byName), func(a, b *enumeration) int {
		return strings.Compare(a.name, b.name)
	}), nil
}

// readFlags finds whether the declared values are all single bits, as in a
// type whose constants are 1 << iota. Such a type holds a set of them, so the
// value 3 is two of its constants and not a third value of its own.
func (e *enumeration) readFlags() {
	if !e.numeric {
		return
	}
	var flags []flag
	var mask uint64
	for spelling := range e.declared {
		n, err := strconv.ParseUint(spelling, 10, 64)
		if err != nil || bits.OnesCount64(n) != 1 {
			return
		}
		flags = append(flags, flag{bit: n, spelling: spelling})
		mask |= n
	}
	slices.SortFunc(flags, func(a, b flag) int { return cmp.Compare(a.bit, b.bit) })
	e.flags, e.mask = flags, mask
}

// held returns the values a document holding the value spelling counts. A bit
// set counts each declared bit it holds, and one more value for the bits no
// constant names; the empty set counts none. Any other value counts as itself.
func (e *enumeration) held(spelling string) []string {
	if e.flags == nil {
		return []string{spelling}
	}
	n, err := strconv.ParseUint(spelling, 10, 64)
	if err != nil {
		return []string{spelling}
	}
	var out []string
	for _, f := range e.flags {
		if n&f.bit != 0 {
			out = append(out, f.spelling)
		}
	}
	if rest := n &^ e.mask; rest != 0 {
		out = append(out, strconv.FormatUint(rest, 10))
	}
	return out
}

// spell returns the raw spelling of the constant c's value and whether it is
// an integer. A constant has the kind of its type, so every constant of an
// enumeration agrees with the first on whether it is.
func spell(c *types.Const) (spelling string, numeric bool, err error) {
	switch c.Val().Kind() {
	case constant.String:
		return constant.StringVal(c.Val()), false, nil
	case constant.Int:
		return c.Val().ExactString(), true, nil
	default:
		return "", false, fmt.Errorf("census: constant %s has kind %s, want String or Int", c.Name(), c.Val().Kind())
	}
}

// compareSpellings orders two raw spellings of one enumeration's values:
// integers by magnitude, strings by their text.
func (e *enumeration) compareSpellings(a, b string) int {
	if !e.numeric {
		return strings.Compare(a, b)
	}
	return compareDecimal(a, b)
}

// compareDecimal orders two integers written in decimal by magnitude, without
// parsing them: by sign, then by digit count, then digit by digit.
func compareDecimal(a, b string) int {
	negA, negB := strings.HasPrefix(a, "-"), strings.HasPrefix(b, "-")
	switch {
	case negA && !negB:
		return -1
	case negB && !negA:
		return 1
	case negA:
		// Both negative: the longer run of digits is the smaller number.
		a, b = b[1:], a[1:]
	}
	return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b))
}

// render writes a raw spelling as the report prints it: an integer bare and a
// string quoted.
func (e *enumeration) render(spelling string) string {
	if e.numeric {
		return spelling
	}
	return strconv.Quote(spelling)
}
