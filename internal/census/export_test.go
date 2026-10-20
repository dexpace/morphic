package census

import (
	"maps"
	"slices"

	"github.com/dexpace/morphic/internal/decls"
)

// ErrNoDeclaration is the error a census returns for a value its universe's
// declarations do not describe.
var ErrNoDeclaration = errNoDeclaration

// UniverseOf derives the universe of the named type root, which p declares.
func UniverseOf(p *decls.Package, root string) (Universe, error) {
	return universeOf(p, root)
}

// NewUniverseOfVersion derives the IR's universe from declarations that must
// be of the IRVersion version.
func NewUniverseOfVersion(version string) (Universe, error) {
	return newUniverse(version)
}

// CheckVersion refuses p unless its IRVersion constant is want.
func CheckVersion(p *decls.Package, want string) error {
	return checkVersion(p, want)
}

// AddAny counts the facts root holds, whatever its type.
func (c *Census) AddAny(root any) error {
	return c.add(root)
}

// WithoutStruct returns u as declarations would that have no struct named
// name.
func (u Universe) WithoutStruct(name string) Universe {
	u.structs = maps.Clone(u.structs)
	delete(u.structs, name)
	return u
}

// WithFields returns u as declarations would that give the struct name the
// fields named.
func (u Universe) WithFields(name string, fields ...string) Universe {
	u.structs = maps.Clone(u.structs)
	sf := *u.structs[name]
	sf.fields = slices.Clone(fields)
	u.structs[name] = &sf
	return u
}

// WithoutVariant returns u as declarations would in which the sum holds no
// variant named variant.
func (u Universe) WithoutVariant(sum, variant string, byPointer bool) Universe {
	u.sums = maps.Clone(u.sums)
	u.sums[sum] = maps.Clone(u.sums[sum])
	delete(u.sums[sum], variantKey{name: variant, byPointer: byPointer})
	return u
}
