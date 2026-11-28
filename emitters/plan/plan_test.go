// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/plan"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// widgetDoc returns a fresh document of one model over one base, stamped as a
// compiled one is, with a source and a diagnostic of its own.
func widgetDoc() *ir.Document {
	base := irtest.ModelOf("type/base", func(m *ir.Model) {
		m.Properties = []ir.Property{property(m.ID, "id", func(p *ir.Property) { p.Required = true })}
	})
	widget := irtest.ModelOf("type/widget", func(m *ir.Model) {
		m.Base = &ir.TypeRef{Target: base.ID}
		m.Properties = []ir.Property{property(m.ID, "name", nil)}
	})
	doc := irtest.DocOf(base, widget, &ir.Union{ID: "type/either", Exclusive: true})
	doc.IRVersion = ir.IRVersion
	doc.Name = "widgets"
	doc.Sources = []ir.SourceInfo{{Path: "widgets.yaml"}}
	doc.Diagnostics = []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityInfo, "test/note", "a note", ir.Provenance{})}
	return doc
}

func TestBuild_ANilDocumentPlansAsAnEmptyOne(t *testing.T) {
	t.Parallel()

	fromNil, diags := plan.Build(nil, nil, plan.Policy{})
	require.Empty(t, diags)
	require.NotNil(t, fromNil, "a nil document has a plan")
	fromEmpty := build(t, &ir.Document{}, plan.Policy{})

	assert.Empty(t, cmp.Diff(fromEmpty, fromNil), "(-empty +nil)")
	assert.Empty(t, fromNil.Types)
	assert.NotEmpty(t, fromNil.Version, "an empty document has a digest as any other has")
}

func TestBuild_LeavesTheDocumentAsItFoundIt(t *testing.T) {
	t.Parallel()
	doc := widgetDoc()
	before := irtest.Encoded(t, doc)

	build(t, doc, plan.Policy{StrictExclusive: true})

	assert.Equal(t, before, irtest.Encoded(t, doc))
}

func TestBuild_RecordsThePolicyItRanUnder(t *testing.T) {
	t.Parallel()
	policy := plan.Policy{StrictExclusive: true}

	built := build(t, widgetDoc(), policy)

	assert.Equal(t, policy, built.Policy)
}

// TestBuild_VersionNamesTheDocumentAndThePolicy holds the plan's identity
// (T-31): one document under one policy has one version, a policy field or
// anything the document says moves it, and the document's sources and
// diagnostics, which Build never reads, do not.
func TestBuild_VersionNamesTheDocumentAndThePolicy(t *testing.T) {
	t.Parallel()
	version := func(mutate func(*ir.Document), policy plan.Policy) string {
		doc := widgetDoc()
		if mutate != nil {
			mutate(doc)
		}
		built := build(t, doc, policy)
		require.Len(t, built.Version, 64, "a SHA-256 in hexadecimal")
		return built.Version
	}
	reference := version(nil, plan.Policy{})

	assert.Equal(t, reference, version(nil, plan.Policy{}), "one document under one policy")
	assert.Equal(t, reference, version(func(doc *ir.Document) {
		doc.Sources = []ir.SourceInfo{{Path: "moved/widgets.yaml"}, {Path: "overlay.yaml"}}
	}, plan.Policy{}), "the sources are left out of the digest")
	assert.Equal(t, reference, version(func(doc *ir.Document) { doc.Sources = nil }, plan.Policy{}))
	assert.Equal(t, reference, version(func(doc *ir.Document) {
		doc.Diagnostics = append(doc.Diagnostics,
			ir.NewDiagnostic(ir.SeverityWarning, "test/other", "reworded", ir.Provenance{}))
	}, plan.Policy{}), "the diagnostics are left out of the digest")
	assert.Equal(t, reference, version(func(doc *ir.Document) { doc.Diagnostics = nil }, plan.Policy{}))

	moved := map[string]string{
		"a policy field": version(nil, plan.Policy{StrictExclusive: true}),
		"the document's name": version(func(doc *ir.Document) {
			doc.Name = "gadgets"
		}, plan.Policy{}),
		"a type's fact the plan does not read": version(func(doc *ir.Document) {
			doc.Types["type/widget"].Common().Docs.Description = "a widget"
		}, plan.Policy{}),
		"a type the registry gains": version(func(doc *ir.Document) {
			doc.Types["type/other"] = &ir.Any{ID: "type/other"}
		}, plan.Policy{}),
		"the IR's version": version(func(doc *ir.Document) {
			doc.IRVersion = "0.0.1"
		}, plan.Policy{}),
	}
	seen := map[string]string{reference: "the reference"}
	for what, got := range moved {
		assert.NotContains(t, seen, got, "%s shares a version with %s", what, seen[got])
		seen[got] = what
	}
}

// TestBuild_VersionHashesTheDigestThenThePolicy holds the version to the two
// things the plan's documents say it hashes, computed here apart from Build:
// the SHA-256 of the document's canonical JSON with its sources and
// diagnostics left out, then the policy's canonical JSON.
func TestBuild_VersionHashesTheDigestThenThePolicy(t *testing.T) {
	t.Parallel()
	policy := plan.Policy{StrictExclusive: true}
	doc := widgetDoc()

	stripped := widgetDoc()
	stripped.Sources, stripped.Diagnostics = nil, nil
	document, err := json.Marshal(stripped)
	require.NoError(t, err)
	require.NotContains(t, string(document), `"sources"`)
	require.NotContains(t, string(document), `"diagnostics"`)
	digest := sha256.Sum256(document)
	policyJSON, err := json.Marshal(policy, ir.CanonicalOptions())
	require.NoError(t, err)
	want := sha256.Sum256(append(digest[:], policyJSON...))

	assert.Equal(t, hex.EncodeToString(want[:]), build(t, doc, policy).Version)
}

// versionGolden holds the version of one document under each policy.
const versionGolden = "testdata/version.golden.txt"

// TestBuild_VersionIsPinned holds the version of one document, under each
// policy, to a golden (T-31): what Build hashes is the same from run to run
// and from build to build, so a toolchain or a codec that moves a byte of it,
// a policy's encoding or a document's but for its sources and diagnostics,
// fails here, and -update records the move. The document is stamped with a
// version of its own, so a bump of the IR's moves nothing, and neither do the
// document's sources and diagnostics nor a field it leaves unset that the
// encoding omits.
func TestBuild_VersionIsPinned(t *testing.T) {
	t.Parallel()
	doc := widgetDoc()
	doc.IRVersion = "0.0.0"
	var pinned strings.Builder
	for _, policy := range []plan.Policy{{}, {StrictExclusive: true}} {
		raw, err := json.Marshal(policy, ir.CanonicalOptions())
		require.NoError(t, err)
		fmt.Fprintf(&pinned, "%s %s\n", build(t, doc, policy).Version, raw)
	}

	irtest.CompareBytes(t, versionGolden, []byte(pinned.String()))
}

func TestBuild_ADocumentThatDoesNotEncodeHasNoVersion(t *testing.T) {
	t.Parallel()
	doc := widgetDoc()
	doc.Name = "not UTF-8: \xff"

	built, diags := plan.Build(doc, irfacts.New(doc), plan.Policy{})

	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, "emitters/plan/unencodable", diags[0].Code)
	assert.Equal(t, ir.NoSource, diags[0].Provenance.Source, "the finding names no place in a source")
	require.NotNil(t, built)
	assert.Empty(t, built.Version)
	assert.Len(t, built.Types, len(doc.Types), "the types are planned all the same")
}

// TestPlan_EncodesAsOneDocument holds the plan's own bytes: it round-trips
// through JSON, and one plan has one encoding whatever options the caller
// passes.
func TestPlan_EncodesAsOneDocument(t *testing.T) {
	t.Parallel()
	doc := widgetDoc()
	doc.Types["type/base"].(*ir.Model).Properties[0].Deprecation = &ir.Deprecation{Message: "use uid"}
	built := build(t, doc, plan.Policy{StrictExclusive: true})

	raw, err := json.Marshal(built)
	require.NoError(t, err)
	var decoded plan.Plan
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Empty(t, cmp.Diff(built, &decoded), "(-built +decoded)")

	empty := build(t, &ir.Document{}, plan.Policy{})
	rawEmpty, err := json.Marshal(empty)
	require.NoError(t, err)
	var decodedEmpty plan.Plan
	require.NoError(t, json.Unmarshal(rawEmpty, &decodedEmpty))
	assert.Empty(t, cmp.Diff(empty, &decodedEmpty), "a plan of no type decodes to what encoded it (-built +decoded)")

	again, err := json.Marshal(built, json.Deterministic(false), jsontext.EscapeForHTML(true))
	require.NoError(t, err)
	assert.Equal(t, string(raw), string(again), "a caller's options change no byte")

	var members map[string]jsontext.Value
	require.NoError(t, json.Unmarshal(raw, &members))
	assert.True(t, strings.HasPrefix(string(members["types"]), `{"type/base":`),
		"the types are written in sorted-key order: %s", members["types"])
}

func TestPlan_RefusesToEncodeAStringThatIsNotUTF8(t *testing.T) {
	t.Parallel()
	unencodable := &plan.Plan{Types: map[ir.TypeID]plan.TypePlan{"type/model": {Undeclared: []string{"\xff"}}}}

	_, err := json.Marshal(unencodable, jsontext.AllowInvalidUTF8(true))

	require.Error(t, err)
	assert.ErrorContains(t, err, "plan: encode")
}

// TestPolicy_RoundTripsThroughJSON holds a policy to being data (T-31): it
// decodes back to the value it encoded from.
func TestPolicy_RoundTripsThroughJSON(t *testing.T) {
	t.Parallel()
	for _, policy := range []plan.Policy{{}, {StrictExclusive: true}} {
		raw, err := json.Marshal(policy, ir.CanonicalOptions())
		require.NoError(t, err)
		var decoded plan.Policy
		require.NoError(t, json.Unmarshal(raw, &decoded))
		assert.Equal(t, policy, decoded, "%s", raw)
	}
}
