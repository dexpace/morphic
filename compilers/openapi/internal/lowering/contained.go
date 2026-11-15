// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// DropContained removes from u each entry that another entry of u holds
// whole: its provenance lies in the same source strictly beneath the other's,
// and its value is the value found there inside the other's. An entry naming a
// key p reads stays, unscoped or beneath any scope, since a promotion reads
// the kept entries and one inside another's value reaches no field
// (docs/ir/unmodeled.md#promotion). With whole set, only those entries are
// asked whether they hold another.
func DropContained(p Promoter, u *ir.Unmodeled, whole ...string) {
	if u == nil || len(*u) < 2 {
		return
	}
	holders := whole
	if len(holders) == 0 {
		holders = slices.Collect(maps.Keys(*u))
	}
	for key, inner := range *u {
		if namesReadKey(p.keys, key) {
			continue
		}
		for _, h := range holders {
			outer, held := (*u)[h]
			if h != key && held && holdsWhole(outer, inner) {
				delete(*u, key)
				break
			}
		}
	}
}

// namesReadKey reports whether key names one of keys, which are sorted:
// "openapi:x-k" and "openapi:<scope>/x-k" both name x-k. It allocates
// nothing.
func namesReadKey(keys []string, key string) bool {
	// Every key this compiler keeps opens with the prefix.
	rest := strings.TrimPrefix(key, extensionKeyPrefix)
	for _, k := range keys {
		if !strings.HasSuffix(rest, k) {
			continue
		}
		if at := len(rest) - len(k); at == 0 || rest[at-1] == '/' {
			return true
		}
	}
	return false
}

// holdsWhole reports whether outer's value holds inner's at the pointer inner
// lies at beneath outer's, in the same source, the two compared as canonical
// JSON. An entry assembled from several positions holds nothing at a pointer
// beneath its own, so it holds no other.
func holdsWhole(outer, inner ir.UnmodeledEntry) bool {
	if outer.Provenance.Source != inner.Provenance.Source {
		return false
	}
	rest, beneath := strings.CutPrefix(string(inner.Provenance.Pointer), string(outer.Provenance.Pointer)+"/")
	if !beneath {
		return false
	}
	at := outer.Value
	for token := range jsontext.Pointer("/" + rest).Tokens() {
		next, ok := heldAt(at, token)
		if !ok {
			return false
		}
		at = next
	}
	found, okFound := canonical(at)
	want, okWant := canonical(inner.Value)
	return okFound && okWant && found == want
}

// heldAt returns what v holds at the pointer token: an object's member it
// names, or a list's element at the index it spells as RFC 6901 does, digits
// with no leading zero. A value that is neither holds nothing beneath it.
func heldAt(v jsontext.Value, token string) (jsontext.Value, bool) {
	switch v.Kind() {
	case '{':
		var members map[string]jsontext.Value
		if json.Unmarshal(v, &members) != nil {
			return nil, false
		}
		next, ok := members[token]
		return next, ok
	case '[':
		var elements []jsontext.Value
		i, err := strconv.Atoi(token)
		if err != nil || i < 0 || strconv.Itoa(i) != token || json.Unmarshal(v, &elements) != nil || i >= len(elements) {
			return nil, false
		}
		return elements[i], true
	default:
		return nil, false
	}
}

// canonical returns v in its canonical form (RFC 8785), and false for v that
// is no JSON value.
func canonical(v jsontext.Value) (string, bool) {
	c := slices.Clone(v)
	if c.Canonicalize() != nil {
		return "", false
	}
	return string(c), true
}
