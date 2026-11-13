// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/engine/reader"
)

// remoteDoc is a document whose one schema is a $ref to ref, as a network
// document that reaches out of itself would be.
func remoteDoc(ref string) string {
	return "components:\n  schemas:\n    S: {$ref: '" + ref + "'}\n"
}

// remoteReader is the reader of a 3.1 source's external references, judging
// what it fetches against the rule table, as a load hands it one.
func remoteReader(t *testing.T) *external {
	t.Helper()
	self := sourceDocument{judged: judgment{checker: oas.NewChecker()}}
	return newExternal(Options{Reader: reader.Disk{}}, newExternalReads(self, budget.Unbounded()))
}

// TestExternal_ASourceReadOverTheNetworkReadsNoFile pins the read every
// document of a compile whose source was read over the network goes through:
// a location on no http or https URL is refused unread, a path or a file URI
// alike.
func TestExternal_ASourceReadOverTheNetworkReadsNoFile(t *testing.T) {
	t.Parallel()
	fake := &fakeReader{data: []byte("X: {type: integer}\n")}
	self := sourceDocument{path: "http://host/v1/root.yaml", judged: judgment{checker: oas.NewChecker()}}
	e := newExternal(Options{Reader: fake}, newExternalReads(self, budget.Unbounded()))

	for _, name := range []string{"/etc/secret.yaml", "file:///etc/secret.yaml"} {
		_, err := e.document(t.Context(), name)
		require.ErrorContains(t, err, bind.OffNetwork, name)
	}
	assert.Empty(t, fake.reqs, "nothing is read")
}

// TestExternal_DocumentReadsANetworkDocumentStayingOnTheHost pins the other side:
// a network document whose $refs stay on the host is prepared and stored, not
// refused, and so is one whose example writes an `id` a drive letter would
// spell, as no dialect it names reads one.
func TestExternal_DocumentReadsANetworkDocumentStayingOnTheHost(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"a relative $ref":       remoteDoc("./b.yaml#/x"),
		"an example's id":       "openapi: 3.1.0\ncomponents:\n  schemas:\n    S: {type: object, example: {id: 'u:42'}}\n",
		"a 2020-12 schema's id": "components:\n  schemas:\n    S: {id: 'file:///x/', properties: {p: {$ref: 'b.yaml'}}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte(body))
				assert.NoError(t, err)
			})
			key := srv.URL + "/a.yaml"
			ext := remoteReader(t)

			_, err := ext.document(t.Context(), key)

			require.NoError(t, err)
			_, ok := ext.read.preparedFor(key)
			assert.True(t, ok, "a document that stays on the host is prepared")
		})
	}
}

// TestLoad_ANetworkDocumentReachingALocalFileIsRefusedAndReadsNothing pins the
// whole path through Load: the $ref in the network document naming the local
// file is reported unresolved with the reason. A two-order check: the local
// file is also referenced straight from the source, which resolves on its own,
// so the network crossing is the only thing refused, in either declaration
// order.
func TestLoad_ANetworkDocumentReachingALocalFileIsRefusedAndReadsNothing(t *testing.T) {
	t.Parallel()
	dir := externalDir(t, map[string]string{
		"local.yaml": "components:\n  schemas:\n    S: {type: string}\n",
	})
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(remoteDoc("file://" + filepath.ToSlash(filepath.Join(dir, "local.yaml")) +
			"#/components/schemas/S")))
		assert.NoError(t, err)
	})
	remote := "    R: {$ref: '" + srv.URL + "/remote.yaml#/components/schemas/S'}\n"
	fromSource := "    L: {$ref: './local.yaml#/components/schemas/S'}\n"
	for _, order := range []string{remote + fromSource, fromSource + remote} {
		spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n" + order

		_, diags := loadExternal(t, dir, spec, Options{})

		_, unresolved := budgetFindings(diags)
		require.Len(t, unresolved, 1, "only the network crossing is unresolved: %+v", diags)
		assert.Contains(t, unresolved[0], bind.OffNetwork)
		assert.NotContains(t, unresolved[0], "./local.yaml", "the source's own reference to the file resolves")
	}
}
