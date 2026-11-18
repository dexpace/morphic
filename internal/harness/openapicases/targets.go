// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

// target is a schema a construct is said of: its name, its body, and two
// distinct instances it holds valid, which a construct whose value is an
// instance writes.
type target struct {
	name      string
	body      object
	instances [2]raw
}

// The targets' names, which constructs.go's rows name the targets they are
// said of by.
const (
	objectTarget    = "object"
	stringTarget    = "string"
	integerTarget   = "integer"
	arrayTarget     = "array"
	enumTarget      = "enum"
	unionTarget     = "union"
	formattedTarget = "formatted"
	mapTarget       = "map"
)

// narrowed returns the target name narrows, whose cases the gate runs in its
// place, or "" for a target the gate runs: an enum and a formatted string are
// strings narrowed by a value set or a format, and a map an object narrowed to
// one value type. Each repeats its target's findings with the same signature,
// so a nightly run records them under that target.
func narrowed(name string) string {
	switch name {
	case enumTarget, formattedTarget:
		return stringTarget
	case mapTarget:
		return objectTarget
	default:
		return ""
	}
}

// targets returns the targets, in the order the target axis lists them.
func targets() []target {
	typed := func(t string, more ...member) object { return append(object{{"type", t}}, more...) }
	return []target{
		{objectTarget, typed("object", member{"properties", raw(`{"a": {"type": "string"}}`)}),
			[2]raw{`{"a": "x"}`, `{"a": "y"}`}},
		{stringTarget, typed("string"), [2]raw{`"a"`, `"z"`}},
		{integerTarget, typed("integer"), [2]raw{`1`, `2`}},
		{arrayTarget, typed("array", member{"items", raw(`{"type": "string"}`)}), [2]raw{`["a"]`, `["z"]`}},
		{enumTarget, typed("string", member{"enum", raw(`["x", "y"]`)}), [2]raw{`"x"`, `"y"`}},
		{unionTarget, object{{"oneOf", raw(`[{"type": "string"}, {"type": "integer"}]`)}}, [2]raw{`"a"`, `1`}},
		{formattedTarget, typed("string", member{"format", "uuid"}),
			[2]raw{`"00000000-0000-0000-0000-000000000001"`, `"00000000-0000-0000-0000-000000000002"`}},
		{mapTarget, typed("object", member{"additionalProperties", raw(`{"type": "string"}`)}),
			[2]raw{`{"k": "a"}`, `{"k": "z"}`}},
	}
}
