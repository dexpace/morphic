// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package triage

import (
	"encoding/json/v2"
	"errors"
	"fmt"
)

// file is the shape of a triage file: an object whose rules member lists the
// rules in the order they are given.
type file struct {
	Rules []Rule `json:"rules"`
}

// Decode reads a triage file, a JSON object whose "rules" member lists its
// rules, and checks them as NewRuleset does. It refuses a member neither the
// file nor a rule names, a name given twice and bytes that are not UTF-8, so a
// misspelt field is reported rather than read as absent, and a file holding
// no object, null among them.
func Decode(data []byte) ([]Rule, error) {
	var f *file
	if err := json.Unmarshal(data, &f, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("triage: %w", err)
	}
	if f == nil {
		return nil, errors.New("triage: the file holds null, not an object listing rules")
	}
	if _, err := NewRuleset(f.Rules); err != nil {
		return nil, err
	}
	return f.Rules, nil
}
