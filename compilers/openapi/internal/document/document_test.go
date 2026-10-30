// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package document_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const metaSpec = `openapi: 3.2.0
info:
  title: Meta
  version: "2"
  summary: A summary
  description: A description
  contact: {name: Team, url: 'https://team', email: t@example.com}
  license: {name: Apache-2.0, identifier: Apache-2.0}
externalDocs: {url: 'https://docs', description: docs}
x-top: {a: 1}
paths:
  /p:
    get: {operationId: p, responses: {"200": {description: ok}}}
`

func TestMeta_FullDocumentMetadata(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, metaSpec)
	assert.Equal(t, "Meta", doc.Name)
	assert.Equal(t, "2", doc.Version)
	require.NotNil(t, doc.Contact)
	assert.Equal(t, "t@example.com", doc.Contact.Email)
	require.NotNil(t, doc.License)
	assert.Equal(t, "Apache-2.0", doc.License.Identifier)
	assert.NotEmpty(t, doc.Docs.ExternalDocs, "root externalDocs folded into docs")
	assert.NotEmpty(t, doc.Unmodeled, "top-level x-* extension")
}

func TestMeta_NoInfo(t *testing.T) {
	t.Parallel()
	// With no info block the title is empty.
	doc, _ := parseFull(t, "openapi: 3.1.0\npaths: {}\n")
	assert.Empty(t, doc.Name)
	assert.Empty(t, doc.Version)
}
