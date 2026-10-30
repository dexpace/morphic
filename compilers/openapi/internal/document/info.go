// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package document

import (
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// lowerInfo maps info onto the document identity, docs, contact, and license.
// GetInfo always returns a non-nil Info (it addresses an embedded struct value),
// so no nil guard is needed.
func lowerInfo(c lowering.Ctx) Meta {
	info := c.Doc.GetInfo()
	m := Meta{
		Name:           info.GetTitle(),
		Version:        info.GetVersion(),
		TermsOfService: info.GetTermsOfService(),
		Docs:           infoDocs(c, info),
	}
	if ct := info.GetContact(); ct != nil {
		m.Contact = &ir.Contact{Name: ct.GetName(), URL: ct.GetURL(), Email: ct.GetEmail()}
	}
	if lic := info.GetLicense(); lic != nil {
		m.License = &ir.License{Name: lic.GetName(), Identifier: lic.GetIdentifier(), URL: lic.GetURL()}
	}
	return m
}

// infoDocs builds the document docs from info summary and description, folding
// in the root externalDocs link when present.
func infoDocs(c lowering.Ctx, info *soa.Info) ir.Docs {
	d := ir.Docs{Summary: info.GetSummary(), Description: info.GetDescription()}
	if ed := c.Doc.GetExternalDocs(); ed != nil {
		d.ExternalDocs = append(d.ExternalDocs, ir.Link{URL: ed.GetURL(), Description: ed.GetDescription()})
	}
	return d
}
