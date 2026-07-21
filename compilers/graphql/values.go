package graphql

import (
	"encoding/json"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/dexpace/morphic/ir"
)

// maxValueDepth bounds value recursion (styleguide bounded-recursion rule):
// values nested deeper than this are pathological input, not a schema the
// compiler is expected to lower.
const maxValueDepth = 128

// irValue converts an SDL literal into an ir.Value on the Values channel
// (ir-design §6). Numeric literals keep their exact source text (the no-float64
// escape), enum literals become symbols distinct from strings, and object member
// order is preserved. A bad numeric literal yields a diagnostic and a null.
func (l *lowerer) irValue(v *ast.Value, prov ir.Provenance) *ir.Value {
	if v == nil {
		return nil
	}
	out := l.irValueAt(v, prov, 0)
	return &out
}

// irValueAt is irValue with an explicit recursion depth counter.
func (l *lowerer) irValueAt(v *ast.Value, prov ir.Provenance, depth int) ir.Value {
	if depth > maxValueDepth {
		l.diags = append(l.diags, diagf(ir.SeverityWarning, codeDegradedConstruct, prov,
			"value nesting exceeds %d; lowered as null", maxValueDepth))
		return ir.Value{Kind: ir.ValueNull}
	}
	switch v.Kind {
	case ast.NullValue:
		return ir.Value{Kind: ir.ValueNull}
	case ast.BooleanValue:
		return ir.Value{Kind: ir.ValueBool, Bool: v.Raw == "true"}
	case ast.StringValue, ast.BlockValue:
		return ir.Value{Kind: ir.ValueString, Str: v.Raw}
	case ast.IntValue, ast.FloatValue:
		return l.numericValue(v, prov)
	case ast.EnumValue:
		return ir.Value{Kind: ir.ValueSymbol, Str: v.Raw}
	case ast.ListValue:
		return l.listValue(v, prov, depth)
	case ast.ObjectValue:
		return l.objectValue(v, prov, depth)
	default:
		l.diags = append(l.diags, diagf(ir.SeverityWarning, codeDegradedConstruct, prov,
			"unsupported value kind %d; lowered as null", v.Kind))
		return ir.Value{Kind: ir.ValueNull}
	}
}

// numericValue lowers an int/float literal as an exact BigVal, diagnosing a
// literal that is not a valid decimal.
func (l *lowerer) numericValue(v *ast.Value, prov ir.Provenance) ir.Value {
	big, err := ir.NewBigVal(v.Raw)
	if err != nil {
		l.diags = append(l.diags, diagf(ir.SeverityWarning, codeInvalidValue, prov,
			"numeric literal %q is not a valid decimal", v.Raw))
		return ir.Value{Kind: ir.ValueNull}
	}
	return ir.Value{Kind: ir.ValueNumber, Num: big}
}

// listValue lowers a list literal, preserving element order.
func (l *lowerer) listValue(v *ast.Value, prov ir.Provenance, depth int) ir.Value {
	items := make([]ir.Value, 0, len(v.Children))
	for _, c := range v.Children {
		items = append(items, l.irValueAt(c.Value, prov, depth+1))
	}
	return ir.Value{Kind: ir.ValueList, List: items}
}

// objectValue lowers an object literal, preserving member order (member order
// carries meaning, so it is a slice, never a map).
func (l *lowerer) objectValue(v *ast.Value, prov ir.Provenance, depth int) ir.Value {
	fields := make([]ir.Field, 0, len(v.Children))
	for _, c := range v.Children {
		fields = append(fields, ir.Field{Name: c.Name, Value: l.irValueAt(c.Value, prov, depth+1)})
	}
	return ir.Value{Kind: ir.ValueObject, Object: fields}
}

// valueJSON renders an SDL literal as verbatim JSON for the Extensions escape
// hatch. Enum members render as JSON strings (JSON has no symbol); numbers keep
// their exact source text. It shares the maxValueDepth bound with irValue.
func valueJSON(v *ast.Value) json.RawMessage {
	return valueJSONAt(v, 0)
}

// valueJSONAt is valueJSON with an explicit recursion depth counter.
func valueJSONAt(v *ast.Value, depth int) json.RawMessage {
	if v == nil || depth > maxValueDepth {
		return json.RawMessage("null")
	}
	switch v.Kind {
	case ast.NullValue:
		return json.RawMessage("null")
	case ast.BooleanValue:
		return jsonBool(v.Raw)
	case ast.IntValue, ast.FloatValue:
		return jsonNumber(v.Raw)
	case ast.StringValue, ast.BlockValue, ast.EnumValue, ast.Variable:
		return jsonString(v)
	case ast.ListValue:
		return jsonList(v, depth)
	case ast.ObjectValue:
		return jsonObjectValue(v, depth)
	default:
		return json.RawMessage("null")
	}
}

// jsonBool renders a boolean literal, defaulting a malformed raw to false.
func jsonBool(raw string) json.RawMessage {
	if raw == "true" {
		return json.RawMessage("true")
	}
	return json.RawMessage("false")
}

// jsonNumber renders a numeric literal verbatim when it is valid JSON, else as a
// quoted string so the output stays well-formed.
func jsonNumber(raw string) json.RawMessage {
	if _, err := ir.NewBigVal(raw); err != nil {
		quoted, _ := json.Marshal(raw)
		return quoted
	}
	return json.RawMessage(raw)
}

// jsonString renders a string, block, enum, or variable literal as a JSON
// string; a variable keeps its leading "$".
func jsonString(v *ast.Value) json.RawMessage {
	s := v.Raw
	if v.Kind == ast.Variable {
		s = "$" + v.Raw
	}
	quoted, _ := json.Marshal(s)
	return quoted
}

// jsonList renders a list literal as a JSON array.
func jsonList(v *ast.Value, depth int) json.RawMessage {
	var b strings.Builder
	b.WriteByte('[')
	for i, c := range v.Children {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(valueJSONAt(c.Value, depth+1))
	}
	b.WriteByte(']')
	return json.RawMessage(b.String())
}

// jsonObjectValue renders an object literal as a JSON object, preserving member
// order.
func jsonObjectValue(v *ast.Value, depth int) json.RawMessage {
	var b strings.Builder
	b.WriteByte('{')
	for i, c := range v.Children {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(c.Name)
		b.Write(key)
		b.WriteByte(':')
		b.Write(valueJSONAt(c.Value, depth+1))
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
}

// stringArg returns the string value of a directive argument, or "" when absent.
func stringArg(d *ast.Directive, name string) string {
	arg := d.Arguments.ForName(name)
	if arg == nil || arg.Value == nil {
		return ""
	}
	return arg.Value.Raw
}
