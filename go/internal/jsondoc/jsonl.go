package jsondoc

import (
	"bufio"
	"io"

	"github.com/smm-h/strictspec/go/internal/doc"
)

// JSONL framing rules, pinned by spec/DESIGN.md ("Document model" bullet on
// JSONL) and the parse-error catalogue in spec/appendix-error-codes.md:
//
//   - One JSON document per line. Lines are split on LF only ("LF-only
//     splitting"); a trailing LF terminates the final line rather than starting
//     an empty one, and empty input yields zero documents.
//   - Trailing CR invalidates the line: a line whose last byte (before the LF)
//     is '\r' is a hard error (STRICTSPEC_PARSE_JSONL_TRAILING_CR) — CRLF
//     endings are rejected, JSONL is LF-only.
//   - A blank line (empty or whitespace-only) is a hard error
//     (STRICTSPEC_PARSE_JSONL_BLANK_LINE); it is NOT skipped. This is the pinned
//     edge case from primitives appendix item 9.
//   - A final line without a trailing LF is valid (the other pinned edge case):
//     the LF is a terminator, not a required suffix.
//   - Positions are GLOBAL: line numbers and byte offsets are relative to the
//     whole input, so a value on line 3 reports its true global byte offset and
//     diagnostics can carry @L<line>:<offset> anchors.
//   - Duplicate keys within a line are the same hard error as in JSON.

// ParseLines parses JSONL bytes into one doc.Document per line. It stops at the
// first framing or parse error, returning a nil slice and the typed
// *doc.ParseError (whose Position is global). On success it returns the ordered
// documents and a nil error; empty input yields an empty, non-nil-safe slice.
//
// ParseLines is defined in terms of ParseStream so the slice and streaming APIs
// share one parser and one set of framing rules — they can never diverge.
func ParseLines(src []byte) ([]*doc.Document, *doc.ParseError) {
	var docs []*doc.Document
	perr, ioErr := ParseStream(newByteReader(src), func(d *doc.Document) {
		docs = append(docs, d)
	})
	if ioErr != nil {
		// A byteReader never returns a non-EOF error, so this is unreachable for
		// the in-memory path; surface it defensively rather than dropping it.
		return nil, &doc.ParseError{Format: doc.FormatJSONL, Message: ioErr.Error()}
	}
	if perr != nil {
		return nil, perr
	}
	return docs, nil
}

// ParseStream parses JSONL from an io.Reader, invoking emit once per successfully
// parsed line-document in order. Memory is bounded by the largest line, never the
// whole stream: lines are read one at a time with a growing buffer (no fixed
// line-length limit).
//
// It returns two independent failures: a *doc.ParseError for a framing or JSON
// error (with a global position), and a plain error for an underlying io.Reader
// failure (never io.EOF). At most one is non-nil. Parsing stops at the first
// error of either kind.
func ParseStream(r io.Reader, emit func(*doc.Document)) (*doc.ParseError, error) {
	br := bufio.NewReader(r)
	lineNo := 0
	offset := 0
	for {
		chunk, rerr := br.ReadBytes('\n')
		if rerr != nil && rerr != io.EOF {
			return nil, rerr
		}
		if len(chunk) == 0 {
			// EOF with nothing buffered: either empty input or a trailing LF that
			// terminated the previous line. No further line to frame.
			return nil, nil
		}
		lineNo++
		lineStart := offset
		offset += len(chunk)

		// Strip the terminating LF, if present, to get the line's own bytes.
		line := chunk
		if line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}

		if perr := frameLine(line, lineNo, lineStart); perr != nil {
			return perr, nil
		}
		p := &parser{data: line, line: lineNo, col: 1, baseOffset: lineStart, format: doc.FormatJSONL}
		root, perr := p.parseDocument()
		if perr != nil {
			return perr, nil
		}
		emit(doc.NewDocument(doc.FormatJSONL, root, line))

		if rerr == io.EOF {
			// The final line had no trailing LF; it was framed and emitted above.
			return nil, nil
		}
	}
}

// frameLine applies the JSONL framing rules (trailing CR, blank line) to one
// line's bytes and returns a hard error if either is violated. line has already
// had its terminating LF stripped; lineNo and lineStart are global.
func frameLine(line []byte, lineNo, lineStart int) *doc.ParseError {
	if n := len(line); n > 0 && line[n-1] == '\r' {
		return &doc.ParseError{
			Format:   doc.FormatJSONL,
			Position: doc.Position{Line: lineNo, Column: n, ByteOffset: lineStart + n - 1},
			Message:  "line ends with a carriage return; JSONL is LF-only",
		}
	}
	if isBlankLine(line) {
		return &doc.ParseError{
			Format:   doc.FormatJSONL,
			Position: doc.Position{Line: lineNo, Column: 1, ByteOffset: lineStart},
			Message:  "blank line is not a valid JSONL document",
		}
	}
	return nil
}

// isBlankLine reports whether a line is empty or contains only spaces and tabs.
// A line whose only content is a carriage return is handled by the trailing-CR
// rule before this is reached.
func isBlankLine(line []byte) bool {
	for _, b := range line {
		if b != ' ' && b != '\t' {
			return false
		}
	}
	return true
}

// byteReader is a minimal io.Reader over an in-memory slice, letting ParseLines
// reuse ParseStream without importing bytes. It never returns a non-EOF error.
type byteReader struct {
	data []byte
	pos  int
}

func newByteReader(data []byte) *byteReader { return &byteReader{data: data} }

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
