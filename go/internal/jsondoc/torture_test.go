package jsondoc

// torture is a JSON document exercising every lexical construct the strictspec
// JSON backend must map into the doc model and round-trip byte-identically:
// nested objects and arrays, all string escape forms (including a \uXXXX astral
// surrogate pair and a BMP \u escape), unicode object keys written literally,
// every number shape (integer, negative, negative zero, leading-zero-free,
// fraction, both exponent spellings, and a magnitude far beyond float64 so the
// verbatim-lexeme guarantee is meaningfully tested), a long string, booleans,
// and null. Spellings are intentionally non-canonical so lexeme recovery is
// meaningful. It is pretty-printed across many lines so byte-offset computation
// through newlines is exercised.
const torture = `{
  "title": "basic \"quoted\" string",
  "escapes": "tab\tnewline\nreturn\rbackslash\\slash\/bfk\b\fbmpéastral𝄞",
  "ünîcodé": "key written in raw UTF-8",
  "empty_string": "",
  "empty_object": {},
  "empty_array": [],
  "numbers": {
    "int": 42,
    "neg": -17,
    "zero": 0,
    "neg_zero": -0,
    "float": 3.14,
    "neg_zero_float": -0.0,
    "exp_lower": 1e5,
    "exp_upper": 1E-5,
    "exp_signed": 6.626e-34,
    "small_frac": 0.1,
    "big_beyond_f64": 123456789012345678901234567890,
    "big_float": 1.7976931348623159e308
  },
  "flags": [true, false, null],
  "nested": {
    "level1": {
      "level2": {
        "items": [1, 2, [3, 4, {"deep": "value"}]]
      }
    }
  },
  "long": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}`
