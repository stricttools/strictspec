"""The pinned diagnostic-code catalogue and message templates.

Transcribed VERBATIM from ``spec/appendix-error-codes.md`` (the normative
catalogue). This module is the single source the harness uses to (a) reject a
fixture that cites a code outside the catalogue, and (b) render the expected
message text for a diagnostic from the pinned template plus the fixture's slot
values -- so a fixture never embeds message prose, only codes, paths, and slot
values (conformance/DESIGN.md, "Fixture format").

Slot names are derived mechanically from each template via ``{slot}``
placeholders; the template is the source of truth for which slots exist. The
only slot that is OPTIONAL in a fixture is ``suggestion`` (did-you-mean is
computed and may render empty; see appendix-rendering.md Part C).

Appendix stability policy (appendix-error-codes.md sec. 2): codes are permanent;
any wording/slot change is a breaking-class, changelog-covered event that
triggers full conformance-fixture regeneration. When the appendix changes, this
table changes with it, in the same commit.
"""

from __future__ import annotations

import re

# Slots that a fixture may omit (rendered as the empty string when absent).
OPTIONAL_SLOTS = frozenset({"suggestion"})

# code -> pinned message template. Verbatim from appendix-error-codes.md.
# Backtick-delimited literals in the prose are part of the template text; only
# ``{name}`` sequences are slots.
CATALOGUE: dict[str, str] = {
    # 4. Parse errors
    "STRICTSPEC_PARSE_JSON_SYNTAX": "JSON parse error at {path}: {detail}.",
    "STRICTSPEC_PARSE_JSON_TRAILING": "Trailing content after the JSON document at {path}.",
    "STRICTSPEC_PARSE_JSON_EMPTY": "Empty input: expected a JSON document, found none.",
    "STRICTSPEC_PARSE_JSON_DUPLICATE_KEY": "Duplicate key {key} in JSON object at {path}.",
    "STRICTSPEC_PARSE_JSONL_LINE_SYNTAX": "JSONL parse error on line {line} at {path}: {detail}.",
    "STRICTSPEC_PARSE_JSONL_TRAILING_CR": "Line {line} ends with a carriage return; JSONL is LF-only.",
    "STRICTSPEC_PARSE_JSONL_BLANK_LINE": "Blank line {line} is not a valid JSONL document.",
    "STRICTSPEC_PARSE_JSONL_DUPLICATE_KEY": "Duplicate key {key} on JSONL line {line} at {path}.",
    "STRICTSPEC_PARSE_TOML_SYNTAX": "TOML parse error at {path}: {detail}.",
    "STRICTSPEC_PARSE_TOML_DUPLICATE_KEY": "Duplicate key {key} in TOML table at {path}.",
    "STRICTSPEC_PARSE_TOML_EMPTY": "Empty TOML input: expected a document, found none.",
    # 5. Meta-schema violations
    "STRICTSPEC_SCHEMA_DEFAULT_KEY": "The `default` key is not a construct of strictspec (field {path}); remove it. A typed value never carries data the author did not write.",
    "STRICTSPEC_SCHEMA_TUPLE_ARRAY_BOUNDS": "Field {path} declares both tuple form and array-length bounds; the two are mutually exclusive.",
    "STRICTSPEC_SCHEMA_ALIAS_ON_DISCRIMINATOR": "Alias {alias} targets discriminator field {path}; a discriminator may not be aliased.",
    "STRICTSPEC_SCHEMA_TOML_NULLABLE": "A nullable union is reachable at {path}, but the target document syntax is TOML; TOML models absence as an optional field.",
    "STRICTSPEC_SCHEMA_REGEX_UNSUPPORTED": "Regex at {path} uses a feature outside the RE2-compatible subset: {detail}.",
    "STRICTSPEC_SCHEMA_DATETIME_KIND_MISMATCH": "Datetime range at {path} compares an offset scalar with a local scalar; comparisons must be same-kind.",
    "STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE": "Opaque JSON leaf {path} declares neither `consumer_check` nor `unchecked`; one is required.",
    "STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON": "Leaf {path} declares `unchecked = true` without the mandatory `unchecked_reason`.",
    "STRICTSPEC_SCHEMA_TS_WITHOUT_SAFE_INTEGERS": "Schema {schema} declares a TypeScript target but omits `safe_integers = true`; a TS target requires it.",
    "STRICTSPEC_SCHEMA_UNKNOWN_META_KEY": "Unknown meta-schema key {key} at {path}.{suggestion}",
    "STRICTSPEC_SCHEMA_UNKNOWN_TYPE_REF": "Field {path} references named type {name}, which is not declared or imported.",
    "STRICTSPEC_SCHEMA_NODE_KIND_UNION_AMBIGUOUS": "Undiscriminated union at {path} has two arms of the same node kind ({kind}); same-kind arms require a discriminator.",
    "STRICTSPEC_SCHEMA_MISSING_FORMAT_VERSION": "Schema {schema} does not declare `format_version`.",
    "STRICTSPEC_SCHEMA_MISSING_META_VERSION": "Schema {schema} does not declare `meta_version`.",
    # 6. Import violations
    "STRICTSPEC_IMPORT_MISSING_TYPE_FILE": "Type-definition file {file} imported by {schema} does not exist.",
    "STRICTSPEC_IMPORT_UNKNOWN_TYPE": "Type {name} is not defined in imported file {file}.",
    "STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT": "Imported file {file} declares a constraint; type-definition files may declare types only, not constraints.",
    "STRICTSPEC_IMPORT_TRANSITIVE": "Type-definition file {file} imports another file; transitive imports are not permitted.",
    # 7. Enum-sourcing errors
    "STRICTSPEC_ENUMSRC_MISSING_SOURCE": "Enum at {path} sources arms from {source}, which does not exist.",
    "STRICTSPEC_ENUMSRC_STALE": "Baked enum arms at {path} differ from source {source}; regenerate with `strictspec gen`. Baked: {baked}. Source: {actual}.",
    "STRICTSPEC_ENUMSRC_SOURCE_NOT_STRINGS": "Enum source {source} at {path} yields a non-string arm; sourced enum arms must be strings.",
    "STRICTSPEC_ENUMSRC_BAD_SELECTOR": "Enum-source selector {selector} at {path} does not resolve within {source}.",
    # 8. Version-gate errors
    "STRICTSPEC_GATE_ABSENT": "Document is missing `format_version`. Schema {schema} expects {expected}. Run: {invocation}",
    "STRICTSPEC_GATE_WRONG_TYPE": "Document `format_version` must be an integer; got {got}. Schema {schema} expects {expected}. Run: {invocation}",
    "STRICTSPEC_GATE_UNSUPPORTED": "Document `format_version` is {got}, but schema {schema} accepts exactly {expected} (migration set {migset}). Run: {invocation}",
    # 9. meta_version-gate errors
    "STRICTSPEC_METAGATE_ABSENT": "Schema {schema} is missing `meta_version`. This strictspec release expects {expected}. Run: {invocation}",
    "STRICTSPEC_METAGATE_WRONG_TYPE": "Schema `meta_version` must be an integer; got {got}.",
    "STRICTSPEC_METAGATE_UNSUPPORTED": "Schema {schema} declares `meta_version` {got}, but this strictspec release supports {expected}. Run: {invocation}",
    # 10. Unknown and duplicate keys
    "STRICTSPEC_KEY_UNKNOWN": "Unknown key {key} at {path}.{suggestion}",
    "STRICTSPEC_KEY_DUPLICATE": "Duplicate key {key} at {path}.",
    # 11. Type mismatches
    "STRICTSPEC_TYPE_MISMATCH": "Expected {expected} at {path}, got {got}.",
    "STRICTSPEC_TYPE_NOT_INTEGER": "Expected an integer at {path}, got {got}.",
    "STRICTSPEC_TYPE_NOT_FLOAT": "Expected a float at {path}, got {got}.",
    "STRICTSPEC_TYPE_NOT_BOOLEAN": "Expected a boolean at {path}, got {got}.",
    "STRICTSPEC_TYPE_NOT_STRING": "Expected a string at {path}, got {got}.",
    "STRICTSPEC_TYPE_NOT_RECORD": "Expected a record at {path}, got {got}.",
    "STRICTSPEC_TYPE_NOT_ARRAY": "Expected an array at {path}, got {got}.",
    "STRICTSPEC_TYPE_NOT_MAP": "Expected a map at {path}, got {got}.",
    "STRICTSPEC_TYPE_NOT_DATE": "Expected a date at {path}, got {got}.",
    "STRICTSPEC_TYPE_NOT_TIME": "Expected a time at {path}, got {got}.",
    "STRICTSPEC_TYPE_NOT_DATETIME": "Expected a datetime at {path}, got {got}.",
    "STRICTSPEC_TYPE_DATETIME_KIND": "Expected a {expected} datetime at {path}; got a {got} datetime.",
    "STRICTSPEC_TYPE_NOT_LITERAL": "Expected the literal {expected} at {path}, got {got}.",
    "STRICTSPEC_TYPE_NOT_ENUM_MEMBER": "Value {got} at {path} is not one of {expected}.{suggestion}",
    "STRICTSPEC_TYPE_MISSING_REQUIRED": "Missing required field {key} at {path}.",
    "STRICTSPEC_TYPE_TUPLE_ARITY": "Tuple at {path} expects {expected} elements, got {got}.",
    # 12. Value-constraint violations
    "STRICTSPEC_VALUE_NUM_TOO_SMALL": "Value {actual} at {path} is below the minimum {limit}.",
    "STRICTSPEC_VALUE_NUM_TOO_SMALL_EXCLUSIVE": "Value {actual} at {path} must be greater than {limit}.",
    "STRICTSPEC_VALUE_NUM_TOO_LARGE": "Value {actual} at {path} is above the maximum {limit}.",
    "STRICTSPEC_VALUE_NUM_TOO_LARGE_EXCLUSIVE": "Value {actual} at {path} must be less than {limit}.",
    "STRICTSPEC_VALUE_DATETIME_BEFORE": "Datetime {actual} at {path} is before the minimum {limit}.",
    "STRICTSPEC_VALUE_DATETIME_AFTER": "Datetime {actual} at {path} is after the maximum {limit}.",
    "STRICTSPEC_VALUE_STRING_TOO_SHORT": "String at {path} has {actual} code points; minimum is {limit}.",
    "STRICTSPEC_VALUE_STRING_TOO_LONG": "String at {path} has {actual} code points; maximum is {limit}.",
    "STRICTSPEC_VALUE_STRING_EMPTY": "String at {path} is empty; a non-empty value is required.",
    "STRICTSPEC_VALUE_STRING_REGEX": "String {actual} at {path} does not match the required pattern {pattern}.",
    "STRICTSPEC_VALUE_MAP_KEY_REGEX": "Map key {key} at {path} does not match the required key pattern {pattern}.",
    "STRICTSPEC_VALUE_ARRAY_TOO_SHORT": "Array at {path} has {actual} elements; minimum is {limit}.",
    "STRICTSPEC_VALUE_ARRAY_TOO_LONG": "Array at {path} has {actual} elements; maximum is {limit}.",
    # 13. Intra-document constraint violations
    "STRICTSPEC_INTRA_CONDITIONAL_REQUIRED": "Field {key} at {path} is required when {condition}.",
    "STRICTSPEC_INTRA_CONDITIONAL_VALUE": "Field {key} at {path} must equal {expected} when {condition}; got {got}.",
    "STRICTSPEC_INTRA_EXACTLY_ONE_OF": "Exactly one of {fields} must be present at {path}; found {actual}.",
    "STRICTSPEC_INTRA_AT_LEAST_ONE_OF": "At least one of {fields} must be present at {path}; none were.",
    "STRICTSPEC_INTRA_CO_PRESENCE": "Fields {fields} at {path} must be present together or absent together; found {actual}.",
    "STRICTSPEC_INTRA_MUTUAL_EXCLUSION": "Fields {fields} at {path} are mutually exclusive; found {actual}.",
    "STRICTSPEC_INTRA_COLLECTIONS_DISJOINT": "Arrays {fields} at {path} must share no element; {value} appears in both (normalization: {normalization}).",
    "STRICTSPEC_INTRA_FORBIDDEN_WHEN": "Field {key} at {path} is forbidden when {condition}.",
    "STRICTSPEC_INTRA_UNIQUE_BY": "Duplicate value {value} for unique-by {field} at {path} (normalization: {normalization}).",
    "STRICTSPEC_INTRA_PAIRWISE_DISTINCT": "Values at {path} must be pairwise distinct; {value} repeats (normalization: {normalization}).",
    "STRICTSPEC_INTRA_RANGES_DISJOINT": "Half-open ranges at {path} overlap: {value} intersects {actual}.",
    "STRICTSPEC_INTRA_ORDERED_PAIR": "Field {actual} at {path} must be less than sibling {value}.",
    "STRICTSPEC_INTRA_REFERENCE_UNRESOLVED": "Reference {value} at {path} does not resolve within the document.",
    # 14. Cross-document constraint violations
    "STRICTSPEC_CROSS_REFERENCE_UNRESOLVED": "Reference {value} at {path} does not resolve in {source}.",
    "STRICTSPEC_CROSS_SET_COVERAGE": "Element {value} of {source} is not covered by the collection at {path}.",
    "STRICTSPEC_CROSS_COLLECTION_UNIQUE": "Value {value} at {path} also appears in {source}; it must be unique across the collection family.",
    "STRICTSPEC_CROSS_COUNT_LIMIT": "Collection at {path} has {actual} elements across {source}; the limit is {limit}.",
    "STRICTSPEC_CROSS_SUM_LIMIT": "Sum of {field} across {source} at {path} is {actual}; the limit is {limit}.",
    "STRICTSPEC_CROSS_SUM_FIELD_MISSING": "sum-limit at {path} over {source} requires numeric field {field} on every selected document; document {actual} lacks it or has a non-numeric value.",
    "STRICTSPEC_CROSS_RESOLVER_UNAVAILABLE": "Constraint at {path} requires evidence resolver {source}, which this environment cannot satisfy.",
    # 15. Union dispatch errors
    "STRICTSPEC_UNION_DISCRIMINATOR_MISSING": "Missing discriminator {key} at {path}; expected one of {expected}.",
    "STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN": "Discriminator {got} at {path} is not one of {expected}.{suggestion}",
    "STRICTSPEC_UNION_NODE_KIND": "No union arm at {path} accepts a {got}; expected one of {expected}.",
    # 16. Alias, depth, number
    "STRICTSPEC_ALIAS_BOTH_PRESENT": "Both {alias} and canonical {canonical} are present at {path}; provide exactly one.",
    "STRICTSPEC_DEPTH_EXCEEDED": "Document nesting at {path} exceeds the maximum validation depth of {limit}.",
    "STRICTSPEC_NUM_SAFE_INTEGER": "Integer {actual} at {path} exceeds the safe-integer range (|n| >= 2^53) required by `safe_integers`.",
    "STRICTSPEC_NUM_UNREPRESENTABLE": "Lexeme {actual} at {path} cannot be represented exactly as float64; the `number` scalar refuses silent precision loss.",
    "STRICTSPEC_NUM_INT_OVERFLOW": "Integer lexeme {actual} at {path} overflows int64.",
    "STRICTSPEC_NUM_FLOAT_OVERFLOW": "Float lexeme {actual} at {path} is beyond float64 range.",
    "STRICTSPEC_NUM_NON_FINITE": "Non-finite number {actual} at {path} is not permitted.",
    # 17. Migration errors
    "STRICTSPEC_MIGRATE_TARGET_MISSING": "Migration op {op} targets {path}, which is absent in the document.",
    "STRICTSPEC_MIGRATE_COLLISION": "Migration op {op} would write {path}, which already exists.",
    "STRICTSPEC_MIGRATE_TYPE_MISMATCH": "Migration op {op} at {path} expected {expected}, found {got}.",
    "STRICTSPEC_MIGRATE_UNWRAP_NOT_SINGLETON": "unwrap_singleton at {path} requires a single-element array; found {actual} elements.",
    "STRICTSPEC_MIGRATE_ON_CURRENT": "Document at {path} is already at the current `format_version` {expected}; nothing to migrate.",
    "STRICTSPEC_MIGRATE_UNKNOWN_SET": "No migration set {migset} is registered for schema {schema}.",
    "STRICTSPEC_MIGRATE_REVALIDATION_FAILED": "Migrated document at {path} does not validate at `format_version` {expected}; the migration is unsound.",
    "STRICTSPEC_MIGRATE_PREDICATE_UNSUPPORTED": "Predicate at {path} tests more than equality and presence; migration predicates are restricted.",
    "STRICTSPEC_MIGRATE_IRREVERSIBLE_DOWN": "Op {op} at {path} is declared irreversible; a down-migration was requested.",
    # 17a. Write-path serialization refusals
    "STRICTSPEC_SERIALIZE_NONCURRENT": "Refusing to serialize document at {path}: its `format_version` is {got}, but schema {schema} serializes only the current {expected}. Migrate before writing.",
    # 17b. Live-channel version-negotiation refusals
    "STRICTSPEC_CHANNEL_VERSION_REFUSED": "Cannot open channel for schema {schema}: peer offers `format_version` {got}, this endpoint speaks only {expected}. Update the client to the paired strictspec release ({release}).",
    # 18. Diff / certificate errors
    "STRICTSPEC_DIFF_CORPUS_EMPTY": "The corpus glob {source} resolved to zero documents; `diff` requires a non-empty corpus.",
    "STRICTSPEC_DIFF_VIOLATED": "Claim {condition} is VIOLATED: corpus document {source} is a counterexample.",
    "STRICTSPEC_DIFF_NARROWING_UNBUMPED": "Document {source} is valid under the old schema but invalid under the new schema at the same `format_version` {expected}; this narrowing requires a version bump.",
    "STRICTSPEC_DIFF_TAXONOMY_MISDECLARED": "Op {op} is declared {expected} but the corpus shows it is {actual}.",
    "STRICTSPEC_DIFF_ADJUDICATION_MISSING": "Claim {condition} is corpus-supported without a corpus, and no adjudication entry covers it.",
    "STRICTSPEC_DIFF_ADJUDICATION_INVALID": "Adjudication file {source} does not validate against the adjudication schema: {detail}.",
    # 19. doc-diff errors
    "STRICTSPEC_DOCDIFF_SCHEMA_MISMATCH": "Documents {source} and {actual} do not share `format_version`; `doc-diff` compares documents of one schema at one version.",
    "STRICTSPEC_DOCDIFF_INVALID_OPERAND": "Operand {source} does not validate against the schema; `doc-diff` compares valid documents.",
    # 20. CLI / manifest errors
    "STRICTSPEC_MANIFEST_ALREADY_EXISTS": "A `strictspec.toml` already exists at {path}; `strictspec init` refuses to overwrite it.",
    "STRICTSPEC_MANIFEST_UNKNOWN_STORE": "Manifest declares store {name}, whose kind {got} is not a recognized store kind.",
    "STRICTSPEC_MANIFEST_UNKNOWN_RESOLVER": "Manifest / schema references evidence resolver {name}, which is not in the resolver vocabulary.",
    "STRICTSPEC_MANIFEST_GENERATED_PATH_DIRTY": "Generated path {path} has uncommitted local edits; regenerate before proceeding.",
    "STRICTSPEC_MANIFEST_PAIRING_MISMATCH": "Runtime version {actual} does not match generated-code version {expected}; regenerate against the paired release.",
    "STRICTSPEC_MANIFEST_DRIFT": "Generated output at {path} differs from a fresh generation; run `strictspec gen` and commit.",
    # 21. Custom-scalar errors
    "STRICTSPEC_SCALAR_LEXEME": "Value {actual} at {path} does not match the {name} scalar's lexeme rule {pattern}.",
    "STRICTSPEC_SCALAR_BASE_MISMATCH": "Value at {path} is not a {expected} lexeme, which the {name} scalar refines.",
    "STRICTSPEC_SCALAR_UNKNOWN": "Field {path} uses custom scalar {name}, which is not registered in the manifest.",
    "STRICTSPEC_SCALAR_NO_BINDING": "Custom scalar {name} declares no binding for target {got}; every declared target requires a binding.",
    "STRICTSPEC_SCALAR_LENGTH": "Value at {path} violates the {name} scalar's length bound ({actual}, limit {limit}).",
}

_SLOT_RE = re.compile(r"\{(\w+)\}")


def slots_for(code: str) -> set[str]:
    """The set of slot names a code's template interpolates."""
    return set(_SLOT_RE.findall(CATALOGUE[code]))


def render(code: str, slots: dict[str, str]) -> str:
    """Render the pinned message for ``code`` with the provided slot values.

    Slot values are already-rendered strings (fixtures carry rendered slot
    values per appendix-rendering.md, never raw document values). ``suggestion``
    defaults to the empty string when omitted.
    """
    template = CATALOGUE[code]
    filled = dict(slots)
    for opt in OPTIONAL_SLOTS:
        filled.setdefault(opt, "")

    def sub(m: re.Match[str]) -> str:
        return filled[m.group(1)]

    return _SLOT_RE.sub(sub, template)
