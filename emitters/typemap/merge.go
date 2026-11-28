// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package typemap

import (
	"cmp"
	"maps"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// MovedRows is what Moved found, each list sorted.
type MovedRows struct {
	// Primitives holds the keys by primitive, then encoding name, in byte
	// order; nil where none moved.
	Primitives []PrimitiveKey
	// Externals holds the identities in byte order; nil where none moved.
	Externals []string
}

// Merge returns builtIn with each of user's rows laid over it, a user row
// replacing the row under its key whole, so a type, its codec, its imports and
// its dependency come from one source. The result shares nothing with either
// argument, and holds no map without a row: a kind with none is left out, and
// a table with none holds nil.
func Merge(builtIn, user Table) Table {
	return withRows(withRows(Table{}, builtIn), user)
}

// withRows sets a copy of each row from holds in table, under its key, and
// returns table. It makes a map only to hold a row.
func withRows(table, from Table) Table {
	for _, kind := range slices.Sorted(maps.Keys(from.Primitives)) {
		rows := from.Primitives[kind]
		for _, encoding := range slices.Sorted(maps.Keys(rows)) {
			if table.Primitives == nil {
				table.Primitives = map[ir.PrimitiveKind]map[string]Row{}
			}
			if table.Primitives[kind] == nil {
				table.Primitives[kind] = map[string]Row{}
			}
			table.Primitives[kind][encoding] = copyRow(rows[encoding])
		}
	}
	for _, identity := range slices.Sorted(maps.Keys(from.Externals)) {
		if table.Externals == nil {
			table.Externals = map[string]Row{}
		}
		table.Externals[identity] = copyRow(from.Externals[identity])
	}
	return table
}

// Moved returns the keys whose built-in row differs between stated, the table
// of the options version a configuration states, and current, the latest
// version's: a row changed, added or removed. A key user holds a row under is
// left out, whatever the row, since the user's row replaces both. Rows compare
// by value, nil Imports equal to empty ones.
func Moved(stated, current, user Table) MovedRows {
	return MovedRows{
		Primitives: movedKeys(primitiveRows(stated), primitiveRows(current), primitiveRows(user),
			comparePrimitiveKeys),
		Externals: movedKeys(stated.Externals, current.Externals, user.Externals, cmp.Compare[string]),
	}
}

// primitiveRows returns table's primitive rows keyed flat.
func primitiveRows(table Table) map[PrimitiveKey]Row {
	rows := map[PrimitiveKey]Row{}
	for kind, encodings := range table.Primitives {
		for encoding, row := range encodings {
			rows[PrimitiveKey{Primitive: kind, Encoding: encoding}] = row
		}
	}
	return rows
}

// movedKeys returns, sorted by compare, each key of stated or current whose
// row the two do not hold alike and user holds none under; nil where none.
func movedKeys[K comparable](stated, current, user map[K]Row, compare func(K, K) int) []K {
	var moved []K
	for _, key := range keysOfEither(stated, current) {
		if _, replaced := user[key]; replaced {
			continue
		}
		statedRow, inStated := stated[key]
		currentRow, inCurrent := current[key]
		if inStated == inCurrent && (!inStated || sameRow(statedRow, currentRow)) {
			continue
		}
		moved = append(moved, key)
	}
	slices.SortFunc(moved, compare)
	return moved
}

// keysOfEither returns each key first or second holds, once, in no order.
func keysOfEither[K comparable](first, second map[K]Row) []K {
	keys := slices.Collect(maps.Keys(first))
	for key := range second {
		if _, inFirst := first[key]; !inFirst {
			keys = append(keys, key)
		}
	}
	return keys
}

// comparePrimitiveKeys orders keys by primitive, then encoding name, in byte
// order.
func comparePrimitiveKeys(a, b PrimitiveKey) int {
	return cmp.Or(cmp.Compare(a.Primitive, b.Primitive), cmp.Compare(a.Encoding, b.Encoding))
}

// sameRow reports whether a and b are one row: each member equal, nil Imports
// equal to empty ones.
func sameRow(a, b Row) bool {
	return a.Type == b.Type && a.Codec == b.Codec && maps.Equal(a.Imports, b.Imports) &&
		sameDependency(a.Dependency, b.Dependency)
}

// sameDependency reports whether a and b are both nil or name one module at
// one minimum version.
func sameDependency(a, b *Dependency) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Missing returns, in byte order, each kind ir declares that table holds no
// default row for, the row under the encoding "". A kind only keyed rows name
// is missing; a kind ir does not declare is never reported. It returns nil
// where none is missing.
func Missing(table Table) []ir.PrimitiveKind {
	var missing []ir.PrimitiveKind
	for _, kind := range declaredKinds() {
		if _, ok := table.Primitives[kind][""]; !ok {
			missing = append(missing, kind)
		}
	}
	return missing
}

// declaredKinds returns a new list of every kind ir declares, in byte order,
// which a test holds to ir's declarations.
func declaredKinds() []ir.PrimitiveKind {
	return []ir.PrimitiveKind{
		ir.PrimitiveAny, ir.PrimitiveBool, ir.PrimitiveBytes, ir.PrimitiveDate, ir.PrimitiveDatetime,
		ir.PrimitiveDatetimeOffset, ir.PrimitiveDecimal, ir.PrimitiveDecimal128, ir.PrimitiveDuration,
		ir.PrimitiveFloat, ir.PrimitiveFloat32, ir.PrimitiveFloat64, ir.PrimitiveInt16, ir.PrimitiveInt32,
		ir.PrimitiveInt64, ir.PrimitiveInt8, ir.PrimitiveInteger, ir.PrimitiveNumber, ir.PrimitiveString,
		ir.PrimitiveTime, ir.PrimitiveUint16, ir.PrimitiveUint32, ir.PrimitiveUint64, ir.PrimitiveUint8,
		ir.PrimitiveURL, ir.PrimitiveUUID,
	}
}
