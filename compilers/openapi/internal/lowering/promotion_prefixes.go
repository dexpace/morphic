// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// UnreadExtensions reports each vendor extension doc keeps under a prefix c's
// promotion policy owns that the compiler reads no key of, naming the nearest
// key it reads, as the likeliest one it misspells. The keys read are each
// reader's own: the promotion mapping's (Promoter.Keys) and grouping's
// (GroupingExtension.Keys). doc is the whole document, every node's Unmodeled
// read once; an extension inside an entry kept verbatim, such as an allOf
// branch, is a value of that entry and is not read.
func UnreadExtensions(c Ctx, doc *ir.Document) []ir.Diagnostic {
	p := c.Promoter()
	if len(p.prefixes) == 0 {
		return nil
	}
	read := slices.AppendSeq(c.GroupingExtension.Keys(c.Grouping), p.Keys())
	slices.Sort(read)
	read = slices.Compact(read)
	var diags []ir.Diagnostic
	unmodeledType := reflect.TypeFor[ir.Unmodeled]()
	// Whether the walk stopped goes unread: the visitor prunes at every Unmodeled,
	// so it stops only where a visitor shown everything does, and the run's
	// validate, whose walk carries one, reports ir/walk-truncated there.
	ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
		if v.Type() != unmodeledType {
			return true
		}
		if entries, ok := reflect.TypeAssert[ir.Unmodeled](v); ok {
			diags = append(diags, unreadIn(p, read, entries)...)
		}
		return false
	}))
	return diags
}

// unreadIn reports the extensions of entries UnreadExtensions reports for p,
// read being every key the compiler reads, sorted, in key order.
func unreadIn(p Promoter, read []string, entries ir.Unmodeled) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, key := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[key]
		if entry.Reason != ir.ReasonVendorExtension {
			continue
		}
		name := extensionName(key)
		owner, owned := ownerOf(p.prefixes, name)
		if _, isRead := slices.BinarySearch(read, name); !owned || isRead {
			continue
		}
		hint := ""
		if near, ok := nearest(name, read); ok {
			hint = "; the nearest it reads is " + strconv.Quote(near)
		}
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.UnreadExtension, entry.Provenance,
			"extension %q is under prefix %q, which the promotion policy owns, and the compiler reads no such key%s",
			name, owner, hint))
	}
	return diags
}

// extensionName is the extension an Unmodeled key keeps: its last token,
// whatever scope the key names it under.
func extensionName(key string) string {
	key = strings.TrimPrefix(key, extensionKeyPrefix)
	if i := strings.LastIndexByte(key, '/'); i >= 0 {
		key = key[i+1:]
	}
	return strings.NewReplacer("~1", "/", "~0", "~").Replace(key)
}

// ownerOf returns the longest of prefixes name starts with.
func ownerOf(prefixes []string, name string) (string, bool) {
	best := ""
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	return best, best != ""
}

// nearest returns the key of keys at the least edit distance from name, the
// first in their order at a tie, and false for no keys.
func nearest(name string, keys []string) (string, bool) {
	best, bestDistance := "", -1
	for _, k := range keys {
		if d := editDistance(name, k); bestDistance < 0 || d < bestDistance {
			best, bestDistance = k, d
		}
	}
	return best, bestDistance >= 0
}

// editDistance is the Levenshtein distance between a and b, in bytes: the
// fewest insertions, deletions and substitutions turning one into the other.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
