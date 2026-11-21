// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

var unmodeledType = reflect.TypeFor[ir.Unmodeled]()

// attachUnmodeledTargets records on every UnmodeledEntry in doc the sorted,
// deduplicated TypeIDs lowered for schema $refs and kept discriminator routes
// written inside that entry's Value. It runs after compile.Types.Finish and
// before pruneUnreferencedTypes so every kept landing is visible in the IR to
// pruning, irverify, and emitters.
func attachUnmodeledTargets(ctx context.Context, doc *ir.Document, landings map[schema.LandingSite][]schema.Landing) {
	if len(landings) == 0 {
		return
	}
	idx := indexLandings(landings, doc.Types)
	if len(idx) == 0 {
		return
	}
	_, _ = ir.WalkValuesContext(ctx, doc, ir.DocumentPath, &unmodeledTargetAttacher{bySource: idx})
}

// sourceLandings holds the live landings of one input document, keyed by the
// pointer where each reference or discriminator was written.
type sourceLandings struct {
	exact   map[jsontext.Pointer][]schema.Landing
	hasDisc bool
}

// indexLandings filters landings to type IDs present in types and groups them
// by input document index.
func indexLandings(landings map[schema.LandingSite][]schema.Landing, types ir.TypeRegistry) map[int]*sourceLandings {
	bySource := map[int]*sourceLandings{}
	for site, list := range landings {
		for _, landing := range list {
			if _, live := types[landing.ID]; !live {
				continue
			}
			src := bySource[site.Source]
			if src == nil {
				src = &sourceLandings{exact: map[jsontext.Pointer][]schema.Landing{}}
				bySource[site.Source] = src
			}
			src.exact[site.Pointer] = append(src.exact[site.Pointer], landing)
			if landing.Ref == "" {
				src.hasDisc = true
			}
		}
	}
	return bySource
}

var _ ir.Visitor = (*unmodeledTargetAttacher)(nil)

// unmodeledTargetAttacher visits every ir.Unmodeled map in a Document and
// fills each entry's Targets from bySource.
type unmodeledTargetAttacher struct {
	bySource map[int]*sourceLandings
}

// Visit populates Targets on every entry of an ir.Unmodeled map and skips
// descending into the map's entries.
func (a *unmodeledTargetAttacher) Visit(v reflect.Value, _ ir.WalkPath) bool {
	if v.Type() != unmodeledType {
		return true
	}
	um, _ := reflect.TypeAssert[ir.Unmodeled](v)
	for key, entry := range um {
		src := a.bySource[entry.Provenance.Source]
		if src == nil {
			continue
		}
		if targets := src.targetsFor(key, entry); len(targets) > 0 {
			entry.Targets = targets
			um[key] = entry
		}
	}
	return false
}

// targetsFor returns the sorted, deduplicated TypeIDs referenced inside
// entry.Value, located at entry.Provenance.Pointer.
func (s *sourceLandings) targetsFor(key string, entry ir.UnmodeledEntry) []ir.TypeID {
	raw := bytes.TrimSpace(entry.Value)
	if len(raw) == 0 {
		return nil
	}
	var collected []ir.TypeID
	switch raw[0] {
	case '"':
		collected = s.matchString(raw, entry.Provenance.Pointer)
	case '{', '[':
		if !mightCarryLanding(raw, s.hasDisc) {
			return nil
		}
		var parsed any
		if json.Unmarshal(raw, &parsed) != nil {
			return nil
		}
		collected = s.matchTree(parsed, key, entry.Provenance.Pointer)
	default:
		return nil
	}
	if len(collected) == 0 {
		return nil
	}
	slices.Sort(collected)
	return slices.Compact(collected)
}

// matchString resolves a scalar-string Unmodeled entry (such as openapi:$ref
// or openapi:$dynamicRef) against the landings at or beneath base.
func (s *sourceLandings) matchString(raw []byte, base jsontext.Pointer) []ir.TypeID {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return nil
	}
	var out []ir.TypeID
	out = appendMatchingRef(out, s.exact[base], text)
	if isRefKeyword(base.LastToken()) {
		out = appendMatchingRef(out, s.exact[base.Parent()], text)
	}
	if len(out) == 0 {
		for ptr, list := range s.exact {
			if pointerBeneath(ptr, base) {
				out = appendMatchingRef(out, list, text)
			}
		}
	}
	return out
}

// matchTree walks a decoded JSON object or array iteratively tracking its JSON
// Pointer relative to base, collecting exact landings and falling back when two
// agreeing allOf entries collapsed their provenance to base.
func (s *sourceLandings) matchTree(root any, key string, base jsontext.Pointer) []ir.TypeID {
	type frame struct {
		node any
		rel  jsontext.Pointer
	}
	stack := []frame{{node: root}}
	var out []ir.TypeID
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch val := cur.node.(type) {
		case map[string]any:
			at := base + cur.rel
			before := len(out)
			for _, landing := range s.exact[at] {
				out = append(out, landing.ID)
			}
			if len(out) == before {
				out = s.matchCollapsedObject(val, key, base, cur.rel, out)
			}
			for k, child := range val {
				stack = append(stack, frame{node: child, rel: cur.rel + ids.Ptr(k)})
			}
		case []any:
			for i, child := range val {
				stack = append(stack, frame{node: child, rel: cur.rel + ids.Ptr(strconv.Itoa(i))})
			}
		}
	}
	return out
}

// matchCollapsedObject handles the fallback when two agreeing allOf entries
// collapsed an entry's provenance to the enclosing schema pointer base.
func (s *sourceLandings) matchCollapsedObject(obj map[string]any, key string, base, rel jsontext.Pointer, out []ir.TypeID) []ir.TypeID {
	if refText, ok := refStringOf(obj); ok {
		out = s.matchCollapsedRef(base, ids.Ptr(entryKeyword(key))+rel, refText, out)
	}
	if s.hasDisc && (obj["mapping"] != nil || obj["defaultMapping"] != nil) {
		out = s.matchCollapsedDisc(base, out)
	}
	return out
}

// matchCollapsedRef collects landings strictly beneath base whose pointer ends
// with suffix and whose reference text equals refText.
func (s *sourceLandings) matchCollapsedRef(base, suffix jsontext.Pointer, refText string, out []ir.TypeID) []ir.TypeID {
	for ptr, list := range s.exact {
		if pointerBeneath(ptr, base) && strings.HasSuffix(string(ptr), string(suffix)) {
			out = appendMatchingRef(out, list, refText)
		}
	}
	return out
}

// matchCollapsedDisc collects kept discriminator routes strictly beneath base.
func (s *sourceLandings) matchCollapsedDisc(base jsontext.Pointer, out []ir.TypeID) []ir.TypeID {
	for ptr, list := range s.exact {
		if !pointerBeneath(ptr, base) || ptr.LastToken() != "discriminator" {
			continue
		}
		for _, landing := range list {
			if landing.Ref == "" {
				out = append(out, landing.ID)
			}
		}
	}
	return out
}

// mightCarryLanding cheaply rejects raw JSON payloads that contain no schema
// reference keyword or (when the source has kept discriminators) mapping key.
func mightCarryLanding(raw []byte, hasDisc bool) bool {
	if bytes.Contains(raw, []byte(`"$ref"`)) ||
		bytes.Contains(raw, []byte(`"$dynamicRef"`)) ||
		bytes.Contains(raw, []byte(`"$recursiveRef"`)) {
		return true
	}
	return hasDisc && (bytes.Contains(raw, []byte(`"mapping"`)) || bytes.Contains(raw, []byte(`"defaultMapping"`)))
}

// appendMatchingRef appends the IDs in list whose Ref equals text.
func appendMatchingRef(out []ir.TypeID, list []schema.Landing, text string) []ir.TypeID {
	for _, landing := range list {
		if landing.Ref == text {
			out = append(out, landing.ID)
		}
	}
	return out
}

// refStringOf returns the reference string if obj holds a $ref, $dynamicRef,
// or $recursiveRef string member.
func refStringOf(obj map[string]any) (string, bool) {
	for _, kw := range []string{"$ref", "$dynamicRef", "$recursiveRef"} {
		if text, ok := obj[kw].(string); ok {
			return text, true
		}
	}
	return "", false
}

// isRefKeyword reports whether token is a schema reference keyword.
func isRefKeyword(token string) bool {
	return token == "$ref" || token == "$dynamicRef" || token == "$recursiveRef"
}

// entryKeyword returns the trailing keyword segment of an Unmodeled key (such
// as "not" from "openapi:not" or "openapi:schema/not").
func entryKeyword(key string) string {
	_, rest, _ := strings.Cut(key, ":")
	if _, after, ok := strings.CutLast(rest, "/"); ok {
		return after
	}
	return rest
}

// pointerBeneath reports whether ptr sits strictly beneath base.
func pointerBeneath(ptr, base jsontext.Pointer) bool {
	if base == "" {
		return ptr != ""
	}
	return len(ptr) > len(base) && strings.HasPrefix(string(ptr), string(base)+"/")
}
