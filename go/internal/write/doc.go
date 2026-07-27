package write

import (
	"fmt"
	"sort"

	"github.com/smm-h/strictspec/go/internal/doc"
	"github.com/smm-h/strictspec/go/internal/jsondoc"
	"github.com/smm-h/strictspec/go/internal/tomldoc"
)

// Doc is a byte-splicing editor over one source document (a single JSON or TOML
// value; a JSONL stream is edited line-by-line, one Doc per line). It holds the
// CURRENT source bytes and their parse; every Splice applies to the current
// bytes and re-parses, so node spans are always valid and edits never overlap.
// Untouched bytes carry through verbatim — this is the lexeme-retention guarantee
// (untouched values serialize byte-identically within the backend).
type Doc struct {
	format doc.Format
	src    []byte
	root   doc.Node
}

// Edit is one byte-range replacement against the CURRENT source bytes: the
// half-open range [Start, End) is replaced by Repl. Callers compute Start/End
// from doc.Node / doc.Entry spans (which are byte offsets into the current
// bytes).
type Edit struct {
	Start int
	End   int
	Repl  []byte
}

// New parses source bytes into a Doc. JSONL is parsed with JSON semantics (a
// JSONL line IS a JSON document); callers split the stream first.
func New(format doc.Format, src []byte) (*Doc, error) {
	d := &Doc{format: format, src: append([]byte(nil), src...)}
	if err := d.reparse(); err != nil {
		return nil, err
	}
	return d, nil
}

// Format returns the document's surface format.
func (d *Doc) Format() doc.Format { return d.format }

// Bytes returns a copy of the current source bytes.
func (d *Doc) Bytes() []byte { return append([]byte(nil), d.src...) }

// Root returns the current parsed root node.
func (d *Doc) Root() doc.Node { return d.root }

func (d *Doc) reparse() error {
	switch d.format {
	case doc.FormatTOML:
		parsed, perr := tomldoc.Parse(d.src)
		if perr != nil {
			return perr
		}
		d.root = parsed.Root
	default: // JSON and JSONL lines
		parsed, perr := jsondoc.Parse(d.src)
		if perr != nil {
			return perr
		}
		d.root = parsed.Root
	}
	return nil
}

// Splice applies edits to the current bytes and re-parses. Edits must not
// overlap; they are applied right-to-left so earlier offsets stay valid.
func (d *Doc) Splice(edits []Edit) error {
	if len(edits) == 0 {
		return nil
	}
	sorted := append([]Edit(nil), edits...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start > sorted[j].Start })
	// Overlap check (in the right-to-left order, each edit's Start must be >= the
	// previous edit's End... but we iterate descending, so check ascending pairs).
	for i := 0; i+1 < len(sorted); i++ {
		if sorted[i+1].End > sorted[i].Start {
			return fmt.Errorf("write: overlapping edits at [%d,%d) and [%d,%d)",
				sorted[i+1].Start, sorted[i+1].End, sorted[i].Start, sorted[i].End)
		}
	}
	out := d.src
	for _, e := range sorted {
		if e.Start < 0 || e.End > len(out) || e.Start > e.End {
			return fmt.Errorf("write: edit range [%d,%d) out of bounds (len %d)", e.Start, e.End, len(out))
		}
		next := make([]byte, 0, len(out)-(e.End-e.Start)+len(e.Repl))
		next = append(next, out[:e.Start]...)
		next = append(next, e.Repl...)
		next = append(next, out[e.End:]...)
		out = next
	}
	d.src = out
	return d.reparse()
}
