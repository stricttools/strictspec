package tomldoc

// torture is a TOML document exercising every lexical construct the strictspec
// TOML backend must map into the doc model and round-trip byte-identically:
// line and inline comments, blank lines, all four string styles, every integer
// base (with underscores), every float form (incl. inf/nan/negative-zero),
// all four datetime kinds, booleans, dotted keys, inline tables, inline and
// multiline arrays, standard tables, nested tables, and array-of-tables. Value
// spellings are intentionally non-canonical (1_000, 0xDEAD_beef, 1.0) so lexeme
// recovery is meaningfully tested. It is written independently of, but covers
// the same constructs as, the toml-eslint-parser spike fixture.
const torture = `# Top-level document comment
# second comment line

title = "basic \"quoted\" string"   # inline comment on a key
literal = 'C:\path\no\escape'
multiline = """
line one
line two"""
multiline_literal = '''raw
lines'''

dec = 1_000
neg = -17
hex = 0xDEAD_beef
oct = 0o755
bin = 0b1010
big = 9_223_372_036_854_775_807

f1 = 1.0
f2 = 3.14
f3 = 1e5
negzero = -0.0
planck = 6.626e-34
inf_pos = inf
inf_neg = -inf
not_a_num = nan

yes = true
no = false

odt = 1979-05-27T07:32:00Z
ldt = 1979-05-27T07:32:00
ld = 1979-05-27
lt = 07:32:00

fruit.name = "apple"
fruit.color = "red"

inline = { x = 1, y = 2.5, label = "pt" }
arr = [1, 2, 3]
nested_arr = [
  "a",
  "b",
]

[table_a]
key = "value"   # trailing comment inside a table
count = 42

[table_a.sub]
deep = true

[[products]]
name = "hammer"
sku = 738594937

[[products]]
name = "nail"
sku = 284758393
`
