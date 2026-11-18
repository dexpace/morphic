// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement

import (
	yaml "go.yaml.in/yaml/v3"
)

// The readers below return what a slot holds, and nothing where the slot is
// nil, as it is where the schema writes no keyword of it, or the schema is
// nil, as a position naming none is. A reader of a group of keywords returns
// the group's value, and a reader of one keyword its value, so a caller reads
// a keyword without stepping past the slot it sits in.

// ArrayKeywords returns what s's array keywords say, the zero Array where it
// writes none.
func (s *Schema) ArrayKeywords() Array {
	if s == nil || s.Array == nil {
		return Array{}
	}
	return *s.Array
}

// ObjectKeywords returns what s's object keywords say, the zero Object where
// it writes none.
func (s *Schema) ObjectKeywords() Object {
	if s == nil || s.Object == nil {
		return Object{}
	}
	return *s.Object
}

// TextKeywords returns what s's string keywords say, the zero Text where it
// writes none.
func (s *Schema) TextKeywords() Text {
	if s == nil || s.Text == nil {
		return Text{}
	}
	return *s.Text
}

// ChoiceKeywords returns what s's oneOf and anyOf list, the zero Choice where
// it writes neither.
func (s *Schema) ChoiceKeywords() Choice {
	if s == nil || s.Choice == nil {
		return Choice{}
	}
	return *s.Choice
}

// LogicKeywords returns what s's in-place applicators say, the zero Logic
// where it writes none.
func (s *Schema) LogicKeywords() Logic {
	if s == nil || s.Logic == nil {
		return Logic{}
	}
	return *s.Logic
}

// Types returns the types s's `type` names, as written, nil for none.
func (s *Schema) Types() []Kind {
	if s == nil || s.Kinds == nil {
		return nil
	}
	return s.Kinds.Types
}

// Enum returns the members s's `enum` lists, nil where it writes none and
// empty where it writes an empty list.
func (s *Schema) Enum() []*yaml.Node {
	if s == nil || s.Values == nil {
		return nil
	}
	return s.Values.Enum
}

// Const returns the value s's `const` holds, nil where it writes none.
func (s *Schema) Const() *yaml.Node {
	if s == nil || s.Values == nil {
		return nil
	}
	return s.Values.Const
}

// FormatName returns the format s names, "" where it names none.
func (s *Schema) FormatName() string {
	if s == nil || s.Format == nil {
		return ""
	}
	return s.Format.Name
}

// PrefixItems returns the schemas s writes `prefixItems` as, nil for none.
func (s *Schema) PrefixItems() []Sub {
	if s == nil || s.Array == nil {
		return nil
	}
	return s.Array.PrefixItems
}

// Discriminator returns the discriminator s writes, nil for none.
func (s *Schema) Discriminator() *Discriminator {
	if s == nil || s.Object == nil {
		return nil
	}
	return s.Object.Discriminator
}

// Default returns the default s writes, nil for none.
func (s *Schema) Default() *yaml.Node {
	if s == nil || s.Notes == nil {
		return nil
	}
	return s.Notes.Default
}

// XML returns the xml object s writes, nil for none.
func (s *Schema) XML() *XML {
	if s == nil || s.Notes == nil {
		return nil
	}
	return s.Notes.XML
}

// ExternalDocs returns the externalDocs object s writes, nil for none.
func (s *Schema) ExternalDocs() *ExternalDocs {
	if s == nil || s.Notes == nil {
		return nil
	}
	return s.Notes.ExternalDocs
}
