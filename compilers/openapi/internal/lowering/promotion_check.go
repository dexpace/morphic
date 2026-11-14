// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// extensionPrefix starts every vendor extension's name.
const extensionPrefix = "x-"

// Targets returns the vocabulary, every target a policy may name, sorted.
func Targets() []ExtensionTarget {
	return []ExtensionTarget{
		TargetDeprecation, TargetDeprecationMessage, TargetDeprecationRemovalDate,
		TargetDeprecationRemovalVersion, TargetDeprecationReplacedBy, TargetDeprecationSince,
		TargetEnumOpen, TargetTokenEndpointAuthMethods,
		TargetPaginationCursor, TargetPaginationItems, TargetPaginationLimit, TargetPaginationNextCursor,
		TargetPaginationNextLink, TargetPaginationOffset, TargetPaginationPage,
		TargetPaginationTotalCount, TargetPaginationTotalPages, TargetAllowReserved,
		TargetRedaction, TargetServerName, TargetStability, TargetUnionOpen,
	}
}

// ListsValues reports whether a source of target may list the values that
// state its fact (ExtensionSource.Values): a target stating that something is
// so, or marking where a page walk's value sits, and not one reading a text, a
// name or a tier, which a list of values could only narrow.
func ListsValues(target ExtensionTarget) bool {
	switch target {
	case TargetDeprecation, TargetEnumOpen, TargetUnionOpen, TargetRedaction, TargetAllowReserved:
		return true
	default:
		return isPaginationTarget(target)
	}
}

// Check refuses a policy that would read less than it says: a target outside
// the vocabulary, a source naming no extension, values listed for a target
// that reads none, an owned prefix that is no extension's, or a mapping beside
// Disabled, which reads nothing. A refusal is the caller's mistake and not the
// document's, so it is an error and no diagnostic.
func (p ExtensionPromotions) Check() error {
	if p.Disabled && (len(p.Targets) > 0 || len(p.Prefixes) > 0) {
		return errors.New("promotions: disabled, so the targets and prefixes named would read nothing")
	}
	for _, target := range slices.Sorted(maps.Keys(p.Targets)) {
		if err := checkTarget(target, p.Targets[target]); err != nil {
			return err
		}
	}
	for _, prefix := range p.Prefixes {
		if len(prefix) <= len(extensionPrefix) || !strings.HasPrefix(prefix, extensionPrefix) {
			return fmt.Errorf("promotions: prefix %q: want %q followed by the start of an extension name", prefix,
				extensionPrefix)
		}
	}
	return nil
}

// checkTarget refuses target, or one of the sources it reads.
func checkTarget(target ExtensionTarget, sources []ExtensionSource) error {
	if !slices.Contains(Targets(), target) {
		return fmt.Errorf("promotions: target %q is none the compiler reads (known: %s)", target, targetList())
	}
	for _, src := range sources {
		if !strings.HasPrefix(src.Key, extensionPrefix) {
			return fmt.Errorf("promotions: target %q: source key %q names no extension, which starts %q", target,
				src.Key, extensionPrefix)
		}
		if len(src.Values) > 0 && !ListsValues(target) {
			return fmt.Errorf("promotions: target %q reads no listed values, so source %q may list none", target, src)
		}
	}
	return nil
}

// targetList spells the vocabulary for a refusal.
func targetList() string {
	names := make([]string, 0, len(Targets()))
	for _, t := range Targets() {
		names = append(names, string(t))
	}
	return strings.Join(names, ", ")
}
