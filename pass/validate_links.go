// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// linkSite is one link an operation's responses carry, with the field of the
// operation it sits at, which spells where it is reported.
type linkSite struct {
	from  ir.OperationID
	link  *ir.OperationLink
	field string
}

// appendLinkSites appends to dst every link operation's responses carry: its
// responses', its errors' and its default's, in that order.
func appendLinkSites(dst []linkSite, operation *ir.Operation) []linkSite {
	for i := range operation.Responses {
		dst = appendLinks(dst, operation.ID, operation.Responses[i].Links, "responses/"+strconv.Itoa(i))
	}
	for i := range operation.Errors {
		dst = appendLinks(dst, operation.ID, operation.Errors[i].Links, "errors/"+strconv.Itoa(i))
	}
	if operation.Default != nil {
		dst = appendLinks(dst, operation.ID, operation.Default.Links, "default")
	}
	return dst
}

// appendLinks appends to dst each of links, which a response of from at at
// carries.
func appendLinks(dst []linkSite, from ir.OperationID, links []ir.OperationLink, at string) []linkSite {
	for j := range links {
		dst = append(dst, linkSite{from: from, link: &links[j], field: at + "/links/" + strconv.Itoa(j)})
	}
	return dst
}

// appendLinkInputRefs appends to dst the parameters operation's links read from the
// request their response answers, which are operation's own.
func appendLinkInputRefs(dst []paramRef, operation *ir.Operation) []paramRef {
	for _, site := range appendLinkSites(nil, operation) {
		for k := range site.link.Params {
			dst = appendLinkInputRef(dst, &site.link.Params[k].Value, site.field+"/params/"+strconv.Itoa(k))
		}
		if body := site.link.Body; body != nil {
			dst = appendLinkInputRef(dst, body, site.field+"/body")
		}
	}
	return dst
}

// appendLinkInputRef appends the parameter lv reads, under field, when it
// reads one of the request.
func appendLinkInputRef(dst []paramRef, lv *ir.LinkValue, field string) []paramRef {
	if lv.Kind != ir.LinkRequest {
		return dst
	}
	return appendInputRef(dst, lv.Request, field)
}

// checkLinkTargets holds each link's parameters to the operation it leads to:
// each names a parameter that operation declares, and none is given two
// values. A target the document does not declare is the dangling walk's to
// report, as is a parameter it declares nowhere, and an empty parameter is
// checkEmptyRefs' (reportedHere). Only the operations some link leads to have
// their parameters read.
func checkLinkTargets(doc *ir.Document, refs *ir.IDReferences) []ir.Diagnostic {
	var sites []linkSite
	ir.ForEachOperation(doc, func(operation ir.Operation) { sites = appendLinkSites(sites, &operation) })
	if len(sites) == 0 {
		return nil
	}
	targets := map[ir.OperationID]map[ir.ParameterID]bool{}
	for _, site := range sites {
		targets[site.link.Operation] = nil
	}
	ir.ForEachOperation(doc, func(operation ir.Operation) {
		if _, named := targets[operation.ID]; !named {
			return
		}
		own := make(map[ir.ParameterID]bool, len(operation.Params))
		for _, parameter := range operation.Params {
			own[parameter.ID] = true
		}
		targets[operation.ID] = own
	})
	var diags []ir.Diagnostic
	for _, site := range sites {
		if own := targets[site.link.Operation]; own != nil {
			diags = append(diags, linkParamDiags(site, own, refs)...)
		}
	}
	return diags
}

// linkParamDiags reports each parameter site's link gives that own, its
// target's parameters, does not hold, and each it gives a second value. An
// empty parameter is checkEmptyRefs' to report, however often it is given.
func linkParamDiags(site linkSite, own map[ir.ParameterID]bool, refs *ir.IDReferences) []ir.Diagnostic {
	var diags []ir.Diagnostic
	given := make(map[ir.ParameterID]bool, len(site.link.Params))
	for k, lp := range site.link.Params {
		if lp.Param == "" {
			continue
		}
		where := func() string { return string(site.from) + "/" + site.field + "/params/" + strconv.Itoa(k) }
		switch {
		case given[lp.Param]:
			diags = append(diags, diag(ir.SeverityError, "pass/param-binding-mismatch",
				fmt.Sprintf("%s gives parameter %q a second value", where(), lp.Param), where()))
		case !own[lp.Param] && reportedHere(lp.Param, refs):
			diags = append(diags, diag(ir.SeverityError, "pass/param-binding-mismatch",
				fmt.Sprintf("%s names parameter %q, which operation %q, the link's target, does not declare",
					where(), lp.Param, site.link.Operation), where()))
		}
		given[lp.Param] = true
	}
	return diags
}
