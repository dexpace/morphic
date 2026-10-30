// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/dexpace/morphic/ir"
)

// Encoded returns doc's canonical JSON, which changes when anything doc
// reaches does, and fails the test when doc does not encode.
func Encoded(t *testing.T, doc *ir.Document) string {
	t.Helper()
	return encoded(t, doc)
}

// encoded holds Encoded's logic against the minimal testingT surface, so its
// failure is testable.
func encoded(t testingT, doc *ir.Document) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode the document: %v", err)
		return ""
	}
	return string(raw)
}

// RefusedAsCyclic reports whether the JSON encoder refuses doc for containing
// itself. The encoder's sentinel is not exported, so its message is all there
// is to read. That serves, since the question is what the encoder makes of doc,
// not what irverify or ir's own walk does.
func RefusedAsCyclic(doc *ir.Document) bool {
	_, err := json.Marshal(doc)
	return err != nil && strings.Contains(err.Error(), "cycle")
}
