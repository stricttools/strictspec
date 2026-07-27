// Package jsondoc is the JSON/JSONL backend of the strictspec document model. It
// parses JSON (and, via jsonl.go, JSONL) bytes with a hand-written lossless
// lexer+parser and folds the result into the format-neutral doc model: every
// value node maps to a tagged, lexeme-retaining doc.Node, object keys resolve
// into an ordered record while spans point at the source, and the original bytes
// round-trip byte-identically via Document.Bytes().
//
// encoding/json is deliberately NOT used: it is lossy (it normalizes numbers,
// decodes strings, discards ordering, and silently last-wins on duplicate keys).
// This backend retains the EXACT source lexeme of every scalar and raises a hard
// error on duplicate object keys.
package jsondoc

import (
	"strings"
	"unicode/utf8"

	"github.com/smm-h/strictspec/go/internal/doc"
)

// maxDepth bounds nesting to keep parsing safe on adversarial input. The parser
// is recursive-descent; to guarantee it never overflows the stack on a document
// like "[[[[...", it counts container depth and returns a clean ParseError once
// the bound is crossed. This is a stack-safety guard, NOT a document-size limit:
// string length, array length, object size, and numeric-lexeme length are all
// unbounded. 10000 is far below the depth at which Go's default goroutine stack
// would fault, yet far above any realistic legitimate nesting.
const maxDepth = 10000

// Parse parses a single JSON document into a doc.Document. On any lexical or
// structural error it returns a typed *doc.ParseError carrying a source position
// (1-based line/column, 0-based byte offset) and a nil document. Parse never
// returns a plain error — the only failure type is *doc.ParseError.
//
// Numeric lexemes are retained VERBATIM and classified by grammar only (see
// parseNumber): no value is parsed, so integer int64 overflow and float64 range
// are NOT checked here. Those are schema-level (number-scalar) concerns per the
// constitution's primitives appendix item 4; the lossless document model records
// the lexeme and its class and nothing more.
func Parse(src []byte) (*doc.Document, *doc.ParseError) {
	p := &parser{data: src, line: 1, col: 1, format: doc.FormatJSON}
	root, err := p.parseDocument()
	if err != nil {
		return nil, err
	}
	return doc.NewDocument(doc.FormatJSON, root, src), nil
}

// parser is the per-parse cursor over a byte slice. For a standalone JSON
// document data is the whole source and baseOffset is 0. For one JSONL line data
// is just that line's bytes (LF stripped) and baseOffset/line seed the cursor so
// every emitted Position is GLOBAL (relative to the whole input), letting
// diagnostics carry @L<line>:<offset> anchors.
type parser struct {
	data       []byte
	i          int        // 0-based index into data
	line       int        // current 1-based line number (global)
	col        int        // current 1-based column, byte-based (matches doc.Position)
	baseOffset int        // global byte offset of data[0]
	format     doc.Format // doc.FormatJSON or doc.FormatJSONL — tags every ParseError
}

// pos returns the cursor's current source position.
func (p *parser) pos() doc.Position {
	return doc.Position{Line: p.line, Column: p.col, ByteOffset: p.baseOffset + p.i}
}

// atEnd reports whether the cursor is past the last byte.
func (p *parser) atEnd() bool { return p.i >= len(p.data) }

// peek returns the current byte without consuming it. Callers must check atEnd
// first.
func (p *parser) peek() byte { return p.data[p.i] }

// next consumes and returns the current byte, advancing the line/column cursor.
// A newline advances the line and resets the column; every other byte advances
// the (byte-based) column by one.
func (p *parser) next() byte {
	b := p.data[p.i]
	p.i++
	if b == '\n' {
		p.line++
		p.col = 1
	} else {
		p.col++
	}
	return b
}

// isJSONSpace reports whether b is one of JSON's four insignificant-whitespace
// bytes (space, tab, LF, CR).
func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// skipWS consumes any run of JSON whitespace.
func (p *parser) skipWS() {
	for !p.atEnd() && isJSONSpace(p.peek()) {
		p.next()
	}
}

// errAt builds a ParseError at the given position with the parser's format tag.
func (p *parser) errAt(pos doc.Position, msg string) *doc.ParseError {
	return &doc.ParseError{Format: p.format, Position: pos, Message: msg}
}

// parseDocument parses exactly one top-level value surrounded by optional
// whitespace, rejecting empty input and any trailing content. It is shared by
// the JSON entry point and the JSONL per-line parser.
func (p *parser) parseDocument() (doc.Node, *doc.ParseError) {
	p.skipWS()
	if p.atEnd() {
		return nil, p.errAt(doc.Position{Line: 1, Column: 1, ByteOffset: 0},
			"empty input: expected a JSON document, found none")
	}
	root, err := p.parseValue(0)
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if !p.atEnd() {
		return nil, p.errAt(p.pos(), "unexpected trailing content after the JSON document")
	}
	return root, nil
}

// parseValue parses one JSON value at the current cursor. depth is the current
// container nesting level, used for the stack-safety bound.
func (p *parser) parseValue(depth int) (doc.Node, *doc.ParseError) {
	p.skipWS()
	if p.atEnd() {
		return nil, p.errAt(p.pos(), "unexpected end of input, expected a value")
	}
	switch b := p.peek(); {
	case b == '{':
		return p.parseObject(depth)
	case b == '[':
		return p.parseArray(depth)
	case b == '"':
		span, lexeme, _, err := p.scanString()
		if err != nil {
			return nil, err
		}
		return doc.NewScalar(doc.String, lexeme, span), nil
	case b == 't':
		return p.parseLiteral("true", doc.Bool)
	case b == 'f':
		return p.parseLiteral("false", doc.Bool)
	case b == 'n':
		return p.parseLiteral("null", doc.Null)
	case b == '-' || (b >= '0' && b <= '9'):
		return p.parseNumber()
	case b == 'N' || b == 'I':
		// NaN, Infinity, -Infinity are valid float64 values but NOT valid JSON.
		return nil, p.errAt(p.pos(), "NaN and Infinity are not valid JSON")
	default:
		return nil, p.errAt(p.pos(), "unexpected character "+quoteByte(b))
	}
}

// parseObject parses a JSON object into an ordered Record. DUPLICATE KEYS ARE A
// HARD ERROR: per spec/DESIGN.md ("JSON duplicate keys are a canonical hard
// error in every backend ... silent last-wins is the typo'd-field failure mode
// in disguise"), the second occurrence of a decoded key aborts the parse with a
// position at that key. Keys are compared by decoded code points (no
// normalization, per primitives appendix item 10).
func (p *parser) parseObject(depth int) (doc.Node, *doc.ParseError) {
	if depth >= maxDepth {
		return nil, p.errAt(p.pos(), "maximum nesting depth exceeded")
	}
	startPos := p.pos()
	p.next() // consume '{'
	entries := []doc.Entry{}
	seen := map[string]struct{}{}

	p.skipWS()
	if !p.atEnd() && p.peek() == '}' {
		p.next()
		return doc.NewRecord(entries, doc.Span{Start: startPos, End: p.pos()}), nil
	}

	for {
		p.skipWS()
		if p.atEnd() {
			return nil, p.errAt(p.pos(), "unterminated object, expected a key or '}'")
		}
		if p.peek() != '"' {
			return nil, p.errAt(p.pos(), "expected a string key, found "+quoteByte(p.peek()))
		}
		keySpan, _, decoded, err := p.scanString()
		if err != nil {
			return nil, err
		}
		if _, dup := seen[decoded]; dup {
			return nil, p.errAt(keySpan.Start, "duplicate key "+quoteString(decoded)+" in JSON object")
		}
		seen[decoded] = struct{}{}

		p.skipWS()
		if p.atEnd() || p.peek() != ':' {
			return nil, p.errAt(p.pos(), "expected ':' after object key")
		}
		p.next() // consume ':'

		value, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		entries = append(entries, doc.Entry{Key: decoded, KeySpan: keySpan, Value: value})

		p.skipWS()
		if p.atEnd() {
			return nil, p.errAt(p.pos(), "unterminated object, expected ',' or '}'")
		}
		switch p.peek() {
		case ',':
			p.next()
		case '}':
			p.next()
			return doc.NewRecord(entries, doc.Span{Start: startPos, End: p.pos()}), nil
		default:
			return nil, p.errAt(p.pos(), "expected ',' or '}' in object, found "+quoteByte(p.peek()))
		}
	}
}

// parseArray parses a JSON array into an ordered Array node.
func (p *parser) parseArray(depth int) (doc.Node, *doc.ParseError) {
	if depth >= maxDepth {
		return nil, p.errAt(p.pos(), "maximum nesting depth exceeded")
	}
	startPos := p.pos()
	p.next() // consume '['
	items := []doc.Node{}

	p.skipWS()
	if !p.atEnd() && p.peek() == ']' {
		p.next()
		return doc.NewArray(items, doc.Span{Start: startPos, End: p.pos()}), nil
	}

	for {
		value, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		items = append(items, value)

		p.skipWS()
		if p.atEnd() {
			return nil, p.errAt(p.pos(), "unterminated array, expected ',' or ']'")
		}
		switch p.peek() {
		case ',':
			p.next()
		case ']':
			p.next()
			return doc.NewArray(items, doc.Span{Start: startPos, End: p.pos()}), nil
		default:
			return nil, p.errAt(p.pos(), "expected ',' or ']' in array, found "+quoteByte(p.peek()))
		}
	}
}

// parseLiteral matches one of the three keyword literals (true, false, null) and
// builds its scalar node. The lexeme is the literal text; the kind is supplied
// by the caller.
func (p *parser) parseLiteral(word string, kind doc.Kind) (doc.Node, *doc.ParseError) {
	startPos := p.pos()
	startI := p.i
	for k := 0; k < len(word); k++ {
		if p.atEnd() || p.peek() != word[k] {
			return nil, p.errAt(startPos, "invalid literal, expected "+quoteString(word))
		}
		p.next()
	}
	return doc.NewScalar(kind, string(p.data[startI:p.i]), doc.Span{Start: startPos, End: p.pos()}), nil
}

// parseNumber scans a JSON number and classifies its lexeme. Per the
// constitution's primitives appendix item 4, a lexeme containing '.', 'e', or
// 'E' is a FLOAT; otherwise it is an INTEGER. The lexeme is retained EXACTLY as
// written (sign, leading digits, fraction, exponent form) with no normalization:
// "-0" stays Integer "-0", "-0.0" stays Float "-0.0", "1e5"/"1E-5" stay Float.
// The numeric value is never computed, so no int64/float64 range check happens
// here — that is the schema-level number-scalar concern.
func (p *parser) parseNumber() (doc.Node, *doc.ParseError) {
	startPos := p.pos()
	startI := p.i
	isFloat := false

	if p.peek() == '-' {
		p.next()
	}
	// Integer part: a lone '0', or a nonzero digit followed by more digits.
	// Leading zeros (e.g. "01") are invalid JSON.
	if p.atEnd() {
		return nil, p.errAt(startPos, "invalid number, expected a digit")
	}
	switch b := p.peek(); {
	case b == '0':
		p.next()
	case b >= '1' && b <= '9':
		p.next()
		for !p.atEnd() && isDigit(p.peek()) {
			p.next()
		}
	default:
		return nil, p.errAt(p.pos(), "invalid number, expected a digit, found "+quoteByte(b))
	}
	// Fraction.
	if !p.atEnd() && p.peek() == '.' {
		isFloat = true
		p.next()
		if p.atEnd() || !isDigit(p.peek()) {
			return nil, p.errAt(p.pos(), "invalid number, expected a digit after the decimal point")
		}
		for !p.atEnd() && isDigit(p.peek()) {
			p.next()
		}
	}
	// Exponent.
	if !p.atEnd() && (p.peek() == 'e' || p.peek() == 'E') {
		isFloat = true
		p.next()
		if !p.atEnd() && (p.peek() == '+' || p.peek() == '-') {
			p.next()
		}
		if p.atEnd() || !isDigit(p.peek()) {
			return nil, p.errAt(p.pos(), "invalid number, expected a digit in the exponent")
		}
		for !p.atEnd() && isDigit(p.peek()) {
			p.next()
		}
	}

	kind := doc.Integer
	if isFloat {
		kind = doc.Float
	}
	return doc.NewScalar(kind, string(p.data[startI:p.i]), doc.Span{Start: startPos, End: p.pos()}), nil
}

// scanString scans a double-quoted JSON string starting at the cursor. It
// returns the string's span (quotes included), its EXACT source lexeme (quotes
// and raw escapes as written), and its DECODED value (used as an object key).
// It validates escape sequences, rejects raw control characters, and validates
// UTF-8; any violation is a typed ParseError with a position.
func (p *parser) scanString() (doc.Span, string, string, *doc.ParseError) {
	startPos := p.pos()
	startI := p.i
	p.next() // consume opening '"'
	var dec strings.Builder

	for {
		if p.atEnd() {
			return doc.Span{}, "", "", p.errAt(p.pos(), "unterminated string")
		}
		b := p.peek()
		switch {
		case b == '"':
			p.next()
			span := doc.Span{Start: startPos, End: p.pos()}
			return span, string(p.data[startI:p.i]), dec.String(), nil
		case b == '\\':
			if err := p.scanEscape(&dec); err != nil {
				return doc.Span{}, "", "", err
			}
		case b < 0x20:
			return doc.Span{}, "", "", p.errAt(p.pos(),
				"raw control character U+"+hex4(rune(b))+" in string; it must be escaped")
		case b < 0x80:
			p.next()
			dec.WriteByte(b)
		default:
			// Multi-byte UTF-8: validate and consume the whole rune.
			r, size := utf8.DecodeRune(p.data[p.i:])
			if r == utf8.RuneError && size == 1 {
				return doc.Span{}, "", "", p.errAt(p.pos(), "invalid UTF-8 byte in string")
			}
			for k := 0; k < size; k++ {
				p.next()
			}
			dec.WriteRune(r)
		}
	}
}

// scanEscape consumes one backslash escape at the cursor and writes its decoded
// value to dec. It handles the simple escapes and \uXXXX, including surrogate
// pairs (a high surrogate followed by \uXXXX low surrogate decodes to the astral
// code point; an unpaired surrogate decodes to U+FFFD, matching how JSON
// consumers recover from a malformed but syntactically-legal escape).
func (p *parser) scanEscape(dec *strings.Builder) *doc.ParseError {
	escPos := p.pos()
	p.next() // consume '\'
	if p.atEnd() {
		return p.errAt(escPos, "unterminated escape sequence")
	}
	e := p.next()
	switch e {
	case '"':
		dec.WriteByte('"')
	case '\\':
		dec.WriteByte('\\')
	case '/':
		dec.WriteByte('/')
	case 'b':
		dec.WriteByte('\b')
	case 'f':
		dec.WriteByte('\f')
	case 'n':
		dec.WriteByte('\n')
	case 'r':
		dec.WriteByte('\r')
	case 't':
		dec.WriteByte('\t')
	case 'u':
		r1, ok := p.readHex4()
		if !ok {
			return p.errAt(escPos, "invalid \\u escape: expected four hexadecimal digits")
		}
		if r1 >= 0xD800 && r1 <= 0xDBFF {
			// High surrogate: try to pair with a following \uXXXX low surrogate.
			if p.i+1 < len(p.data) && p.data[p.i] == '\\' && p.data[p.i+1] == 'u' {
				save := *p
				p.next() // '\'
				p.next() // 'u'
				r2, ok2 := p.readHex4()
				if ok2 && r2 >= 0xDC00 && r2 <= 0xDFFF {
					dec.WriteRune(0x10000 + (r1-0xD800)<<10 + (r2 - 0xDC00))
					return nil
				}
				*p = save // not a valid low surrogate; leave it for the main loop
			}
			dec.WriteRune(utf8.RuneError)
		} else if r1 >= 0xDC00 && r1 <= 0xDFFF {
			// Unpaired low surrogate.
			dec.WriteRune(utf8.RuneError)
		} else {
			dec.WriteRune(r1)
		}
	default:
		return p.errAt(escPos, "invalid escape sequence \\"+string(e))
	}
	return nil
}

// readHex4 reads exactly four hexadecimal digits and returns their rune value.
// It returns ok=false (consuming nothing) if fewer than four hex digits follow.
func (p *parser) readHex4() (rune, bool) {
	if p.i+4 > len(p.data) {
		return 0, false
	}
	var r rune
	for k := 0; k < 4; k++ {
		v, ok := hexVal(p.data[p.i+k])
		if !ok {
			return 0, false
		}
		r = r<<4 | rune(v)
	}
	for k := 0; k < 4; k++ {
		p.next()
	}
	return r, true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func hexVal(b byte) (int, bool) {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0'), true
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10, true
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10, true
	}
	return 0, false
}

// quoteByte renders a single byte for an error message: printable ASCII as
// 'x', everything else as its U+XXXX code.
func quoteByte(b byte) string {
	if b >= 0x20 && b < 0x7f {
		return "'" + string(b) + "'"
	}
	return "U+" + hex4(rune(b))
}

// quoteString renders s in double quotes for an error message.
func quoteString(s string) string { return "\"" + s + "\"" }

// hex4 renders r as a 4-digit uppercase hex string (zero-padded).
func hex4(r rune) string {
	const digits = "0123456789ABCDEF"
	var buf [4]byte
	for k := 3; k >= 0; k-- {
		buf[k] = digits[r&0xF]
		r >>= 4
	}
	return string(buf[:])
}
