package operation

import (
	"cmp"
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strconv"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

const (
	// defaultResponseKey is the responses-map key OpenAPI reserves for the
	// catch-all response, spelled once so the pointer that response is lowered
	// under and the parse that recognizes the key cannot drift apart.
	defaultResponseKey = "default"
	// statusKeyLen is the width of every responses-map key that names a status,
	// whether digits ("200") or a wildcard range ("2XX").
	statusKeyLen = 3
)

// lowerResponses lowers an operation's responses onto op (ir-design §7.2): each
// status below 400, or a key naming no status, to Responses and each from 400
// to Errors, in source order, as RFC 9110 classes them, and the default, which
// answers every status the others leave and so states neither class, to
// Default. opDeclPtr is the operation's own declaration pointer, so a $ref'd
// response interns its content once at its component pointer rather than once
// per mount site (issue #107).
func lowerResponses(c lowering.Ctx, ts *compile.Types, op *ir.Operation, src *soa.Operation, opDeclPtr jsontext.Pointer) []ir.Diagnostic {
	// GetResponses never returns nil (it addresses an always-present map), so the
	// loop simply yields nothing when no responses are declared.
	resps := src.GetResponses()
	var diags []ir.Diagnostic
	// Every key read, by the range it resolved to: the map is keyed by the
	// spelling, so two spellings of one range are two entries here and one
	// condition in the IR. Collisions are reported after the loop, once per
	// range, so the report does not depend on which spelling came first.
	byRange := map[ir.StatusRange][]string{}
	for code, rr := range resps.All() {
		// The key's own entry, kept apart from rptr: a $ref'd response resolves
		// to its component, and a fault in the operation's map is sited at the
		// map. Sited at the component it named no operation, and the second
		// operation to make the same mistake lost its warning to the first,
		// since the diagnostic stream dedups on full identity.
		entry := opDeclPtr + ids.Ptr("responses", code)
		r, rc, rptr := lowering.Declared[soa.Response](c, rr, entry)
		if r == nil {
			continue
		}
		rng, named := statusRange(code)
		if !named {
			diags = append(diags, invalidStatusKeyDiag(c, code, entry))
		} else {
			byRange[rng] = append(byRange[rng], code)
		}
		// An unreadable key always takes the else branch, because statusRange pairs
		// a false with the zero range and that is no error range. It has to: an
		// ErrorCase would carry a fault classified from a range nothing derived,
		// and its Conditions would assert a status the key never named — where
		// statusConditions lets the success side record no status at all.
		// TestStatusRange_NamesNoStatus is what pins the pairing.
		if isErrorRange(rng) {
			ec, ecDiags := lowerErrorCase(rc, ts, r, code, rng, rptr, rr)
			diags = append(diags, ecDiags...)
			op.Errors = append(op.Errors, ec)
		} else {
			resp, respDiags := lowerResponse(rc, ts, r, code, statusConditions(rng, named), rptr, rr)
			diags = append(diags, respDiags...)
			op.Responses = append(op.Responses, resp)
		}
	}
	diags = append(diags, lowerDefaultResponse(c, ts, op, resps, opDeclPtr)...)
	return append(diags, duplicateStatusKeyDiags(c, byRange, opDeclPtr+ids.Ptr("responses"))...)
}

// lowerDefaultResponse lowers the responses map's default, if it declares one,
// to op.Default, whose conditions are the catch-all. OpenAPI says it answers
// every status no other key names, which takes in successes and failures alike
// (ir-design §7.2), so the compiler classes it as neither: whether it is the
// success response, an error, or both is a reading of the API, which the plan
// makes under its policy.
func lowerDefaultResponse(c lowering.Ctx, ts *compile.Types, op *ir.Operation,
	resps *soa.Responses, opDeclPtr jsontext.Pointer,
) []ir.Diagnostic {
	def, dc, dptr := lowering.Declared[soa.Response](c, resps.GetDefault(), opDeclPtr+ids.Ptr("responses", defaultResponseKey))
	if def == nil {
		return nil
	}
	resp, diags := lowerResponse(dc, ts, def, defaultResponseKey,
		statusConditions(statusRange(defaultResponseKey)), dptr, resps.GetDefault())
	op.Default = &resp
	return diags
}

// duplicateStatusKeyDiags reports each status range more than one responses-map
// key resolved to, at the map itself. Ranges are visited in order and the keys
// of each sorted, so the same map reads the same however it was written.
func duplicateStatusKeyDiags(c lowering.Ctx, byRange map[ir.StatusRange][]string, mapPtr jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	ranges := slices.SortedFunc(maps.Keys(byRange), func(a, b ir.StatusRange) int {
		return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To))
	})
	for _, rng := range ranges {
		keys := byRange[rng]
		if len(keys) < 2 {
			continue
		}
		slices.Sort(keys)
		diags = append(diags, duplicateStatusKeyDiag(c, keys, mapPtr))
	}
	return diags
}

// lowerResponse lowers one success or default response: its naming, status
// condition, payload (all media types), headers, docs, and any raw links
// preserved for later promotion. code is the responses-map key the response is
// declared under and conds is what that key resolved to, which is nothing at
// all when it named no status (see statusConditions). ref is the entry the
// document wrote there, which carries the summary and description a Reference
// Object may put beside its $ref.
func lowerResponse(c lowering.Ctx, ts *compile.Types, r *soa.Response, code string, conds ir.ResponseConditions, rptr jsontext.Pointer, ref *soa.ReferencedResponse) (ir.Response, []ir.Diagnostic) {
	parts, diags := lowerResponseParts(c, ts, r, code, rptr, ref)
	resp := ir.Response{
		Name:       parts.name,
		Conditions: conds,
		Payload:    parts.payload,
		Headers:    parts.headers,
		Docs:       parts.docs,
		Unmodeled:  parts.unmodeled,
	}
	return resp, diags
}

// responseParts is everything a Response Object lowers to before its status
// class decides which node carries it: an ir.Response and an ir.ErrorCase are
// two lowerings of one source object, and this is the half they share.
type responseParts struct {
	name      ir.Naming
	payload   *ir.Payload
	headers   []ir.Property
	docs      ir.Docs
	unmodeled ir.Unmodeled
}

// lowerResponseParts lowers what a Response Object declares regardless of the
// status it answers to: naming, payload, headers, docs, and any raw links or
// extensions preserved for later promotion.
//
// It is one function for both status classes so nothing can depend on which
// class reached a declaration first: a response mounted as a success and an
// error interns its body once, so two naming fallbacks would rename it when
// paths are reordered. The use-site docs fold over ref, the entry written here,
// must agree across classes for the same reason (GitHub #610).
func lowerResponseParts(c lowering.Ctx, ts *compile.Types, r *soa.Response, code string, rptr jsontext.Pointer, ref *soa.ReferencedResponse) (responseParts, []ir.Diagnostic) {
	headers, diags := lowerHeaders(c, ts, r.GetHeaders(), rptr)
	payload, payloadDiags := lowerPayload(c, ts, r.GetContent(), rptr)
	diags = append(diags, payloadDiags...)
	parts := responseParts{name: responseName(code), payload: payload, headers: headers}
	summary, taken, summaryDiags := responseSummary(c, &parts.unmodeled, r, rptr)
	diags = append(diags, summaryDiags...)
	parts.docs = resolve.UseDocs[soa.Response](ref, ir.Docs{Summary: summary, Description: r.GetDescription()})
	return parts, append(diags, preserveResponseExtras(c, &parts.unmodeled, r, rptr, taken...)...)
}

// responseSummary reads the summary OpenAPI 3.2 gives a Response Object, which
// the parser's model names no member for (GitHub #615), and returns the keys it
// read for the census to leave alone. It reads at every version, as load
// reports the key below 3.2 (diag.LaterVersionConstruct).
func responseSummary(c lowering.Ctx, p *ir.Unmodeled, r *soa.Response, rptr jsontext.Pointer) (string, []string, []ir.Diagnostic) {
	summary, read, diags := annotation.TakeString(p, r.GetRootNode(), "summary", nil, c.ProvenanceAt, rptr, "")
	if !read {
		return "", nil, diags
	}
	return summary, []string{"summary"}, diags
}

// preserveResponseExtras keeps what a Response Object declares that has no home
// on its node: its links map, its x-* extensions, and the undefined keys other
// than taken (responseSummary's). One helper serves ir.Response and
// ir.ErrorCase, so neither keeps a construct the other drops (GitHub #275).
//
// The links entry carries ReasonNoIRHome and no diagnostic, since nothing is
// degraded and the IR could add a links field. A Link Object inside it gets no
// entry or census: this compiler lowers none, and a keyed entry would duplicate
// the node above. A components/links entry it names is kept on the document
// (GitHub #616).
func preserveResponseExtras(c lowering.Ctx, p *ir.Unmodeled, r *soa.Response, rptr jsontext.Pointer, taken ...string) []ir.Diagnostic {
	_, diags := annotation.PreserveNodeInto(p, "openapi:links",
		nodeview.Child(r.GetRootNode(), "links"), ir.ReasonNoIRHome, c.ProvenanceAt(rptr+ids.Ptr("links")))
	ext, extDiags := annotation.ExtensionsFrom(r.GetExtensions(), c.ProvenanceAt, rptr)
	*p = annotation.MergeUnmodeled(*p, ext)
	diags = append(diags, extDiags...)
	return append(diags, annotation.UnknownKeysIn(p, r, c.ProvenanceAt, rptr, taken...)...)
}

// responseName builds a success response's neutral naming. OpenAPI names no
// response, so the hint ir-design §7.2 allows is the status key: "200", "2_xx",
// or the shared mint for a key with no word in it (GitHub #259).
//
// It is the key as declared, not the range it parses to, so "2XX" is not named
// by a spelling its document never used, nor a key naming no status by the
// catch-all range (GitHub #262). Source stays empty: a components/responses key
// names a reusable definition, not this mount of it.
//
// The error side calls this too, since ErrorCase.Name is Response.Name (GitHub
// #422).
func responseName(code string) ir.Naming {
	return compile.NamingHint(code)
}

// lowerErrorCase lowers one error response into an ErrorCase: naming, status
// condition, payload, headers, docs and fault classification, plus any raw
// links preserved for later promotion.
//
// Everything but the condition and the fault is lowerResponseParts' work, the
// helper the success side calls, so a shared declaration lowers one way (GitHub
// #422). code is the responses-map key, the only record of how the source
// spelled a status its range cannot state ("4XX"); ref is the entry written
// here.
func lowerErrorCase(c lowering.Ctx, ts *compile.Types, r *soa.Response, code string, rng ir.StatusRange, rptr jsontext.Pointer, ref *soa.ReferencedResponse) (ir.ErrorCase, []ir.Diagnostic) {
	parts, diags := lowerResponseParts(c, ts, r, code, rptr, ref)
	ec := ir.ErrorCase{
		Name:       parts.name,
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{rng}},
		Payload:    parts.payload,
		Headers:    parts.headers,
		Fault:      faultFor(rng),
		Docs:       parts.docs,
		Unmodeled:  parts.unmodeled,
	}
	return ec, diags
}

// statusRange maps an OpenAPI response key to an inclusive status range: "200"
// → {200,200}, "4XX" → {400,499}, "default" → {0,0} (ir-design §7.2). ok
// reports whether the key named a status, and a false always comes paired with
// the zero range; callers route on that.
//
// Past "default", a key is three characters over one leading digit: OpenAPI
// defines exactly the wildcards 1XX through 5XX and HTTP codes 100 through 599,
// and every character is checked, so "1XY" and "9XX" are refused (GitHub #262).
//
// "default" names the catch-all, the zero range, which lowerDefaultResponse
// asks for: soa.Responses carries it on its own Default field, apart from the
// keys the map loop reads.
func statusRange(code string) (ir.StatusRange, bool) {
	if code == defaultResponseKey {
		return ir.StatusRange{}, true
	}
	if len(code) != statusKeyLen || code[0] < '1' || code[0] > '5' {
		return ir.StatusRange{}, false
	}
	base := int(code[0]-'0') * 100
	if isWildcard(code[1]) && isWildcard(code[2]) {
		return ir.StatusRange{From: base, To: base + 99}, true
	}
	rest, ok := twoDigits(code[1], code[2])
	if !ok {
		return ir.StatusRange{}, false
	}
	return ir.StatusRange{From: base + rest, To: base + rest}, true
}

// isWildcard reports whether b is the wildcard character of a status range.
// OpenAPI writes it uppercase; lowercase is read too, because the reading is
// unambiguous and documents in the wild spell it both ways.
func isWildcard(b byte) bool { return b == 'X' || b == 'x' }

// twoDigits reads the tens and units of a status-code key, reporting false when
// either character is not a digit. It is what separates "200" from "20A" and
// from the half-wildcard "2X0", neither of which names a status.
func twoDigits(tens, units byte) (int, bool) {
	if tens < '0' || tens > '9' || units < '0' || units > '9' {
		return 0, false
	}
	return int(tens-'0')*10 + int(units-'0'), true
}

// statusConditions records the status a response applies to, or no status at
// all when its key named none.
//
// Not the catch-all: {0,0} is what "default" lowers to, so folding an
// unreadable key into it makes a typo indistinguishable from a declared
// default, and a document with both gets two responses claiming the catch-all.
// Nothing is lost by declining to guess: the key survives as the response's
// name hint, and the warning beside it carries its pointer.
func statusConditions(rng ir.StatusRange, ok bool) ir.ResponseConditions {
	if !ok {
		return ir.ResponseConditions{}
	}
	return ir.ResponseConditions{StatusCodes: []ir.StatusRange{rng}}
}

// invalidStatusKeyDiag reports a responses-map key that names no status.
//
// A warning rather than an error: the response itself lowers completely — body,
// headers, docs, name — and only the status it answers to is unknown, so the
// document still compiles to IR worth having. It also keeps a fixture carrying
// one reachable, since harness.Check returns at the first error diagnostic and
// FuzzCompile skips an input that produces one, which would put every oracle
// past this point out of reach of the case that provokes it.
func invalidStatusKeyDiag(c lowering.Ctx, code string, entry jsontext.Pointer) ir.Diagnostic {
	return c.DiagAt(ir.SeverityWarning, diag.InvalidStatusKey, entry,
		"response key %q is no status code, no 1XX-5XX range, and not %s; "+
			"the response is kept with no status condition", code, defaultResponseKey)
}

// duplicateStatusKeyDiag reports the responses-map keys, sorted, that resolve
// to one status range.
//
// A warning, and every response is kept: no key is wrong on its own, and
// dropping one would choose a winner on declaration order. The caller is told
// the entries answer to one status and cannot be told apart by name or
// condition. It is sited at the map and names all the keys, so the report reads
// the same from either spelling of the map.
func duplicateStatusKeyDiag(c lowering.Ctx, keys []string, mapPtr jsontext.Pointer) ir.Diagnostic {
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = strconv.Quote(k)
	}
	return c.DiagAt(ir.SeverityWarning, diag.DuplicateStatusKey, mapPtr,
		"response keys %s name one status range; all are kept, "+
			"and they reach the IR with the same name and condition", strings.Join(quoted, ", "))
}

// isErrorRange reports whether a status range denotes an error: from 400, the
// client and server error classes of RFC 9110 §15.5 and §15.6.
func isErrorRange(r ir.StatusRange) bool { return r.From >= 400 }

// faultFor classifies a status range as RFC 9110 classes it: a 4xx status is a
// client error and a 5xx one a server error, the status code's own meaning
// rather than a reading of the API (ir-design §7.2). Any other range, which no
// ErrorCase holds, is neither.
func faultFor(r ir.StatusRange) string {
	switch {
	case r.From >= 400 && r.From <= 499:
		return "client"
	case r.From >= 500 && r.From <= 599:
		return "server"
	default:
		return ""
	}
}
