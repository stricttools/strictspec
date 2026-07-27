package render

import (
	"strings"
	"testing"

	"github.com/smm-h/strictspec/go/internal/diag"
)

// slots is a small helper for building a slot map inline.
func slots(kv ...any) map[string]diag.Slot {
	m := map[string]diag.Slot{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1].(diag.Slot)
	}
	return m
}

// goldenCases are hand-authored expected strings derived directly from
// spec/appendix-error-codes.md (templates) and spec/appendix-rendering.md
// (rendering rules) — never generated from any target. They are the spec-derived
// oracle for byte-identical message text.
func TestRenderGolden(t *testing.T) {
	cases := []struct {
		name string
		diag diag.Diagnostic
		want string
	}{
		{
			name: "TYPE_NOT_INTEGER: string slot double-quoted",
			diag: diag.Diagnostic{
				Code:  "STRICTSPEC_TYPE_NOT_INTEGER",
				Path:  diag.NewPath(diag.Key{Name: "count"}),
				Slots: slots("got", diag.SlotString{S: "float"}),
			},
			want: `Expected an integer at $.count, got "float".`,
		},
		{
			name: "TYPE_MISMATCH: two string slots",
			diag: diag.Diagnostic{
				Code:  "STRICTSPEC_TYPE_MISMATCH",
				Path:  diag.NewPath(diag.Key{Name: "canvas"}),
				Slots: slots("expected", diag.SlotString{S: "record"}, "got", diag.SlotString{S: "array"}),
			},
			want: `Expected "record" at $.canvas, got "array".`,
		},
		{
			name: "KEY_UNKNOWN: no suggestion (empty)",
			diag: diag.Diagnostic{
				Code:  "STRICTSPEC_KEY_UNKNOWN",
				Path:  diag.NewPath(diag.Key{Name: "config"}),
				Slots: slots("key", diag.SlotString{S: "colour"}),
			},
			want: `Unknown key "colour" at $.config.`,
		},
		{
			name: "KEY_UNKNOWN: one suggestion",
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_KEY_UNKNOWN",
				Path: diag.NewPath(diag.Key{Name: "config"}),
				Slots: slots(
					"key", diag.SlotString{S: "colr"},
					"suggestion", diag.SlotSuggestion{Unknown: "colr", Candidates: []string{"color", "width", "height"}},
				),
			},
			want: `Unknown key "colr" at $.config. Did you mean color?`,
		},
		{
			name: "VALUE_NUM_TOO_SMALL: integer value slots",
			diag: diag.Diagnostic{
				Code:  "STRICTSPEC_VALUE_NUM_TOO_SMALL",
				Path:  diag.NewPath(diag.Key{Name: "age"}),
				Slots: slots("actual", diag.SlotValue{V: diag.IntVal(3)}, "limit", diag.SlotValue{V: diag.IntVal(18)}),
			},
			want: `Value 3 at $.age is below the minimum 18.`,
		},
		{
			name: "VALUE_STRING_TOO_LONG: int slots",
			diag: diag.Diagnostic{
				Code:  "STRICTSPEC_VALUE_STRING_TOO_LONG",
				Path:  diag.NewPath(diag.Key{Name: "bio"}),
				Slots: slots("actual", diag.SlotInt{N: 200}, "limit", diag.SlotInt{N: 64}),
			},
			want: `String at $.bio has 200 code points; maximum is 64.`,
		},
		{
			name: "VALUE_STRING_REGEX: value string quoted + regex pattern string quoted (A.7)",
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_VALUE_STRING_REGEX",
				Path: diag.NewPath(diag.Key{Name: "slug"}),
				Slots: slots(
					"actual", diag.SlotValue{V: diag.StringVal("Hello World")},
					"pattern", diag.SlotString{S: `^[a-z-]+$`},
				),
			},
			want: `String "Hello World" at $.slug does not match the required pattern "^[a-z-]+$".`,
		},
		{
			name: "TYPE_NOT_ENUM_MEMBER: list>3 truncated + two suggestions (distance ordering)",
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_TYPE_NOT_ENUM_MEMBER",
				Path: diag.NewPath(diag.Key{Name: "color"}),
				Slots: slots(
					"got", diag.SlotValue{V: diag.StringVal("gren")},
					"expected", diag.SlotList{Elems: []diag.Value{
						diag.StringVal("red"), diag.StringVal("green"), diag.StringVal("blue"), diag.StringVal("cyan"),
					}},
					"suggestion", diag.SlotSuggestion{Unknown: "gren", Candidates: []string{"red", "green", "blue", "cyan"}},
				),
			},
			want: `Value "gren" at $.color is not one of ["red", "green", "blue", ...]. Did you mean green or red?`,
		},
		{
			name: "GATE_UNSUPPORTED: version + identifier + string invocation + literal backticks",
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_GATE_UNSUPPORTED",
				Path: diag.NewPath(),
				Slots: slots(
					"got", diag.SlotVersion{V: 2},
					"schema", diag.SlotIdentifier{Name: "canvas"},
					"expected", diag.SlotVersion{V: 3},
					"migset", diag.SlotIdentifier{Name: "canvas_v2_v3"},
					"invocation", diag.SlotString{S: "strictspec migrate --schema canvas --to 3 doc.json"},
				),
			},
			want: "Document `format_version` is 2, but schema canvas accepts exactly 3 (migration set canvas_v2_v3). Run: \"strictspec migrate --schema canvas --to 3 doc.json\"",
		},
		{
			name: "INTRA_FORBIDDEN_WHEN: condition inserted verbatim (Part D), key quoted",
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_INTRA_FORBIDDEN_WHEN",
				Path: diag.NewPath(diag.Key{Name: "legacy"}),
				Slots: slots(
					"key", diag.SlotString{S: "legacy"},
					"condition", diag.SlotString{S: `mode == "strict"`},
				),
			},
			want: `Field "legacy" at $.legacy is forbidden when mode == "strict".`,
		},
		{
			name: "INTRA_EXACTLY_ONE_OF: two list slots",
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_INTRA_EXACTLY_ONE_OF",
				Path: diag.NewPath(diag.Key{Name: "payment"}),
				Slots: slots(
					"fields", diag.SlotList{Elems: []diag.Value{diag.StringVal("card"), diag.StringVal("bank")}},
					"actual", diag.SlotList{Elems: []diag.Value{diag.StringVal("card"), diag.StringVal("bank")}},
				),
			},
			want: `Exactly one of ["card", "bank"] must be present at $.payment; found ["card", "bank"].`,
		},
		{
			name: "NUM_SAFE_INTEGER: literal pipes in template preserved",
			diag: diag.Diagnostic{
				Code:  "STRICTSPEC_NUM_SAFE_INTEGER",
				Path:  diag.NewPath(diag.Key{Name: "id"}),
				Slots: slots("actual", diag.SlotValue{V: diag.IntVal(9007199254740993)}),
			},
			want: "Integer 9007199254740993 at $.id exceeds the safe-integer range (|n| >= 2^53) required by `safe_integers`.",
		},
		{
			name: "UNION_DISCRIMINATOR_UNKNOWN: value + list + suggestion",
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN",
				Path: diag.NewPath(diag.Key{Name: "shape"}),
				Slots: slots(
					"got", diag.SlotValue{V: diag.StringVal("circl")},
					"expected", diag.SlotList{Elems: []diag.Value{diag.StringVal("circle"), diag.StringVal("square")}},
					"suggestion", diag.SlotSuggestion{Unknown: "circl", Candidates: []string{"circle", "square"}},
				),
			},
			want: `Discriminator "circl" at $.shape is not one of ["circle", "square"]. Did you mean circle?`,
		},
		{
			name: "PARSE_JSONL_LINE_SYNTAX: path with JSONL anchor + int + string detail",
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX",
				Path: diag.NewPath().WithAnchor(3, 12),
				Slots: slots(
					"line", diag.SlotInt{N: 3},
					"detail", diag.SlotString{S: "unexpected end of input"},
				),
			},
			want: `JSONL parse error on line 3 at $@L3:12: "unexpected end of input".`,
		},
		{
			name: "MIGRATE_UNWRAP_NOT_SINGLETON: int slot",
			diag: diag.Diagnostic{
				Code:  "STRICTSPEC_MIGRATE_UNWRAP_NOT_SINGLETON",
				Path:  diag.NewPath(diag.Key{Name: "tags"}),
				Slots: slots("actual", diag.SlotInt{N: 3}),
			},
			want: `unwrap_singleton at $.tags requires a single-element array; found 3 elements.`,
		},
		{
			name: "ALIAS_BOTH_PRESENT: identifiers rendered bare",
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_ALIAS_BOTH_PRESENT",
				Path: diag.NewPath(diag.Key{Name: "node"}),
				Slots: slots(
					"alias", diag.SlotIdentifier{Name: "colour"},
					"canonical", diag.SlotIdentifier{Name: "color"},
				),
			},
			want: `Both colour and canonical color are present at $.node; provide exactly one.`,
		},
		{
			name: "TYPE_NOT_LITERAL: two integer value slots",
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_TYPE_NOT_LITERAL",
				Path: diag.NewPath(diag.Key{Name: "version"}),
				Slots: slots(
					"expected", diag.SlotValue{V: diag.IntVal(1)},
					"got", diag.SlotValue{V: diag.IntVal(2)},
				),
			},
			want: `Expected the literal 1 at $.version, got 2.`,
		},
		{
			name: "INTRA_UNIQUE_BY: value + string field + string normalization",
			diag: diag.Diagnostic{
				Code: "STRICTSPEC_INTRA_UNIQUE_BY",
				Path: diag.NewPath(diag.Key{Name: "users"}),
				Slots: slots(
					"value", diag.SlotValue{V: diag.StringVal("alice")},
					"field", diag.SlotString{S: "username"},
					"normalization", diag.SlotString{S: "case-fold"},
				),
			},
			want: `Duplicate value "alice" for unique-by "username" at $.users (normalization: "case-fold").`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := Render(tt.diag); got != tt.want {
				t.Errorf("Render() mismatch\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// --- Value rendering (A.1 / A.3 / A.5) ---------------------------------------

func TestRenderValue(t *testing.T) {
	tests := []struct {
		name string
		v    diag.Value
		want string
	}{
		{"int", diag.IntVal(1000), "1000"},
		{"negative int", diag.IntVal(-42), "-42"},
		{"int negative zero renders 0", diag.IntVal(0), "0"},
		{"float from lexeme unchanged (exponent preserved)", diag.FloatVal{Lexeme: "1e3", HasLexeme: true}, "1e3"},
		{"float from lexeme 5.0", diag.FloatVal{Lexeme: "5.0", HasLexeme: true}, "5.0"},
		{"constructed float gains .0", diag.FloatVal{F: 5}, "5.0"},
		{"constructed negative zero float keeps sign", diag.FloatVal{F: negZero()}, "-0.0"},
		{"number scalar int-class from lexeme", diag.NumberVal{Lexeme: "007", IntClass: true}, "007"},
		{"number scalar float-class from lexeme", diag.NumberVal{Lexeme: "1.50", IntClass: false}, "1.50"},
		{"string quoted and escaped", diag.StringVal("line\nbreak"), `"line\nbreak"`},
		{"bool true", diag.BoolVal(true), "true"},
		{"bool false", diag.BoolVal(false), "false"},
		{"null", diag.NullVal{}, "null"},
		{"date", diag.DateVal("2026-07-27"), "2026-07-27"},
		{"time", diag.TimeVal("13:37:00"), "13:37:00"},
		{"datetime offset preserved verbatim", diag.DatetimeVal("2026-07-27T13:37:00+00:00"), "2026-07-27T13:37:00+00:00"},
		{"empty array", diag.ArrayVal{}, "[]"},
		{"array of 3", diag.ArrayVal{diag.IntVal(1), diag.IntVal(2), diag.IntVal(3)}, "[1, 2, 3]"},
		{"array truncated to 3", diag.ArrayVal{diag.IntVal(1), diag.IntVal(2), diag.IntVal(3), diag.IntVal(4)}, "[1, 2, 3, ...]"},
		{"empty record", diag.RecordVal{}, "{}"},
		{
			"record with ident and non-ident keys",
			diag.RecordVal{Keys: []string{"a", "weird key"}, Vals: []diag.Value{diag.IntVal(1), diag.BoolVal(true)}},
			`{a: 1, "weird key": true}`,
		},
		{
			"record truncated to 3 pairs",
			diag.RecordVal{Keys: []string{"a", "b", "c", "d"}, Vals: []diag.Value{diag.IntVal(1), diag.IntVal(2), diag.IntVal(3), diag.IntVal(4)}},
			"{a: 1, b: 2, c: 3, ...}",
		},
		{
			"nesting beyond 2 levels renders sentinel",
			diag.ArrayVal{diag.ArrayVal{diag.ArrayVal{diag.IntVal(1)}}},
			"[[[...]]]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderValue(tt.v); got != tt.want {
				t.Errorf("renderValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func negZero() float64 {
	z := 0.0
	return -z
}

// --- String truncation boundary (A.4) ----------------------------------------

func TestStringTruncationBoundary(t *testing.T) {
	// 63 code points: not truncated.
	s63 := strings.Repeat("a", 63)
	if got := renderQuotedString(s63); got != `"`+s63+`"` {
		t.Errorf("63 cp: got %q", got)
	}
	// 64 code points: exactly at the limit, NOT truncated (limit is >64).
	s64 := strings.Repeat("a", 64)
	if got := renderQuotedString(s64); got != `"`+s64+`"` {
		t.Errorf("64 cp: got %q, want no ellipsis", got)
	}
	// 65 code points: truncated to 64 + "...".
	s65 := strings.Repeat("a", 65)
	want65 := `"` + strings.Repeat("a", 64) + `..."`
	if got := renderQuotedString(s65); got != want65 {
		t.Errorf("65 cp: got %q, want %q", got, want65)
	}
	// Multi-byte runes are counted by code point, not byte. 65 emoji (4 bytes
	// each) truncate to the first 64.
	emoji := strings.Repeat("\U0001F600", 65)
	first64 := strings.Repeat("\U0001F600", 64)
	wantEmoji := `"` + first64 + `..."`
	if got := renderQuotedString(emoji); got != wantEmoji {
		t.Errorf("65 emoji: got %q, want %q", got, wantEmoji)
	}
	// Truncation counts SOURCE code points and escapes whole characters, so an
	// escape is never split: 65 newlines -> first 64 as \n then ellipsis.
	nl := strings.Repeat("\n", 65)
	wantNL := `"` + strings.Repeat(`\n`, 64) + `..."`
	if got := renderQuotedString(nl); got != wantNL {
		t.Errorf("65 newlines: got %q, want %q", got, wantNL)
	}
}

// --- Programmer-error panics -------------------------------------------------

func TestRenderPanicsUnknownCode(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on unknown code")
		}
	}()
	Render(diag.Diagnostic{Code: "STRICTSPEC_NOT_A_REAL_CODE", Path: diag.NewPath()})
}

func TestRenderPanicsMissingSlot(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on missing required slot")
		}
	}()
	// TYPE_NOT_INTEGER requires {got}; omit it.
	Render(diag.Diagnostic{Code: "STRICTSPEC_TYPE_NOT_INTEGER", Path: diag.NewPath(diag.Key{Name: "x"})})
}

func TestRenderPanicsUnknownSlot(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on unknown slot binding")
		}
	}()
	Render(diag.Diagnostic{
		Code:  "STRICTSPEC_TYPE_NOT_INTEGER",
		Path:  diag.NewPath(diag.Key{Name: "x"}),
		Slots: slots("got", diag.SlotString{S: "float"}, "bogus", diag.SlotString{S: "x"}),
	})
}

func TestRenderPanicsOnManualPathBinding(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when {path} is bound manually")
		}
	}()
	Render(diag.Diagnostic{
		Code:  "STRICTSPEC_TYPE_NOT_INTEGER",
		Path:  diag.NewPath(diag.Key{Name: "x"}),
		Slots: slots("got", diag.SlotString{S: "float"}, "path", diag.SlotPath{P: diag.NewPath()}),
	})
}
