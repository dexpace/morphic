// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

var unmodeledType = reflect.TypeFor[ir.Unmodeled]()

// attachUnmodeledTargets records on every UnmodeledEntry in doc the TypeIDs
// lowered for schema $refs and kept discriminator routes written inside that
// entry's Value, keyed by their RFC 6901 JSON Pointer inside Value. It runs
// after compile.Types.Finish and before pruneUnreferencedTypes so every kept
// landing is visible in the IR to pruning, irverify, and emitters.
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
	exact    map[jsontext.Pointer][]schema.Landing
	pointers []jsontext.Pointer
	hasDisc  bool
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
			if landing.Discriminator {
				src.hasDisc = true
			}
		}
	}
	for _, src := range bySource {
		src.pointers = slices.Sorted(maps.Keys(src.exact))
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

// targetsFor returns the map of relative RFC 6901 JSON Pointers inside
// entry.Value to the TypeIDs they lowered to, located at
// entry.Provenance.Pointer.
func (s *sourceLandings) targetsFor(key string, entry ir.UnmodeledEntry) map[jsontext.Pointer]ir.TypeID {
	raw := bytes.TrimSpace(entry.Value)
	if len(raw) == 0 {
		return nil
	}
	switch raw[0] {
	case '"':
		return s.matchString(raw, entry.Provenance.Pointer)
	case '{', '[':
		if !mightCarryLanding(raw, s.hasDisc) {
			return nil
		}
		var parsed any
		if json.Unmarshal(raw, &parsed) != nil {
			return nil
		}
		return s.matchTree(parsed, key, entry.Provenance.Pointer)
	default:
		return nil
	}
}

// matchString resolves a scalar-string Unmodeled entry (such as openapi:$ref
// or openapi:$dynamicRef) against the landings at or beneath base, mapping the
// root pointer "" to the resolved TypeID.
func (s *sourceLandings) matchString(raw []byte, base jsontext.Pointer) map[jsontext.Pointer]ir.TypeID {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return nil
	}
	if id, ok := matchingRefID(s.exact[base], text); ok {
		return map[jsontext.Pointer]ir.TypeID{"": id}
	}
	if isRefKeyword(base.LastToken()) {
		if id, ok := matchingRefID(s.exact[base.Parent()], text); ok {
			return map[jsontext.Pointer]ir.TypeID{"": id}
		}
	}
	for _, ptr := range s.pointers {
		if !collapsedBeneath(ptr, base, "") {
			continue
		}
		if id, ok := matchingRefID(s.exact[ptr], text); ok {
			return map[jsontext.Pointer]ir.TypeID{"": id}
		}
	}
	return nil
}

// matchTree walks a decoded JSON object or array iteratively tracking its JSON
// Pointer relative to base, collecting exact landings whose key is present in
// the object and falling back when two agreeing allOf entries collapsed their
// provenance to base.
func (s *sourceLandings) matchTree(root any, key string, base jsontext.Pointer) map[jsontext.Pointer]ir.TypeID {
	type frame struct {
		node any
		rel  jsontext.Pointer
	}
	stack := []frame{{node: root}}
	var out map[jsontext.Pointer]ir.TypeID
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch val := cur.node.(type) {
		case map[string]any:
			out = s.matchObject(val, key, base, cur.rel, out)
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

// matchObject records the landings written directly on obj, situated at rel
// within an Unmodeled entry whose provenance is base.
func (s *sourceLandings) matchObject(obj map[string]any, key string, base, rel jsontext.Pointer, out map[jsontext.Pointer]ir.TypeID) map[jsontext.Pointer]ir.TypeID {
	before := len(out)
	for _, landing := range s.exact[base+rel] {
		if landingMatchesObject(obj, landing) {
			out = setTarget(out, rel+landing.Path, landing.ID)
		}
	}
	if len(out) == before {
		out = s.matchCollapsedObject(obj, key, base, rel, out)
	}
	return out
}

// matchCollapsedObject handles the fallback when two agreeing allOf entries
// collapsed an entry's provenance to the enclosing schema pointer base.
func (s *sourceLandings) matchCollapsedObject(obj map[string]any, key string, base, rel jsontext.Pointer, out map[jsontext.Pointer]ir.TypeID) map[jsontext.Pointer]ir.TypeID {
	suffix := collapsedSuffix(key, rel)
	for _, kw := range refKeywords {
		if refText, ok := obj[kw].(string); ok {
			out = s.matchCollapsedRef(base, suffix, rel, kw, refText, out)
		}
	}
	if s.hasDisc && entryKeyword(key) == "discriminator" && rel == "" {
		out = s.matchCollapsedDisc(obj, base, out)
	}
	return out
}

// matchCollapsedRef records a landing beneath an allOf entry of base whose
// pointer ends with suffix and whose keyword and reference text match kw and
// refText.
func (s *sourceLandings) matchCollapsedRef(base, suffix, rel jsontext.Pointer, kw, refText string, out map[jsontext.Pointer]ir.TypeID) map[jsontext.Pointer]ir.TypeID {
	wantPath := ids.Ptr(kw)
	for _, ptr := range s.pointers {
		if !collapsedBeneath(ptr, base, suffix) {
			continue
		}
		for _, landing := range s.exact[ptr] {
			if landing.Ref == refText && landing.Path == wantPath {
				return setTarget(out, rel+landing.Path, landing.ID)
			}
		}
	}
	return out
}

// matchCollapsedDisc records kept discriminator routes beneath an allOf entry
// of base whose paths and reference strings match obj.
func (s *sourceLandings) matchCollapsedDisc(obj map[string]any, base jsontext.Pointer, out map[jsontext.Pointer]ir.TypeID) map[jsontext.Pointer]ir.TypeID {
	for _, ptr := range s.pointers {
		if !collapsedBeneath(ptr, base, "/discriminator") {
			continue
		}
		for _, landing := range s.exact[ptr] {
			if landing.Discriminator && landingMatchesObject(obj, landing) {
				out = setTarget(out, landing.Path, landing.ID)
			}
		}
	}
	return out
}

// landingMatchesObject reports whether obj contains the JSON string member
// addressed by landing.Path with the exact reference text landing.Ref.
func landingMatchesObject(obj map[string]any, landing schema.Landing) bool {
	if !landing.Discriminator {
		text, ok := obj[landing.Path.LastToken()].(string)
		return ok && text == landing.Ref
	}
	if landing.Path == "/defaultMapping" {
		text, ok := obj["defaultMapping"].(string)
		return ok && text == landing.Ref
	}
	if landing.Path.Parent() == "/mapping" {
		mapping, ok := obj["mapping"].(map[string]any)
		if !ok {
			return false
		}
		text, ok := mapping[landing.Path.LastToken()].(string)
		return ok && text == landing.Ref
	}
	return false
}

// setTarget writes id at ptr into dst, allocating dst on first write.
func setTarget(dst map[jsontext.Pointer]ir.TypeID, ptr jsontext.Pointer, id ir.TypeID) map[jsontext.Pointer]ir.TypeID {
	if dst == nil {
		dst = map[jsontext.Pointer]ir.TypeID{}
	}
	dst[ptr] = id
	return dst
}

// refKeywords are the JSON Schema reference keywords whose values land on types.
var refKeywords = [...]string{"$ref", "$dynamicRef", "$recursiveRef"}

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

// matchingRefID returns the first non-discriminator landing ID in list whose
// Ref equals text.
func matchingRefID(list []schema.Landing, text string) (ir.TypeID, bool) {
	for _, landing := range list {
		if !landing.Discriminator && landing.Ref == text {
			return landing.ID, true
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

// collapsedSuffix returns the pointer suffix beneath an allOf entry that
// corresponds to rel within an Unmodeled entry keyed by key. Combined
// validation-only entries already carry their OpenAPI keyword as the leading
// segment of rel.
func collapsedSuffix(key string, rel jsontext.Pointer) jsontext.Pointer {
	switch kw := entryKeyword(key); kw {
	case "if-then-else", "contains", "unevaluated":
		return rel
	default:
		return ids.Ptr(kw) + rel
	}
}

// collapsedBeneath reports whether ptr is base followed by one or more
// "/allOf/<index>" segments and then suffix.
func collapsedBeneath(ptr, base, suffix jsontext.Pointer) bool {
	if !pointerBeneath(ptr, base) {
		return false
	}
	mid := strings.TrimPrefix(string(ptr), string(base))
	mid, ok := strings.CutSuffix(mid, string(suffix))
	return ok && isAllOfPath(mid)
}

// isAllOfPath reports whether rel is one or more "/allOf/<index>" segments.
func isAllOfPath(rel string) bool {
	for {
		rest, ok := strings.CutPrefix(rel, "/allOf/")
		if !ok {
			return false
		}
		seg, tail, hasTail := strings.Cut(rest, "/")
		if seg == "" || strings.TrimLeft(seg, "0123456789") != "" {
			return false
		}
		if !hasTail {
			return true
		}
		rel = "/" + tail
	}
}

// pointerBeneath reports whether ptr sits strictly beneath base.
func pointerBeneath(ptr, base jsontext.Pointer) bool {
	if base == "" {
		return ptr != ""
	}
	return len(ptr) > len(base) && strings.HasPrefix(string(ptr), string(base)+"/")
}

// seedDiscardedLandings adds to discarded the TypeID of every landing at or
// beneath a dropped provenance (an unpreservable construct diagnostic or a
// discarded Unmodeled container). Landings still reached elsewhere remain live
// during pruning.
func seedDiscardedLandings(discarded map[ir.TypeID]bool, landings map[schema.LandingSite][]schema.Landing, diags []ir.Diagnostic, dropped []ir.Provenance) {
	for _, d := range diags {
		if d.Code == diag.UnpreservableConstruct {
			dropped = append(dropped, d.Provenance)
		}
	}
	if len(dropped) == 0 {
		return
	}
	for site, list := range landings {
		if !siteDropped(site, dropped) {
			continue
		}
		for _, landing := range list {
			discarded[landing.ID] = true
		}
	}
}

// siteDropped reports whether site lies at or beneath any provenance in dropped.
func siteDropped(site schema.LandingSite, dropped []ir.Provenance) bool {
	for _, prov := range dropped {
		if site.Source == prov.Source && (site.Pointer == prov.Pointer || pointerBeneath(site.Pointer, prov.Pointer)) {
			return true
		}
	}
	return false
}
