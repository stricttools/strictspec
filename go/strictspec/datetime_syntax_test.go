package strictspec

import "testing"

const datetimeSchemaHead = `
name = "moment"
meta_version = 1
format_version = 1
role = "schema"
root = "Moment"

[types.Moment]
type = "record"
[types.Moment.fields.day]
type = "date"
required = true
[types.Moment.fields.clock]
type = "time"
required = true
[types.Moment.fields.at]
type = "datetime"
datetime_kind = "offset"
required = true
`

func compileDatetimeSchema(t *testing.T, syntax string) *Program {
	t.Helper()
	src := "document_syntax = \"" + syntax + "\"\n" + datetimeSchemaHead
	p, err := CompileEmbedded(map[string]string{"moment.schema.toml": src}, "moment.schema.toml")
	if err != nil {
		t.Fatalf("CompileEmbedded: %v", err)
	}
	return p
}

// A TOML document carries datetimes as native TOML lexemes; only JSON binds RFC
// 3339 strings. A quoted string in a TOML datetime-typed field is a type error.
func TestTOMLDatetimeFieldsRefuseQuotedStrings(t *testing.T) {
	p := compileDatetimeSchema(t, "toml")
	res := p.Validate([]byte("format_version = 1\nday = \"2026-10-07\"\nclock = \"13:37:00\"\nat = \"2026-10-07T13:37:00+00:00\"\n"), "toml")
	want := []string{"STRICTSPEC_TYPE_NOT_DATE", "STRICTSPEC_TYPE_NOT_TIME", "STRICTSPEC_TYPE_NOT_DATETIME"}
	if res.Valid || len(res.Diagnostics) != len(want) {
		t.Fatalf("got valid=%v diagnostics %+v, want %v", res.Valid, res.Diagnostics, want)
	}
	for i, w := range want {
		if res.Diagnostics[i].Code != w {
			t.Errorf("diag[%d] code = %s, want %s", i, res.Diagnostics[i].Code, w)
		}
	}
}

func TestTOMLDatetimeFieldsAcceptNativeLexemes(t *testing.T) {
	p := compileDatetimeSchema(t, "toml")
	res := p.Validate([]byte("format_version = 1\nday = 2026-10-07\nclock = 13:37:00\nat = 2026-10-07T13:37:00+00:00\n"), "toml")
	if !res.Valid {
		t.Fatalf("native TOML datetimes refused: %+v", res.Diagnostics)
	}
}

func TestJSONDatetimeFieldsAcceptRFC3339Strings(t *testing.T) {
	p := compileDatetimeSchema(t, "json")
	res := p.Validate([]byte(`{"format_version":1,"day":"2026-10-07","clock":"13:37:00","at":"2026-10-07T13:37:00+00:00"}`), "json")
	if !res.Valid {
		t.Fatalf("JSON RFC 3339 strings refused: %+v", res.Diagnostics)
	}
}
