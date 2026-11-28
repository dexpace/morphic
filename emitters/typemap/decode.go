// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package typemap

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"

	"github.com/dexpace/morphic/emitters/options"
)

// Decode reads a user's type table strictly, by options.DecodeLayer, then holds
// it to the rules every target shares. It refuses, wrapping ErrInvalidTable
// and naming the member by its JSON pointer, a kind ir does not declare, an
// empty Type and a Dependency lacking a module or a minimum version; a null
// wraps options.ErrNullMember. It checks the table in sorted order, so a table
// with two faults names the same one on every run. On any error the table is
// the zero Table.
func Decode(raw []byte) (Table, error) {
	table, err := options.DecodeLayer(Table{}, raw)
	if err != nil {
		return Table{}, fmt.Errorf("type table: %w", err)
	}
	if err := checkTable(table); err != nil {
		return Table{}, err
	}
	return table, nil
}

// checkTable holds each row of table to the shared rules, primitives before
// externals, each map in byte order of its keys.
func checkTable(table Table) error {
	primitives := jsontext.Pointer("").AppendToken("primitives")
	for _, kind := range slices.Sorted(maps.Keys(table.Primitives)) {
		kindPointer := primitives.AppendToken(string(kind))
		if !kind.Valid() {
			return invalid(kindPointer, "no primitive kind ir declares")
		}
		rows := table.Primitives[kind]
		for _, encoding := range slices.Sorted(maps.Keys(rows)) {
			if err := checkRow(kindPointer.AppendToken(encoding), rows[encoding]); err != nil {
				return err
			}
		}
	}
	externals := jsontext.Pointer("").AppendToken("externals")
	for _, identity := range slices.Sorted(maps.Keys(table.Externals)) {
		if err := checkRow(externals.AppendToken(identity), table.Externals[identity]); err != nil {
			return err
		}
	}
	return nil
}

// checkRow refuses a row at pointer with an empty Type, or with a Dependency
// lacking either of its members.
func checkRow(pointer jsontext.Pointer, row Row) error {
	if row.Type == "" {
		return invalid(pointer.AppendToken("type"), "empty type")
	}
	if dependency := row.Dependency; dependency != nil && (dependency.Module == "" || dependency.MinVersion == "") {
		return invalid(pointer.AppendToken("dependency"), "a dependency needs both a module and a minimum version")
	}
	return nil
}

// invalid is the refusal of the member at pointer for the reason given.
func invalid(pointer jsontext.Pointer, reason string) error {
	return fmt.Errorf("type table %s: %s: %w", pointer, reason, ErrInvalidTable)
}
