package schema

import (
	"testing"
)

const unionSchemaHead = "name = \"u\"\nmeta_version = 1\nformat_version = 1\ndocument_syntax = \"toml\"\nroot = \"Root\"\n\n[types.Root]\ntype = \"record\"\n[types.Root.fields.v]\ntype = \"Either\"\nrequired = true\n\n[types.Either]\ntype = \"node-kind-union\"\n"

// unionDiagnostics reads a schema whose Either union has the given arms
// (TOML arm tables appended to the head) plus extra named types, resolves
// it, and returns its authoring diagnostics.
func unionDiagnostics(t *testing.T, arms string) []diagCode {
	t.Helper()
	files := FileSet{"u.schema.toml": unionSchemaHead + arms}
	s, diags, err := ParseFrom(files, "u.schema.toml")
	if err != nil {
		t.Fatal(err)
	}
	diags = append(diags, ResolveImportsFrom(s, files)...)
	out := make([]diagCode, len(diags))
	for i, d := range diags {
		out[i] = diagCode{d.Code, d.Path.Render()}
	}
	return out
}

// The runtime selects a node-kind union's arm by the input's node category
// (scalar, record, or array), so two arms of one category can never both be
// reached: the schema is refused at its union, as the meta-schema requires.
func TestANodeKindUnionWithTwoArmsOfOneCategoryIsRefused(t *testing.T) {
	ambiguous := map[string]string{
		"string and boolean":      "[types.Either.arms.name]\ntype = \"string\"\n[types.Either.arms.off]\ntype = \"boolean\"\n",
		"string and a named enum": "[types.Either.arms.name]\ntype = \"string\"\n[types.Either.arms.mode]\ntype = \"Mode\"\n\n[types.Mode]\ntype = \"enum\"\nvalues = [\"a\", \"b\"]\n",
		"record and map":          "[types.Either.arms.rec]\ntype = \"Rec\"\n[types.Either.arms.dict]\ntype = \"Dict\"\n\n[types.Rec]\ntype = \"record\"\n[types.Rec.fields.x]\ntype = \"string\"\nrequired = true\n\n[types.Dict]\ntype = \"map\"\n[types.Dict.value]\ntype = \"string\"\n",
	}
	for name, arms := range ambiguous {
		t.Run(name, func(t *testing.T) {
			diags := unionDiagnostics(t, arms)
			if len(diags) != 1 || diags[0].code != "STRICTSPEC_SCHEMA_NODE_KIND_UNION_AMBIGUOUS" || diags[0].path != "$.types.Either" {
				t.Fatalf("an ambiguous node-kind union was not refused at $.types.Either: %v", diags)
			}
		})
	}
	distinct := "[types.Either.arms.name]\ntype = \"string\"\n[types.Either.arms.rec]\ntype = \"Rec\"\n[types.Either.arms.list]\ntype = \"array\"\n[types.Either.arms.list.item]\ntype = \"string\"\n\n[types.Rec]\ntype = \"record\"\n[types.Rec.fields.x]\ntype = \"string\"\nrequired = true\n"
	if diags := unionDiagnostics(t, distinct); len(diags) != 0 {
		t.Fatalf("a union of a scalar, a record, and an array was refused: %v", diags)
	}
}
