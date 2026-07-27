/**
 * The torture TOML document shared by every spike test. It exercises every
 * lexical construct strictspec's lossless write path must preserve: comments
 * (line + inline), blank lines, tables, array-of-tables, inline tables, arrays
 * (inline + multiline), all four string styles, every integer/float/datetime
 * form, booleans, dotted keys, and literal key spellings. Value spellings are
 * intentionally non-canonical (1_000, 0xDEAD_beef, 1.0) so lexeme recovery is
 * meaningfully tested.
 */
export const TORTURE = `# Top-level document comment
# second comment line

title = "basic \\"quoted\\" string"   # inline comment on a key
literal = 'C:\\path\\no\\escape'
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
`;
