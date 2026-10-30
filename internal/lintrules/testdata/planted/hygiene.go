// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package planted

import (
	"context"
	"errors"
	"os"
	"time"
)

// Wire is a JSON shape with one field spelled otherwise than Go spells it,
// beside JSON and YAML names keeping an initialism whole, as Go does.
type Wire struct {
	SnakeField string `json:"snake_field"` // want "json(goCamel): got 'snake_field' want 'snakeField' (tagliatelle)"
	CamelField string `json:"camelField"`
	WireID     int    `json:"wireID"`
	SpecURL    string `yaml:"specURL"`
}

// bidi hides a right-to-left override in a string literal.
const bidi = "‮" // want "found dangerous unicode character sequence RIGHT-TO-LEFT-OVERRIDE (bidichk)"

func init() {} // want "don't use `init` function (gochecknoinits)"

//go:generat echo // want "compiler directive unrecognized: //go:generat (gocheckcompilerdirectives)"
func plantDirective() {}

func work() error { return errors.New("work") }

// plantHygiene draws the bug-class linters' findings.
func plantHygiene(ctx context.Context, d time.Duration, keys []string) (time.Duration, error) {
	total := d * time.Second // want "Multiplication of durations: `d * time.Second` (durationcheck)"
	for _, k := range keys {
		ctx = context.WithValue(ctx, k, k) // want "nested context in loop (fatcontext)"
	}
	made := make([]int, 2)
	made = append(made, 1) // want "append to slice `made` with non-zero initialized length (makezero)"
	os.Args = nil          // want "reassigning variable Args in other package os (reassign)"
	_, _ = ctx, made
	err1 := work()
	if err1 != nil {
		return total, err1
	}
	err2 := work()
	if err2 != nil {
		return total, err1 // want "return a nil value error after check error (nilnesserr)"
	}
	return total, nil
}
