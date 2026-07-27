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
	certProgOnce sync.Once
	certProg     *ir.Program
	adjProgOnce  sync.Once
	adjProg      *ir.Program
)

func certProgram() *ir.Program {
	certProgOnce.Do(func() { certProg = compileBuiltin(certSchemaTOML) })
	return certProg
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

// SelfValidate validates an emitted NORMAL-mode certificate against the built-in
// certificate schema (the shape IS a strictspec schema — dogfooding). It is a
// self-check: a certificate that does not validate means the engine produced a
// malformed certificate (a bug), so it returns an error. Same-version
// certificates carry the "same-version" marker (a distinct shape) and are not
// self-validated here.
func SelfValidate(cert *Certificate) error {
	if _, isSameVersion := cert.OldFormatVersion.(string); isSameVersion {
		return nil // same-version marker; distinct shape, not self-validated
	}
	raw, err := json.Marshal(cert)
	if err != nil {
		return fmt.Errorf("diffeng: marshal certificate: %w", err)
	}
	// The certificate has no `format_version` field (it uses
	// certificate_format_version). Inject the gate field for the self-check only;
	// the emitted certificate keeps the pinned shape.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("diffeng: unmarshal certificate: %w", err)
	}
	m["format_version"] = certProgram().FormatVersion()
	augmented, err := json.Marshal(m)
	if err != nil {
		return err
	}
	wd, werr := write.New(doc.FormatJSON, augmented)
	if werr != nil {
		return fmt.Errorf("diffeng: reparse certificate: %w", werr)
	}
	diags := ir.Execute(certProgram(), wd.Root(), ir.ExecOptions{Format: doc.FormatJSON})
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
