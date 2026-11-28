// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
)

// codeUnencodable reports a document that has no canonical JSON, so no digest
// for the plan's version to hash.
const codeUnencodable = "emitters/plan/unencodable"

// Plan is what [Build] derives from one document under one policy
// (docs/emitters/plan/overview.md). Every key is one of the document's stable
// IDs. A plan points into the document it was built from and copies none of
// it, so it describes that document as it stood. It is safe for concurrent
// reads: nothing writes one once Build returns.
type Plan struct {
	// Ops holds one entry for each operation [ir.ForEachOperation] reaches,
	// keyed by its ID; of two sharing an ID, the first.
	Ops map[ir.OperationID]OpPlan `json:"ops,omitempty"`
	// Types holds one entry for each type of the document's registry.
	Types map[ir.TypeID]TypePlan `json:"types,omitempty"`
	// Policy is the policy Build ran under.
	Policy Policy `json:"policy"`
	// Version hashes the document and the policy the plan was built from
	// (docs/emitters/plan/overview.md#the-plans-version): two plans share one
	// exactly when both were built from one document, its sources and
	// diagnostics aside, under one policy. It is empty for a document that
	// does not encode.
	Version string `json:"version,omitempty"`
}

// MarshalJSONTo encodes the Plan as a document encodes itself, under
// [ir.CanonicalOptions], whatever options the caller passed, so its maps are
// written in sorted-key order and one plan has one encoding. Only whitespace
// stays the caller's.
func (p *Plan) MarshalJSONTo(enc *jsontext.Encoder) error {
	type alias Plan
	if err := json.MarshalEncode(enc, (*alias)(p), ir.CanonicalOptions()); err != nil {
		return fmt.Errorf("plan: encode: %w", err)
	}
	return nil
}

// Build derives the plan of doc under policy. It is pure: it writes neither doc
// nor facts, and one document under one policy builds one plan. A nil doc
// plans as an empty one. The findings are the plan's own, coded
// emitters/plan/…, and the document's are left where they are.
//
// facts are doc's derived facts. No field built so far reads them: the
// shapes a document exercises a type in and the groups reaching it, which
// follow, do.
func Build(doc *ir.Document, _ *irfacts.DocumentFacts, policy Policy) (*Plan, []ir.Diagnostic) {
	if doc == nil {
		doc = &ir.Document{}
	}
	built := &Plan{Ops: operationPlans(doc, policy), Types: typePlans(doc.Types, policy), Policy: policy}

	version, err := versionOf(doc, policy)
	if err != nil {
		return built, []ir.Diagnostic{ir.NewDiagnostic(ir.SeverityError, codeUnencodable,
			fmt.Sprintf("the document has no canonical JSON, so its plan has no version: %v", err),
			ir.Provenance{Source: ir.NoSource})}
	}
	built.Version = version
	return built, nil
}
