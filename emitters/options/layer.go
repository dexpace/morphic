// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package options

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

// ErrNullMember reports a member written null where a layer may not hold one.
var ErrNullMember = errors.New("member written null; a member meaning the default is left out")

// DecodeLayer reads raw, a JSON object stating no version, over a copy of
// defaults made through their JSON form, leaving defaults unwritten: a map
// merges key by key, a struct member by member. A member that form omits, as
// one tagged "-", is not copied; defaults it cannot carry are an error. It
// refuses an unknown or repeated member, a name in another case, invalid UTF-8
// and each null json decodes, a pointer's or a jsontext.Value member's own. A
// null's error wraps ErrNullMember and names its JSON pointer; a null inside a
// jsontext.Value is kept. Any error returns the zero T.
func DecodeLayer[T any](defaults T, raw []byte) (T, error) {
	var zero T
	layer, err := copyThroughJSON(defaults)
	if err != nil {
		return zero, err
	}
	err = json.Unmarshal(raw, &layer,
		json.RejectUnknownMembers(true),
		json.MatchCaseInsensitiveNames(false),
		jsontext.AllowDuplicateNames(false),
		jsontext.AllowInvalidUTF8(false),
		json.WithUnmarshalers(json.UnmarshalFromFunc(refuseNull)),
	)
	if err != nil {
		return zero, nullMemberByPointer(err)
	}
	return layer, nil
}

// copyThroughJSON returns a copy of value sharing no map, slice or pointer with
// it, made through value's own JSON form, which is what a configuration is.
func copyThroughJSON[T any](value T) (T, error) {
	var copied T
	encoded, err := json.Marshal(value)
	if err != nil {
		return copied, fmt.Errorf("copy defaults: %w", err)
	}
	if err := json.Unmarshal(encoded, &copied); err != nil {
		var zero T
		return zero, fmt.Errorf("copy defaults: %w", err)
	}
	return copied, nil
}

// refuseNull refuses a null wherever json decodes a value, which it does not
// inside a jsontext.Value, and leaves every other token to json's own
// decoding.
func refuseNull(decoder *jsontext.Decoder, _ any) error {
	if decoder.PeekKind() == 'n' {
		return ErrNullMember
	}
	return errors.ErrUnsupported
}

// nullMemberByPointer rewrites a null refusal to name the member's JSON
// pointer, since json's message names the type refuseNull reads, interface {},
// rather than the member's. Every other error is returned as json gave it.
func nullMemberByPointer(err error) error {
	var semantic *json.SemanticError
	if !errors.As(err, &semantic) || !errors.Is(semantic.Err, ErrNullMember) {
		return err
	}
	return fmt.Errorf("%q: %w", semantic.JSONPointer, ErrNullMember)
}
