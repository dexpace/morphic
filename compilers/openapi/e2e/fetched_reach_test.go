// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

const (
	reachEvil   = "https://pub.example/evil.yaml"
	reachSecret = "http://10.0.0.5/secret.yaml"

	// reachRefusalPrivate is the verdict on a private address named by a
	// document read from a public one.
	reachRefusalPrivate = "external document http://10.0.0.5/secret.yaml refused: it is at a private address, " +
		"which a reference in a document read from a public one does not reach"
)

// reachSource holds the schemas under a document of the one OpenAPI version
// the fixtures share.
func reachSource(schemas ...string) string {
	return dynHead + strings.Join(schemas, "")
}

// reachEvilReader is the fixture of a public document naming a private one.
func reachEvilReader(evilSpace compilers.AddressSpace) *fetchedReader {
	return &fetchedReader{docs: map[string]fetchedDoc{
		reachEvil:   {data: "X: {$ref: '" + reachSecret + "#/S'}\n", space: evilSpace},
		reachSecret: {data: "S: {type: string, description: secret}\n", space: compilers.SpacePrivate},
	}}
}

// assertTheSecretIsRefused pins F1's facts on a compile of the evil fixture:
// the one error, in evil.yaml's entry, the reader's requests, and nothing of
// the secret in the document.
func assertTheSecretIsRefused(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, reads *fetchedReader, name string) {
	t.Helper()
	require.Len(t, diags, 1, "%s: %v", name, diags)
	diagnostic := diags[0]
	assert.Equal(t, ir.SeverityError, diagnostic.Severity, name)
	assert.Equal(t, "openapi/unresolved-ref", diagnostic.Code, name)
	assert.EqualValues(t, "/X", diagnostic.Provenance.Pointer, name)
	require.Less(t, diagnostic.Provenance.Source, len(doc.Sources), name)
	assert.Contains(t, doc.Sources[diagnostic.Provenance.Source].Path, "evil.yaml", "%s: the finding lies in the fetched document's entry", name)
	assert.Contains(t, diagnostic.Message, reachRefusalPrivate, name)

	assert.Equal(t, []compilers.AddressSpace{compilers.SpaceUnstated}, reads.servedAt(reachEvil),
		"%s: the source's own reference reaches nothing", name)
	assert.Empty(t, reads.servedAt(reachSecret), "%s: nothing was served from the private address", name)
	assert.Equal(t, []compilers.AddressSpace{compilers.SpacePublic}, reads.refusedAt(reachSecret),
		"%s: the read was asked for at the reach of its referrer, and refused", name)
	for _, sourceInfo := range doc.Sources {
		assert.NotContains(t, sourceInfo.Path, "secret.yaml", "%s: the secret is no source", name)
	}
	assert.NotContains(t, jsonOf(t, doc), "secret", "%s: nor is it in the document", name)
}

// TestCompile_AFetchedDocumentMayNotReachAMorePrivateAddress pins that a
// reference in a document read from a public address to a private one is
// refused, and is not requested: the reader is asked at the reach of the
// document that wrote the reference and refuses, so what the private address
// holds is nowhere in the document.
func TestCompile_AFetchedDocumentMayNotReachAMorePrivateAddress(t *testing.T) {
	t.Parallel()
	const spec = "    A: {$ref: '" + reachEvil + "#/X'}\n"
	for name, evilSpace := range map[string]compilers.AddressSpace{
		"evil states public":                  compilers.SpacePublic,
		"evil states nothing, read as public": compilers.SpaceUnstated,
	} {
		reads := reachEvilReader(evilSpace)
		doc, diags := compileFetched(t, reads, reachSource(spec), "")
		assertTheSecretIsRefused(t, doc, diags, reads, name)
	}
}

// TestCompile_AnAddressOneDocumentCannotReachIsStillReadForAnother pins that
// the refusal is the asker's alone: the source names the same private
// document, unbounded, and reads it, whichever of the two asks first, with
// the fetched document's own reference still refused, and the document read
// once, at the source's reach.
func TestCompile_AnAddressOneDocumentCannotReachIsStillReadForAnother(t *testing.T) {
	t.Parallel()
	const (
		a = "    A: {$ref: '" + reachEvil + "#/X'}\n"
		b = "    B: {$ref: '" + reachSecret + "#/S'}\n"
	)
	for name, tc := range map[string]struct {
		schemas string
		slow    map[string]time.Duration
	}{
		"A then B":              {a + b, nil},
		"B then A":              {b + a, nil},
		"evil answers last":     {a + b, map[string]time.Duration{reachEvil: 40 * time.Millisecond}},
		"secret answers last":   {b + a, map[string]time.Duration{reachSecret: 40 * time.Millisecond}},
		"evil last, B then A":   {b + a, map[string]time.Duration{reachEvil: 40 * time.Millisecond}},
		"secret last, A then B": {a + b, map[string]time.Duration{reachSecret: 40 * time.Millisecond}},
	} {
		reads := reachEvilReader(compilers.SpacePublic)
		reads.slow = tc.slow
		doc, diags := compileFetched(t, reads, reachSource(tc.schemas), "")

		require.Len(t, diags, 1, "%s: %v", name, fetchedFindings(doc, diags))
		assert.Contains(t, diags[0].Message, reachRefusalPrivate, name)
		assert.EqualValues(t, "/X", diags[0].Provenance.Pointer, name)

		target := fetchedTypeNamed(doc, "secret.yaml/S")
		require.NotEmpty(t, target, "%s: the source's reference resolves", name)
		sc, ok := doc.Types[componentID("B")].(*ir.Scalar)
		require.True(t, ok, name)
		require.NotNil(t, sc.Base, name)
		assert.Equal(t, target, sc.Base.Target, name)

		assert.Equal(t, []compilers.AddressSpace{compilers.SpaceUnstated}, reads.servedAt(reachSecret),
			"%s: served once, at the source's reach, to no later asker", name)
	}
}

// TestCompile_APrivateDocumentMayReachAPrivateAddressButNotALocalOne pins the
// order: a document read from a private address reaches private addresses and
// is refused a loopback one, by order and not by equality.
func TestCompile_APrivateDocumentMayReachAPrivateAddressButNotALocalOne(t *testing.T) {
	t.Parallel()
	reads := &fetchedReader{docs: map[string]fetchedDoc{
		"http://10.0.0.5/a.yaml": {space: compilers.SpacePrivate,
			data: "X: {$ref: 'b.yaml#/Y'}\nZ: {$ref: 'http://127.0.0.1/c.yaml#/Y'}\n"},
		"http://10.0.0.5/b.yaml":  {space: compilers.SpacePrivate, data: "Y: {type: string}\n"},
		"http://127.0.0.1/c.yaml": {space: compilers.SpaceLocal, data: "Y: {type: integer, description: local}\n"},
	}}
	doc, diags := compileFetched(t, reads, reachSource(
		"    A: {$ref: 'http://10.0.0.5/a.yaml#/X'}\n    B: {$ref: 'http://10.0.0.5/a.yaml#/Z'}\n"), "")

	require.Len(t, diags, 1, "%v", fetchedFindings(doc, diags))
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, "openapi/unresolved-ref", diags[0].Code)
	assert.EqualValues(t, "/Z", diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "external document http://127.0.0.1/c.yaml refused: it is at a loopback address, "+
		"which a reference in a document read from a private one does not reach")

	assert.Equal(t, []compilers.AddressSpace{compilers.SpacePrivate}, reads.servedAt("http://10.0.0.5/b.yaml"),
		"a private document's reference is read at private reach")
	assert.Empty(t, reads.servedAt("http://127.0.0.1/c.yaml"))
	assert.NotEmpty(t, fetchedTypeNamed(doc, "b.yaml/Y"), "X resolves")
	assert.Empty(t, fetchedTypeNamed(doc, "c.yaml/Y"), "and nothing of the loopback document is in the document")
}

// TestCompile_APublicChainOfFetchedDocumentsReachesAsFarAsItGoes pins that
// the bound applies to what a fetched document writes and to nothing else: a
// chain of public documents each naming the next resolves, the source's own
// reference is unbounded, and each later one is read at public reach.
func TestCompile_APublicChainOfFetchedDocumentsReachesAsFarAsItGoes(t *testing.T) {
	t.Parallel()
	reads := &fetchedReader{docs: map[string]fetchedDoc{
		"https://pub.example/a.yaml": {space: compilers.SpacePublic, data: "X: {$ref: 'b.yaml#/Y'}\n"},
		"https://pub.example/b.yaml": {space: compilers.SpacePublic, data: "Y: {$ref: 'c.yaml#/Z'}\n"},
		"https://pub.example/c.yaml": {space: compilers.SpacePublic, data: "Z: {type: string}\n"},
	}}
	doc, diags := compileFetched(t, reads, reachSource("    A: {$ref: 'https://pub.example/a.yaml#/X'}\n"), "")
	assert.Empty(t, diags)
	assert.NotEmpty(t, fetchedTypeNamed(doc, "c.yaml/Z"), "every link resolves")
	assert.Equal(t, []compilers.AddressSpace{compilers.SpaceUnstated}, reads.servedAt("https://pub.example/a.yaml"))
	assert.Equal(t, []compilers.AddressSpace{compilers.SpacePublic}, reads.servedAt("https://pub.example/b.yaml"))
	assert.Equal(t, []compilers.AddressSpace{compilers.SpacePublic}, reads.servedAt("https://pub.example/c.yaml"))
	assert.Empty(t, reads.refusedAt("https://pub.example/b.yaml"))
}

// TestCompile_ADeclarationOfAFileAnswersNoFetchedDocument pins that a
// declaration answers only an asker that could read its document: a private
// document's identifier is answered to the source, which may read it, and to
// no fetched document, whichever asks first; to the fetched one the URI is an
// address, judged as one.
func TestCompile_ADeclarationOfAFileAnswersNoFetchedDocument(t *testing.T) {
	t.Parallel()
	const (
		idURL = "http://10.0.0.5/ids/z"
		lib   = "http://10.0.0.5/lib.yaml"
		l     = "    L: {$ref: '" + lib + "#/T'}\n"
		m     = "    M: {$ref: '" + idURL + "'}\n"
		a     = "    A: {$ref: '" + reachEvil + "#/X'}\n"
	)
	for name, schemas := range map[string]string{"L M A": l + m + a, "A M L": a + m + l, "M A L": m + a + l} {
		reads := &fetchedReader{docs: map[string]fetchedDoc{
			lib: {space: compilers.SpacePrivate,
				data: "Z: {$id: '" + idURL + "', type: string, description: secret}\nT: {type: string}\n"},
			idURL:     {space: compilers.SpacePrivate, data: "{type: boolean}\n"},
			reachEvil: {space: compilers.SpacePublic, data: "X: {$ref: '" + idURL + "'}\n"},
		}}
		doc, diags := compileFetched(t, reads, reachSource(schemas), "")

		// lib.yaml's Z is lowered, so its $id is kept with the info every
		// lowered $id draws.
		failed := byCode(diags, "openapi/unresolved-ref")
		require.Len(t, failed, 1, "%s: %v", name, fetchedFindings(doc, diags))
		require.Len(t, byCode(diags, "openapi/degraded-construct"), 1, "%s: %v", name, fetchedFindings(doc, diags))
		assert.Equal(t, ir.SeverityError, failed[0].Severity, name)
		assert.EqualValues(t, "/X", failed[0].Provenance.Pointer, name)
		assert.Contains(t, failed[0].Message, "external document "+idURL+" refused: it is at a private address, "+
			"which a reference in a document read from a public one does not reach", name)

		mm, ok := doc.Types[componentID("M")].(*ir.Scalar)
		require.True(t, ok, name)
		require.NotNil(t, mm.Base, name)
		assert.Equal(t, fetchedTypeNamed(doc, "lib.yaml/Z"), mm.Base.Target, "%s: the source is answered by the declaration", name)
		assert.Equal(t, []compilers.AddressSpace{compilers.SpaceUnstated}, reads.servedAt(idURL),
			"%s: served once, to the source's unbounded ask, and never at public reach", name)
		for _, at := range reads.refusedAt(idURL) {
			assert.Equal(t, compilers.SpacePublic, at, "%s: the only asks of that address are the fetched document's", name)
		}
	}
}

// TestCompile_AReaderThatCannotBoundAReadFailsOnlyTheAsker pins that a read
// refused for the asker alone is not the document's answer: a reader failing
// every bounded read still serves the source's unbounded one, in whichever
// order, so the source's own reference resolves and the fetched document's
// does not.
func TestCompile_AReaderThatCannotBoundAReadFailsOnlyTheAsker(t *testing.T) {
	t.Parallel()
	const (
		a = "    A: {$ref: 'https://pub.example/pub.yaml#/X'}\n"
		b = "    B: {$ref: 'https://pub.example/t.yaml#/T'}\n"
	)
	for name, tc := range map[string]struct {
		schemas string
		slow    map[string]time.Duration
	}{
		"A then B":     {a + b, nil},
		"B then A":     {b + a, nil},
		"t.yaml first": {a + b, map[string]time.Duration{"https://pub.example/pub.yaml": 40 * time.Millisecond}},
		"t.yaml last":  {b + a, map[string]time.Duration{"https://pub.example/t.yaml": 40 * time.Millisecond}},
	} {
		reads := &fetchedReader{unhookable: true, slow: tc.slow, docs: map[string]fetchedDoc{
			"https://pub.example/pub.yaml": {data: "X: {$ref: 't.yaml#/T'}\n"},
			"https://pub.example/t.yaml":   {data: "T: {type: string}\n"},
		}}
		doc, diags := compileFetched(t, reads, reachSource(tc.schemas), "")

		resolved, ok := doc.Types[componentID("B")].(*ir.Scalar)
		require.True(t, ok, "%s: B resolves", name)
		require.NotNil(t, resolved.Base, name)
		assert.NotEqual(t, ir.TypeID("type/primitive/any"), resolved.Base.Target, name)
		failed := byCode(diags, "openapi/unresolved-ref")
		require.Len(t, failed, 1, "%s: %v", name, fetchedFindings(doc, diags))
		assert.EqualValues(t, "/X", failed[0].Provenance.Pointer, name)
		assert.Equal(t, ir.SeverityError, failed[0].Severity, name)
		assert.NotEmpty(t, reads.servedAt("https://pub.example/t.yaml"), "%s: the unbounded read was served", name)
	}
}

// TestCompile_ASourceLocatedAtAURLIsBoundedAsAPublicDocument pins that a source
// handed with a URL for its location is a fetched document like any other:
// its references are read at public reach, a public one resolves and a private
// one is refused.
func TestCompile_ASourceLocatedAtAURLIsBoundedAsAPublicDocument(t *testing.T) {
	t.Parallel()
	reads := &fetchedReader{docs: map[string]fetchedDoc{
		reachSecret:                  {space: compilers.SpacePrivate, data: "S: {type: string, description: secret}\n"},
		"https://pub.example/b.yaml": {space: compilers.SpacePublic, data: "Y: {type: integer}\n"},
	}}
	doc, diags := compileFetched(t, reads, reachSource(
		"    A: {$ref: '"+reachSecret+"#/S'}\n    B: {$ref: 'https://pub.example/b.yaml#/Y'}\n"), "https://pub.example/spec.yaml")

	require.Len(t, diags, 1, "%v", fetchedFindings(doc, diags))
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.EqualValues(t, "/components/schemas/A", diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, reachRefusalPrivate)
	assert.Equal(t, []compilers.AddressSpace{compilers.SpacePublic}, reads.servedAt("https://pub.example/b.yaml"))
	assert.Empty(t, reads.servedAt(reachSecret))
	assert.NotEmpty(t, fetchedTypeNamed(doc, "b.yaml/Y"))
}

// TestCompile_AFetchedDocumentWhoseReaderSaidNothingIsRefusedToABoundedAsker
// pins that a reader stating no address space protects nothing: a document a
// fetched document names is refused when the reader did not say where it was
// read from, and read when it did.
func TestCompile_AFetchedDocumentWhoseReaderSaidNothingIsRefusedToABoundedAsker(t *testing.T) {
	t.Parallel()
	docs := func(space compilers.AddressSpace) *fetchedReader {
		return &fetchedReader{docs: map[string]fetchedDoc{
			"https://pub.example/a.yaml": {space: space, data: "X: {$ref: 'b.yaml#/Y'}\n"},
			"https://pub.example/b.yaml": {space: space, data: "Y: {type: string}\n"},
		}}
	}
	const spec = "    A: {$ref: 'https://pub.example/a.yaml#/X'}\n"

	doc, diags := compileFetched(t, docs(compilers.SpaceUnstated), reachSource(spec), "")
	require.Len(t, diags, 1, "%v", fetchedFindings(doc, diags))
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, "openapi/unresolved-ref", diags[0].Code)
	assert.EqualValues(t, "/X", diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "the reader did not say what address it was read from")

	doc, diags = compileFetched(t, docs(compilers.SpacePublic), reachSource(spec), "")
	assert.Empty(t, diags, "the same documents stating public resolve")
	assert.NotEmpty(t, fetchedTypeNamed(doc, "b.yaml/Y"))
}

// TestCompile_AFailedReadIsNoAnswerToADocumentThatCouldNotReadIt pins that a
// read made for the source's reference, which failed, answers a fetched
// document's reference to the same private document as its own read would:
// refused, in every order, and never with the failure, which says what lies
// at an address the fetched document may not reach.
func TestCompile_AFailedReadIsNoAnswerToADocumentThatCouldNotReadIt(t *testing.T) {
	t.Parallel()
	const (
		a = "    A: {$ref: '" + reachEvil + "#/X'}\n"
		b = "    B: {$ref: '" + reachSecret + "#/S'}\n"
	)
	for secret, failure := range map[string]string{
		"S: [unclosed\n": "does not parse",
		"":               "is empty",
	} {
		for name, tc := range map[string]struct {
			schemas string
			slow    map[string]time.Duration
		}{
			"A then B":            {a + b, nil},
			"B then A":            {b + a, nil},
			"evil answers last":   {a + b, map[string]time.Duration{reachEvil: 40 * time.Millisecond}},
			"secret answers last": {b + a, map[string]time.Duration{reachSecret: 40 * time.Millisecond}},
		} {
			reads := reachEvilReader(compilers.SpacePublic)
			reads.docs[reachSecret] = fetchedDoc{data: secret, space: compilers.SpacePrivate}
			reads.slow = tc.slow
			doc, diags := compileFetched(t, reads, reachSource(tc.schemas), "")

			require.Len(t, diags, 2, "%s, %s: %v", failure, name, fetchedFindings(doc, diags))
			for _, diagnostic := range diags {
				switch diagnostic.Provenance.Pointer {
				case "/X":
					assert.Contains(t, diagnostic.Message, reachRefusalPrivate, "%s, %s", failure, name)
					assert.NotContains(t, diagnostic.Message, failure, "%s, %s", failure, name)
				default:
					assert.EqualValues(t, "/components/schemas/B", diagnostic.Provenance.Pointer, "%s, %s", failure, name)
					assert.Contains(t, diagnostic.Message, failure, "%s, %s: the source's own read failed", failure, name)
				}
			}
		}
	}
}

// TestCompile_ADocumentReadThroughAProxyAnswersEachReferenceAsItsOwnRead pins,
// through the binder, that a document read through a proxy by a name answers
// a public document's reference as that reference's own read would, whether
// the source's reference read it first or the public document's did: served
// where the name resolves public, refused where it resolves private or to
// nothing. Its own reference to a private address is refused in both orders,
// and the diagnostics and the types the two orders share are equal.
func TestCompile_ADocumentReadThroughAProxyAnswersEachReferenceAsItsOwnRead(t *testing.T) {
	t.Parallel()
	const (
		proxied = "http://name.example/d.yaml"
		pub     = "https://pub.example/p.yaml"
	)
	// The source reads the document first where it names it, and after the
	// public document where it reaches it through two files.
	orders := map[string]string{
		"source first": "    A: {$ref: '" + proxied + "#/D'}\n    B: {$ref: '" + pub + "#/X'}\n",
		"public first": "    A: {$ref: 'f.yaml#/F'}\n    B: {$ref: '" + pub + "#/X'}\n",
	}
	for resolved, refusal := range map[compilers.AddressSpace]string{
		compilers.SpacePublic:   "",
		compilers.SpacePrivate:  "it is at a private address, which a reference in a document read from a public one",
		compilers.SpaceUnstated: "the reader cannot bound the addresses its read dials",
	} {
		var diagsSeen []string
		var typesSeen map[ir.TypeID]string
		for name, schemas := range orders {
			reads := &fetchedReader{docs: map[string]fetchedDoc{
				proxied:     {data: "D: {$ref: '" + reachSecret + "#/S'}\n", space: resolved, proxied: true},
				pub:         {data: "X: {$ref: '" + proxied + "#/D'}\n", space: compilers.SpacePublic},
				reachSecret: {data: "S: {type: string, description: secret}\n", space: compilers.SpacePrivate},
				"f.yaml":    {data: "F: {$ref: 'g.yaml#/G'}\n"},
				"g.yaml":    {data: "G: {$ref: '" + proxied + "#/D'}\n"},
			}}
			doc, diags := compileFetched(t, reads, reachSource(schemas), "")
			if resolved == compilers.SpacePublic {
				read := map[string][]compilers.AddressSpace{
					"source first": {compilers.SpaceUnstated, compilers.SpacePublic},
					"public first": {compilers.SpacePublic},
				}[name]
				assert.Equal(t, read, reads.servedAt(proxied), "%s: the reach each read of it was asked at", name)
			}
			var got []string
			for _, diagnostic := range diags {
				got = append(got, fmt.Sprintf("%s %s %s %s", diagnostic.Code, doc.Sources[diagnostic.Provenance.Source].Path, diagnostic.Provenance.Pointer, diagnostic.Message))
			}
			slices.Sort(got)
			require.NotEmpty(t, got, "%d, %s", resolved, name)
			assert.Contains(t, strings.Join(got, "\n"), "d.yaml /D", "%d, %s: its own reference is refused", resolved, name)
			if refusal == "" {
				assert.Len(t, got, 1, "%d, %s: %v", resolved, name, got)
			} else {
				assert.Contains(t, strings.Join(got, "\n"), "p.yaml /X unresolved $ref", "%d, %s", resolved, name)
				assert.Contains(t, strings.Join(got, "\n"), refusal, "%d, %s", resolved, name)
			}
			types := map[ir.TypeID]string{}
			for id, ty := range doc.Types {
				if s := string(id); !strings.Contains(s, "f.yaml") && !strings.Contains(s, "g.yaml") && id != componentID("A") {
					types[id] = bySourcePath(t, doc, ty)
				}
			}
			if diagsSeen != nil {
				assert.Equal(t, diagsSeen, got, "%d: the diagnostics are alike in both orders", resolved)
				assert.Equal(t, typesSeen, types, "%d: so are the types both orders share", resolved)
			}
			diagsSeen, typesSeen = got, types
		}
	}
}

// sourceIndex is a source's index as a type's encoding writes it.
var sourceIndex = regexp.MustCompile(`"(source|writtenBy)":(\d+)`)

// bySourcePath encodes ty with each source index written as the path of the
// source it names, which two compiles reading their documents in other orders
// number otherwise.
func bySourcePath(t *testing.T, doc *ir.Document, ty ir.TypeDef) string {
	t.Helper()
	enc, err := json.Marshal(ty, json.Deterministic(true))
	require.NoError(t, err)
	return sourceIndex.ReplaceAllStringFunc(string(enc), func(m string) string {
		parts := sourceIndex.FindStringSubmatch(m)
		n, err := strconv.Atoi(parts[2])
		require.NoError(t, err)
		require.Less(t, n, len(doc.Sources))
		return strconv.Quote(parts[1]) + ":" + strconv.Quote(doc.Sources[n].Path)
	})
}
