package diffeng

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/smm-h/strictspec/go/internal/diag"
	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/ir"
	"github.com/smm-h/strictspec/go/internal/render"
	"github.com/smm-h/strictspec/go/internal/schema"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
	"github.com/smm-h/strictspec/go/internal/write"
)

var (
	certProgOnce   sync.Once
	certProg       *ir.Program
	certSVProgOnce sync.Once
	certSVProg     *ir.Program
	adjProgOnce    sync.Once
	adjProg        *ir.Program
)

func certProgram() *ir.Program {
	certProgOnce.Do(func() { certProg = compileBuiltin(certSchemaTOML) })
	return certProg
}

func certSameVersionProgram() *ir.Program {
	certSVProgOnce.Do(func() { certSVProg = compileBuiltin(certSchemaSameVersionTOML) })
	return certSVProg
}

func adjProgram() *ir.Program {
	adjProgOnce.Do(func() { adjProg = compileBuiltin(adjudicationSchemaTOML) })
	return adjProg
}

func compileBuiltin(src string) *ir.Program {
	d, perr := tomldoc.Parse([]byte(src))
	if perr != nil {
		panic("diffeng: built-in schema unparseable: " + perr.Error())
	}
	s, diags := schema.ReadSchema(d.Root, "")
	if len(diags) > 0 {
		panic(fmt.Sprintf("diffeng: built-in schema fails the meta-schema: %v", diags))
	}
	return ir.Compile(s, nil)
}

// SelfValidate validates an emitted certificate against the built-in certificate
// schema (the shape IS a strictspec schema — dogfooding). BOTH modes self-validate:
// normal-mode certificates against certSchemaTOML, and same-version certificates
// (old_format_version == the "same-version" marker) against certSchemaSameVersionTOML.
// It is a self-check: a certificate that does not validate means the engine
// produced a malformed certificate (a bug), so it returns an error.
//
// DOCUMENTED EXCEPTION (appendix-certificates.md "Self-validation"): the certificate
// is NOT a gated document — it carries `certificate_format_version`, not
// `format_version` — so it cannot carry the version-gate field the executor
// requires. The gate field is SYNTHESIZED for the self-check only; the emitted
// certificate keeps the pinned shape. This is the one irreducible synthetic field
// (union modeling of old_format_version is not expressible; see cert.go), and it is
// documented rather than silent.
func SelfValidate(cert *Certificate) error {
	prog := certProgram()
	if _, isSameVersion := cert.OldFormatVersion.(string); isSameVersion {
		prog = certSameVersionProgram()
	}
	raw, err := json.Marshal(cert)
	if err != nil {
		return fmt.Errorf("diffeng: marshal certificate: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("diffeng: unmarshal certificate: %w", err)
	}
	m["format_version"] = prog.FormatVersion()
	augmented, err := json.Marshal(m)
	if err != nil {
		return err
	}
	wd, werr := write.New(doc.FormatJSON, augmented)
	if werr != nil {
		return fmt.Errorf("diffeng: reparse certificate: %w", werr)
	}
	diags := ir.Execute(prog, wd.Root(), ir.ExecOptions{Format: doc.FormatJSON})
	if len(diags) > 0 {
		return fmt.Errorf("diffeng: emitted certificate fails its own schema: %s", renderAll(diags))
	}
	return nil
}

func renderAll(diags []diag.Diagnostic) string {
	out := ""
	for i, d := range diags {
		if i > 0 {
			out += "; "
		}
		out += d.Code + " at " + d.Path.Render() + ": " + safeRenderMsg(d)
	}
	return out
}

func safeRenderMsg(d diag.Diagnostic) (msg string) {
	defer func() {
		if recover() != nil {
			msg = d.Code
		}
	}()
	return render.Render(d)
}
