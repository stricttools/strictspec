"""The strictspec shared emitter IR and its executor.

A faithful port of go/internal/ir (program + execute + walk + values + scalars +
constraints + condition + datetime). It is the single intermediate
representation from which every target's validator is driven: the same executor
that backs the reference interpreter backs the generated validators, which is
the mechanism behind the four-target verdict+code+path+message identity.

Ordering is a property of the IR, not of any target: the executor fixes the
traversal and emission order once (gate first and terminal; document-order
present keys with anchored missing-required interleaving; phase-2 constraints
over records whose phase 1 passed), so every target accumulates diagnostics in
the identical order.
"""

from __future__ import annotations

import math
import re
from dataclasses import dataclass, field
from datetime import timezone

from . import _diag as diag
from . import _doc as doc
from . import _schema as schema
from . import _strdecode as strdecode

# The pinned recursion-depth cap (fired before stack exhaustion).
MAX_VALIDATION_DEPTH = 128


@dataclass
class Program:
    """A compiled, self-contained IR program: a resolved schema plus its bound
    custom scalars.
    """

    schema: schema.Schema
    scalars: dict[str, schema.Scalar] = field(default_factory=dict)

    def schema_name(self) -> str:
        return self.schema.name

    def format_version(self) -> int:
        return self.schema.format_version


def compile_program(s: schema.Schema, scalars: dict[str, schema.Scalar] | None) -> Program:
    return Program(schema=s, scalars=scalars or {})


@dataclass
class ExecOptions:
    format: str = doc.FORMAT_JSON
    evidence: dict[str, list[dict]] | None = None
    structural_only: bool = False
    jsonl: bool = False
    line: int = 0
    line_start: int = 0


@dataclass
class _P2Task:
    typ: schema.Type
    rec: doc.Node
    path: diag.Path


_REGEX_CACHE: dict[str, re.Pattern] = {}


def _compile_regex(pattern: str) -> re.Pattern:
    re_ = _REGEX_CACHE.get(pattern)
    if re_ is not None:
        return re_
    try:
        re_ = re.compile(pattern)
    except re.error:
        re_ = re.compile(r"\A\Z")  # matches only the empty string (fails closed)
    _REGEX_CACHE[pattern] = re_
    return re_


class _Exec:
    def __init__(self, p: Program, root: doc.Node, opts: ExecOptions) -> None:
        self.p = p
        self.s = p.schema
        self.scalars = p.scalars
        self.root = root
        self.format = opts.format
        self.evidence = opts.evidence or {}
        self.jsonl = opts.jsonl
        self.line = opts.line
        self.line_start = opts.line_start
        self.diags = diag.Diagnostics()
        self.phase2: list[_P2Task] = []
        self.clean: dict[int, bool] = {}
        self.depth = 0

    # --- gate ----------------------------------------------------------------

    def gate(self, root: doc.Node) -> bool:
        invocation = (
            f"strictspec migrate --schema {self.s.name} --to {self.s.format_version} <paths>"
        )
        base = {
            "schema": diag.SlotIdentifier(self.s.name),
            "expected": diag.SlotVersion(self.s.format_version),
            "invocation": diag.SlotString(invocation),
        }
        fv = _entry_of(root, "format_version")
        if fv is None:
            self.emit("STRICTSPEC_GATE_ABSENT", diag.new_path(), root, base)
            return False
        if fv.kind != doc.Kind.INTEGER:
            slots = dict(base)
            slots["got"] = diag.SlotValue(self.value_of(fv))
            self.emit("STRICTSPEC_GATE_WRONG_TYPE", diag.new_path(), root, slots)
            return False
        got = _sval_int_lexeme(fv.lexeme)
        if got != self.s.format_version:
            slots = dict(base)
            slots["got"] = diag.SlotVersion(got)
            slots["migset"] = diag.SlotIdentifier(self.s.name)
            self.emit("STRICTSPEC_GATE_UNSUPPORTED", diag.new_path(), root, slots)
            return False
        return True

    # --- emit ----------------------------------------------------------------

    def emit(self, code: str, path: diag.Path, anchor_node: doc.Node | None, slots) -> None:
        if self.jsonl:
            off = 0
            if anchor_node is not None:
                sp = anchor_node.span
                if sp.start.is_valid():
                    off = sp.start.byte_offset - self.line_start
                    if off < 0:
                        off = 0
            path = path.with_anchor(self.line, off)
        self.diags.emit_code(code, path, slots)

    # --- walk (depth-guard + type dispatch) ----------------------------------

    def walk(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> bool:
        self.depth += 1
        try:
            before = len(self.diags)
            if self.depth > MAX_VALIDATION_DEPTH:
                self.emit(
                    "STRICTSPEC_DEPTH_EXCEEDED",
                    path,
                    n,
                    {"limit": diag.SlotInt(MAX_VALIDATION_DEPTH)},
                )
                return False
            self.walk_inner(t, n, path)
            clean = len(self.diags) == before
            if n is not None:
                self.clean[id(n)] = clean
            return clean
        finally:
            self.depth -= 1

    def walk_inner(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        k = t.kind
        if k == schema.SKind.REF:
            named = self.s.types.get(t.ref)
            if named is not None:
                self.walk_inner(named, n, path)
                return
            self.walk_scalar(t, n, path)
        elif k == schema.SKind.RECORD:
            self.walk_record(t, n, path)
        elif k == schema.SKind.MAP:
            self.walk_map(t, n, path)
        elif k == schema.SKind.ARRAY:
            self.walk_array(t, n, path)
        elif k == schema.SKind.TUPLE:
            self.walk_tuple(t, n, path)
        elif k == schema.SKind.ENUM:
            self.walk_enum(t, n, path)
        elif k == schema.SKind.LITERAL:
            self.walk_literal(t, n, path)
        elif k == schema.SKind.DISCRIMINATED_UNION:
            self.walk_discriminated(t, n, path)
        elif k == schema.SKind.NODE_KIND_UNION:
            self.walk_node_kind(t, n, path)
        elif k == schema.SKind.NULLABLE:
            self.walk_nullable(t, n, path)
        elif k == schema.SKind.OPAQUE:
            pass  # verbatim leaf: never introspected

    def walk_record(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is None or n.kind != doc.Kind.RECORD:
            self.emit(
                "STRICTSPEC_TYPE_NOT_RECORD",
                path,
                n,
                {"got": diag.SlotString(_node_kind_name(_kind_of(n)))},
            )
            return
        is_root = len(path.steps) == 1

        if len(t.constraints) > 0:
            self.phase2.append(_P2Task(typ=t, rec=n, path=path))

        field_names = [f.name for f in t.fields]
        matched: set[str] = set()

        doc_index: dict[str, int] = {}
        for i, e in enumerate(n.entries):
            if e.key not in doc_index:
                doc_index[e.key] = i

        ACT_PRESENT = 0
        ACT_ALIAS = 1

        present: list[dict] = []
        missing: list[str] = []
        missing_idx: list[int] = []

        for i, f in enumerate(t.fields):
            found: list[str] = []
            if _has_key(n, f.name):
                found.append(f.name)
            for a in f.aliases:
                if _has_key(n, a):
                    found.append(a)
            if len(found) >= 2:
                alias_name = found[0]
                for fn in found:
                    if fn != f.name:
                        alias_name = fn
                        break
                pos = doc_index[found[0]]
                for fn in found:
                    matched.add(fn)
                    if doc_index[fn] < pos:
                        pos = doc_index[fn]
                present.append(
                    {
                        "kind": ACT_ALIAS,
                        "name": f.name,
                        "alias": alias_name,
                        "decl_idx": i,
                        "doc_pos": pos,
                        "before": [],
                    }
                )
                continue
            if len(found) == 1:
                key = found[0]
                matched.add(key)
                present.append(
                    {
                        "kind": ACT_PRESENT,
                        "name": f.name,
                        "typ": f.type,
                        "key": key,
                        "decl_idx": i,
                        "doc_pos": doc_index[key],
                        "before": [],
                    }
                )
                continue
            if f.required:
                missing.append(f.name)
                missing_idx.append(i)

        # Anchor each missing-required field before the first present field
        # declared after it (declaration-order merge; present still in decl order).
        mi = 0
        for pa in present:
            while mi < len(missing) and missing_idx[mi] < pa["decl_idx"]:
                pa["before"].append(missing[mi])
                mi += 1
        trailing = missing[mi:]

        # Reorder present-key emissions to document order (stable).
        present.sort(key=lambda a: a["doc_pos"])

        def emit_missing(names: list[str]) -> None:
            for name in names:
                self.emit(
                    "STRICTSPEC_TYPE_MISSING_REQUIRED",
                    path,
                    n,
                    {"key": diag.SlotString(name)},
                )

        for pa in present:
            emit_missing(pa["before"])
            if pa["kind"] == ACT_ALIAS:
                self.emit(
                    "STRICTSPEC_ALIAS_BOTH_PRESENT",
                    path,
                    n,
                    {
                        "alias": diag.SlotIdentifier(pa["alias"]),
                        "canonical": diag.SlotIdentifier(pa["name"]),
                    },
                )
            else:
                val = _entry_of(n, pa["key"])
                self.walk(pa["typ"], val, diag.append_key(path, pa["name"]))
        emit_missing(trailing)

        # Unknown-key pass (document order). Root format_version is exempt.
        for e in n.entries:
            if e.key in matched:
                continue
            if is_root and e.key == "format_version":
                continue
            self.emit(
                "STRICTSPEC_KEY_UNKNOWN",
                path,
                n,
                {
                    "key": diag.SlotString(e.key),
                    "suggestion": diag.SlotSuggestion(e.key, tuple(field_names)),
                },
            )

    def walk_map(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is None or n.kind != doc.Kind.RECORD:
            self.emit(
                "STRICTSPEC_TYPE_NOT_MAP",
                path,
                n,
                {"got": diag.SlotString(_node_kind_name(_kind_of(n)))},
            )
            return
        key_re = _compile_regex(t.key_pattern) if t.key_pattern != "" else None
        for e in n.entries:
            kp = diag.append_map_key(path, e.key)
            if key_re is not None and key_re.search(e.key) is None:
                self.emit(
                    "STRICTSPEC_VALUE_MAP_KEY_REGEX",
                    kp,
                    e.value,
                    {
                        "key": diag.SlotString(e.key),
                        "pattern": diag.SlotValue(diag.StringVal(t.key_pattern)),
                    },
                )
            if t.value is not None:
                self.walk(t.value, e.value, kp)

    def walk_array(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is None or n.kind != doc.Kind.ARRAY:
            self.emit(
                "STRICTSPEC_TYPE_NOT_ARRAY",
                path,
                n,
                {"got": diag.SlotString(_node_kind_name(_kind_of(n)))},
            )
            return
        items = n.items
        if t.min_len is not None and len(items) < t.min_len:
            self.emit(
                "STRICTSPEC_VALUE_ARRAY_TOO_SHORT",
                path,
                n,
                {"actual": diag.SlotInt(len(items)), "limit": diag.SlotInt(t.min_len)},
            )
        if t.max_len is not None and len(items) > t.max_len:
            self.emit(
                "STRICTSPEC_VALUE_ARRAY_TOO_LONG",
                path,
                n,
                {"actual": diag.SlotInt(len(items)), "limit": diag.SlotInt(t.max_len)},
            )
        if t.item is not None:
            for i, it in enumerate(items):
                self.walk(t.item, it, diag.append_index(path, i))

    def walk_tuple(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is None or n.kind != doc.Kind.ARRAY:
            self.emit(
                "STRICTSPEC_TYPE_MISMATCH",
                path,
                n,
                {
                    "expected": diag.SlotString("tuple"),
                    "got": diag.SlotString(_node_kind_name(_kind_of(n))),
                },
            )
            return
        items = n.items
        if len(items) != len(t.elements):
            self.emit(
                "STRICTSPEC_TYPE_TUPLE_ARITY",
                path,
                n,
                {"expected": diag.SlotInt(len(t.elements)), "got": diag.SlotInt(len(items))},
            )
            return
        for i, elem_ref in enumerate(t.elements):
            et = schema.Type(kind=schema.SKind.REF, ref=elem_ref)
            self.walk(et, items[i], diag.append_index(path, i))

    def walk_nullable(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is not None and n.kind == doc.Kind.NULL:
            return
        if t.inner is not None:
            self.walk_inner(t.inner, n, path)

    def walk_discriminated(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is None or n.kind != doc.Kind.RECORD:
            self.emit(
                "STRICTSPEC_TYPE_MISMATCH",
                path,
                n,
                {
                    "expected": diag.SlotString("record"),
                    "got": diag.SlotString(_node_kind_name(_kind_of(n))),
                },
            )
            return
        disc_strs: list[diag.Value] = []
        arm_disc: list[str] = []
        for arm in t.arms:
            dv = self.arm_discriminator(arm, t.discriminator)
            arm_disc.append(dv)
            disc_strs.append(diag.StringVal(dv))
        disc_node = _entry_of(n, t.discriminator)
        if disc_node is None:
            self.emit(
                "STRICTSPEC_UNION_DISCRIMINATOR_MISSING",
                path,
                n,
                {
                    "key": diag.SlotString(t.discriminator),
                    "expected": diag.SlotList(tuple(disc_strs)),
                },
            )
            return
        got = self.scalar_key_string(disc_node)
        for i, arm in enumerate(t.arms):
            if arm_disc[i] == got:
                self.walk_inner(arm.type, n, diag.append_arm(path, arm.name))
                return
        self.emit(
            "STRICTSPEC_UNION_DISCRIMINATOR_UNKNOWN",
            path,
            disc_node,
            {
                "got": diag.SlotValue(self.value_of(disc_node)),
                "expected": diag.SlotList(tuple(disc_strs)),
                "suggestion": diag.SlotSuggestion(got, tuple(arm_disc)),
            },
        )

    def walk_node_kind(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        cat = _node_category(_kind_of(n))
        kinds: list[diag.Value] = []
        for arm in t.arms:
            ac = self.arm_category(arm.type)
            kinds.append(diag.StringVal(ac))
            if ac == cat:
                self.walk_inner(arm.type, n, path)
                return
        self.emit(
            "STRICTSPEC_UNION_NODE_KIND",
            path,
            n,
            {"got": diag.SlotString(cat), "expected": diag.SlotList(tuple(kinds))},
        )

    def walk_enum(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        members = self.enum_members(t)
        member_vals = tuple(diag.StringVal(m) for m in members)
        if t.sourced or _all_string_enum(t):
            if n is None or n.kind != doc.Kind.STRING:
                self.emit_enum_miss(t, n, path, members, member_vals)
                return
            val = self.decode_string(n)
            if val in members:
                return
            self.emit_enum_miss(t, n, path, members, member_vals)
            return
        if n is None:
            self.emit_enum_miss(t, n, path, members, member_vals)
            return
        for ev in t.enum_values:
            if self.same_scalar(ev, n):
                return
        self.emit_enum_miss(t, n, path, members, member_vals)

    def emit_enum_miss(self, t, n, path, members, member_vals) -> None:
        got = ""
        if n is not None and n.kind == doc.Kind.STRING:
            got = self.decode_string(n)
        self.emit(
            "STRICTSPEC_TYPE_NOT_ENUM_MEMBER",
            path,
            n,
            {
                "got": diag.SlotValue(self.value_of(n)),
                "expected": diag.SlotList(tuple(member_vals)),
                "suggestion": diag.SlotSuggestion(got, tuple(members)),
            },
        )

    def walk_literal(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is not None and self.same_scalar(t.literal, n):
            return
        self.emit(
            "STRICTSPEC_TYPE_NOT_LITERAL",
            path,
            n,
            {
                "expected": diag.SlotValue(_sval_to_value(t.literal)),
                "got": diag.SlotValue(self.value_of(n)),
            },
        )

    # --- union / enum helpers ------------------------------------------------

    def arm_discriminator(self, arm: schema.Arm, disc_field: str) -> str:
        rec = self.resolve_record(arm.type)
        if rec is not None:
            for f in rec.fields:
                if f.name == disc_field and f.type is not None and f.type.kind == schema.SKind.LITERAL:
                    return _sval_key_string(f.type.literal)
        return arm.name

    def resolve_record(self, t: schema.Type | None) -> schema.Type | None:
        seen = 0
        while t is not None and t.kind == schema.SKind.REF and seen < 32:
            named = self.s.types.get(t.ref)
            if named is None:
                return None
            t = named
            seen += 1
        if t is not None and t.kind == schema.SKind.RECORD:
            return t
        return t

    def arm_category(self, t: schema.Type | None) -> str:
        seen = 0
        while t is not None and t.kind == schema.SKind.REF:
            named = self.s.types.get(t.ref)
            if named is not None and seen < 32:
                t = named
                seen += 1
                continue
            return "scalar"
        if t is None:
            return "scalar"
        if t.kind in (schema.SKind.RECORD, schema.SKind.MAP):
            return "record"
        if t.kind in (schema.SKind.ARRAY, schema.SKind.TUPLE):
            return "array"
        return "scalar"

    def enum_members(self, t: schema.Type) -> list[str]:
        if t.sourced:
            return t.baked
        return [_sval_key_string(ev) for ev in t.enum_values]

    def scalar_key_string(self, n: doc.Node | None) -> str:
        if n is None:
            return ""
        if n.kind == doc.Kind.STRING:
            return self.decode_string(n)
        return n.lexeme

    def same_scalar(self, sv: schema.SVal, n: doc.Node | None) -> bool:
        if n is None:
            return False
        if sv.kind == doc.Kind.STRING:
            return n.kind == doc.Kind.STRING and self.decode_string(n) == sv.s
        if sv.kind == doc.Kind.INTEGER:
            return n.kind == doc.Kind.INTEGER and _sval_int_lexeme(n.lexeme) == sv.i
        if sv.kind == doc.Kind.BOOL:
            return n.kind == doc.Kind.BOOL and (n.lexeme == "true") == sv.b
        if sv.kind == doc.Kind.FLOAT:
            return n.kind == doc.Kind.FLOAT and n.lexeme == sv.lexeme
        return False

    # --- string/value helpers ------------------------------------------------

    def decode_string(self, n: doc.Node | None) -> str:
        if n is None:
            return ""
        if self.format == doc.FORMAT_TOML:
            return strdecode.decode_toml(n.lexeme)
        return strdecode.decode_json(n.lexeme)

    def value_of(self, n: doc.Node | None) -> diag.Value:
        if n is None:
            return diag.NullVal()
        k = n.kind
        if k == doc.Kind.STRING:
            return diag.StringVal(self.decode_string(n))
        if k == doc.Kind.INTEGER:
            return diag.NumberVal(lexeme=n.lexeme, int_class=True)
        if k == doc.Kind.FLOAT:
            return diag.FloatVal(lexeme=n.lexeme, has_lexeme=True)
        if k == doc.Kind.BOOL:
            return diag.BoolVal(n.lexeme == "true")
        if k == doc.Kind.NULL:
            return diag.NullVal()
        if k == doc.Kind.DATE_LOCAL:
            return diag.DateVal(self.decode_string(n))
        if k == doc.Kind.TIME_LOCAL:
            return diag.TimeVal(self.decode_string(n))
        if k in (doc.Kind.DATETIME_OFFSET, doc.Kind.DATETIME_LOCAL):
            return diag.DatetimeVal(self.decode_string(n))
        return diag.StringVal(n.lexeme)

    # --- scalar validation ---------------------------------------------------

    def walk_scalar(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        ref = t.ref
        if ref == "string":
            self.validate_string(t, n, path)
        elif ref == "integer":
            self.validate_integer(t, n, path)
        elif ref == "float":
            self.validate_float(t, n, path)
        elif ref == "number":
            self.validate_number(t, n, path)
        elif ref == "boolean":
            self.validate_bool(t, n, path)
        elif ref in ("date", "time", "datetime"):
            self.validate_datetime(t, n, path)
        else:
            cs = self.scalars.get(ref)
            if cs is not None:
                self.validate_custom_scalar(cs, n, path)
            # Unknown ref: skip silently.

    def validate_string(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is None or n.kind != doc.Kind.STRING:
            self.emit(
                "STRICTSPEC_TYPE_NOT_STRING",
                path,
                n,
                {"got": diag.SlotString(_node_kind_name(_kind_of(n)))},
            )
            return
        val = self.decode_string(n)
        length = len(val)
        if t.non_empty and length == 0:
            self.emit("STRICTSPEC_VALUE_STRING_EMPTY", path, n, {})
        if t.min_length is not None and length < t.min_length:
            self.emit(
                "STRICTSPEC_VALUE_STRING_TOO_SHORT",
                path,
                n,
                {"actual": diag.SlotInt(length), "limit": diag.SlotInt(t.min_length)},
            )
        if t.max_length is not None and length > t.max_length:
            self.emit(
                "STRICTSPEC_VALUE_STRING_TOO_LONG",
                path,
                n,
                {"actual": diag.SlotInt(length), "limit": diag.SlotInt(t.max_length)},
            )
        if t.has_regex and _compile_regex(t.regex).search(val) is None:
            self.emit(
                "STRICTSPEC_VALUE_STRING_REGEX",
                path,
                n,
                {
                    "actual": diag.SlotValue(diag.StringVal(val)),
                    "pattern": diag.SlotValue(diag.StringVal(t.regex)),
                },
            )

    def validate_integer(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is None or n.kind != doc.Kind.INTEGER:
            got = _node_kind_name(_kind_of(n))
            if n is not None and n.kind == doc.Kind.FLOAT:
                got = "float"
            self.emit("STRICTSPEC_TYPE_NOT_INTEGER", path, n, {"got": diag.SlotString(got)})
            return
        iv = schema._go_parse_int(n.lexeme)
        if iv is None:
            self.emit(
                "STRICTSPEC_NUM_INT_OVERFLOW",
                path,
                n,
                {"actual": diag.SlotValue(self.value_of(n))},
            )
            return
        if self.s.safe_integers and abs(iv) >= (1 << 53):
            self.emit(
                "STRICTSPEC_NUM_SAFE_INTEGER",
                path,
                n,
                {"actual": diag.SlotValue(self.value_of(n))},
            )
        self.check_numeric_bounds(t, n, path, float(iv))

    def validate_float(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is None or n.kind != doc.Kind.FLOAT:
            got = _node_kind_name(_kind_of(n))
            if n is not None and n.kind == doc.Kind.INTEGER:
                got = "integer"
            self.emit("STRICTSPEC_TYPE_NOT_FLOAT", path, n, {"got": diag.SlotString(got)})
            return
        f = schema._go_parse_float(n.lexeme)
        if f is None:
            self.emit(
                "STRICTSPEC_NUM_FLOAT_OVERFLOW",
                path,
                n,
                {"actual": diag.SlotValue(self.value_of(n))},
            )
            return
        self.check_numeric_bounds(t, n, path, f)

    def validate_number(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is None or n.kind not in (doc.Kind.INTEGER, doc.Kind.FLOAT):
            self.emit(
                "STRICTSPEC_TYPE_MISMATCH",
                path,
                n,
                {
                    "expected": diag.SlotString("number"),
                    "got": diag.SlotString(_node_kind_name(_kind_of(n))),
                },
            )
            return
        if n.kind == doc.Kind.INTEGER and not _exactly_representable(n.lexeme):
            self.emit(
                "STRICTSPEC_NUM_UNREPRESENTABLE",
                path,
                n,
                {"actual": diag.SlotValue(self.value_of(n))},
            )
            return
        f = schema._go_parse_float(n.lexeme)
        if f is None:
            self.emit(
                "STRICTSPEC_NUM_UNREPRESENTABLE",
                path,
                n,
                {"actual": diag.SlotValue(self.value_of(n))},
            )
            return
        self.check_numeric_bounds(t, n, path, f)

    def validate_bool(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        if n is None or n.kind != doc.Kind.BOOL:
            self.emit(
                "STRICTSPEC_TYPE_NOT_BOOLEAN",
                path,
                n,
                {"got": diag.SlotString(_node_kind_name(_kind_of(n)))},
            )

    def check_numeric_bounds(self, t: schema.Type, n: doc.Node, path: diag.Path, val: float) -> None:
        if t.min is not None and val < _sval_num(t.min):
            self.emit(
                "STRICTSPEC_VALUE_NUM_TOO_SMALL",
                path,
                n,
                {
                    "actual": diag.SlotValue(self.value_of(n)),
                    "limit": diag.SlotValue(_sval_to_value(t.min)),
                },
            )
        if t.exclusive_min is not None and val <= _sval_num(t.exclusive_min):
            self.emit(
                "STRICTSPEC_VALUE_NUM_TOO_SMALL_EXCLUSIVE",
                path,
                n,
                {
                    "actual": diag.SlotValue(self.value_of(n)),
                    "limit": diag.SlotValue(_sval_to_value(t.exclusive_min)),
                },
            )
        if t.max is not None and val > _sval_num(t.max):
            self.emit(
                "STRICTSPEC_VALUE_NUM_TOO_LARGE",
                path,
                n,
                {
                    "actual": diag.SlotValue(self.value_of(n)),
                    "limit": diag.SlotValue(_sval_to_value(t.max)),
                },
            )
        if t.exclusive_max is not None and val >= _sval_num(t.exclusive_max):
            self.emit(
                "STRICTSPEC_VALUE_NUM_TOO_LARGE_EXCLUSIVE",
                path,
                n,
                {
                    "actual": diag.SlotValue(self.value_of(n)),
                    "limit": diag.SlotValue(_sval_to_value(t.exclusive_max)),
                },
            )

    def validate_custom_scalar(self, cs: schema.Scalar, n: doc.Node | None, path: diag.Path) -> None:
        if cs.base == "string" and (n is None or n.kind != doc.Kind.STRING):
            self.emit(
                "STRICTSPEC_SCALAR_BASE_MISMATCH",
                path,
                n,
                {
                    "expected": diag.SlotString(cs.base),
                    "name": diag.SlotIdentifier(cs.name),
                },
            )
            return
        val = self.decode_string(n)
        length = len(val)
        if cs.len_min is not None and length < cs.len_min:
            self.emit_scalar_length(cs, n, path, length, cs.len_min)
            return
        if cs.non_empty and length == 0:
            self.emit_scalar_length(cs, n, path, length, 1)
            return
        if cs.len_max is not None and length > cs.len_max:
            self.emit_scalar_length(cs, n, path, length, cs.len_max)
            return
        if cs.lexeme_rule != "" and _compile_regex(cs.lexeme_rule).search(val) is None:
            self.emit(
                "STRICTSPEC_SCALAR_LEXEME",
                path,
                n,
                {
                    "actual": diag.SlotValue(diag.StringVal(val)),
                    "name": diag.SlotIdentifier(cs.name),
                    "pattern": diag.SlotValue(diag.StringVal(cs.lexeme_rule)),
                },
            )

    def emit_scalar_length(self, cs, n, path, actual, limit) -> None:
        self.emit(
            "STRICTSPEC_SCALAR_LENGTH",
            path,
            n,
            {
                "name": diag.SlotIdentifier(cs.name),
                "actual": diag.SlotInt(actual),
                "limit": diag.SlotInt(limit),
            },
        )

    # --- datetime ------------------------------------------------------------

    def validate_datetime(self, t: schema.Type, n: doc.Node | None, path: diag.Path) -> None:
        form = ""
        if n is not None and n.kind == doc.Kind.STRING:
            form = _classify_rfc3339(self.decode_string(n))
        elif n is not None:
            k = n.kind
            if k == doc.Kind.DATE_LOCAL:
                form = "date"
            elif k == doc.Kind.TIME_LOCAL:
                form = "time"
            elif k == doc.Kind.DATETIME_OFFSET:
                form = "datetime-offset"
            elif k == doc.Kind.DATETIME_LOCAL:
                form = "datetime-local"
        ref = t.ref
        if ref == "date":
            if form != "date":
                self.emit(
                    "STRICTSPEC_TYPE_NOT_DATE",
                    path,
                    n,
                    {"got": diag.SlotString(_form_got(form, n))},
                )
            return
        if ref == "time":
            if form != "time":
                self.emit(
                    "STRICTSPEC_TYPE_NOT_TIME",
                    path,
                    n,
                    {"got": diag.SlotString(_form_got(form, n))},
                )
            return
        if ref == "datetime":
            if form not in ("datetime-offset", "datetime-local"):
                self.emit(
                    "STRICTSPEC_TYPE_NOT_DATETIME",
                    path,
                    n,
                    {"got": diag.SlotString(_form_got(form, n))},
                )
                return
            want = t.datetime_kind  # "offset" | "local"
            got = "local" if form == "datetime-local" else "offset"
            if want != "" and want != got:
                self.emit(
                    "STRICTSPEC_TYPE_DATETIME_KIND",
                    path,
                    n,
                    {"expected": diag.SlotString(want), "got": diag.SlotString(got)},
                )
                return
            self.check_datetime_range(t, n, path)

    def check_datetime_range(self, t: schema.Type, n: doc.Node, path: diag.Path) -> None:
        val = self.decode_string(n)
        vi = _parse_instant(val)
        if vi is None:
            return
        if t.min is not None and t.min.kind == doc.Kind.STRING:
            mi = _parse_instant(t.min.s)
            if mi is not None and vi < mi:
                self.emit(
                    "STRICTSPEC_VALUE_DATETIME_BEFORE",
                    path,
                    n,
                    {
                        "actual": diag.SlotValue(self.value_of(n)),
                        "limit": diag.SlotValue(diag.DatetimeVal(t.min.s)),
                    },
                )
        if t.max is not None and t.max.kind == doc.Kind.STRING:
            ma = _parse_instant(t.max.s)
            if ma is not None and vi > ma:
                self.emit(
                    "STRICTSPEC_VALUE_DATETIME_AFTER",
                    path,
                    n,
                    {
                        "actual": diag.SlotValue(self.value_of(n)),
                        "limit": diag.SlotValue(diag.DatetimeVal(t.max.s)),
                    },
                )

    # --- conditions ----------------------------------------------------------

    def eval_condition(self, rec: doc.Node, c: schema.Condition | None) -> bool:
        if c is None:
            return False
        fn = _entry_of(rec, c.field)
        present = fn is not None
        p = c.predicate
        if p == "present":
            return present
        if p == "absent":
            return not present
        if p == "equals":
            return present and self.same_scalar(c.value, fn)
        if p == "not-equals":
            return present and not self.same_scalar(c.value, fn)
        if p == "in":
            if not present:
                return False
            return any(self.same_scalar(val, fn) for val in c.values)
        if p == "not-in":
            if not present:
                return False
            return not any(self.same_scalar(val, fn) for val in c.values)
        return False

    # --- phase 2 constraints -------------------------------------------------

    def run_constraints(self, task: _P2Task) -> None:
        rec, path = task.rec, task.path
        for c in task.typ.constraints:
            form = c.form
            if form == "conditional-required":
                if self.eval_condition(rec, c.when) and not _has_key(rec, c.field):
                    self.emit(
                        "STRICTSPEC_INTRA_CONDITIONAL_REQUIRED",
                        path,
                        rec,
                        {
                            "key": diag.SlotString(c.field),
                            "condition": diag.SlotString(_render_condition(c.when)),
                        },
                    )
            elif form == "forbidden-when":
                if self.eval_condition(rec, c.when) and _has_key(rec, c.field):
                    fn = _entry_of(rec, c.field)
                    self.emit(
                        "STRICTSPEC_INTRA_FORBIDDEN_WHEN",
                        diag.append_key(path, c.field),
                        fn,
                        {
                            "key": diag.SlotString(c.field),
                            "condition": diag.SlotString(_render_condition(c.when)),
                        },
                    )
            elif form == "conditional-value":
                fn = _entry_of(rec, c.field)
                if (
                    fn is not None
                    and self.eval_condition(rec, c.when)
                    and not self.same_scalar(c.equals_literal, fn)
                ):
                    self.emit(
                        "STRICTSPEC_INTRA_CONDITIONAL_VALUE",
                        diag.append_key(path, c.field),
                        fn,
                        {
                            "key": diag.SlotString(c.field),
                            "expected": diag.SlotValue(_sval_to_value(c.equals_literal)),
                            "got": diag.SlotValue(self.value_of(fn)),
                            "condition": diag.SlotString(_render_condition(c.when)),
                        },
                    )
            elif form == "exactly-one-of":
                present = _present_of(rec, c.fields)
                if len(present) != 1:
                    self.emit(
                        "STRICTSPEC_INTRA_EXACTLY_ONE_OF",
                        path,
                        rec,
                        {"fields": _str_list(c.fields), "actual": _str_list(present)},
                    )
            elif form == "at-least-one-of":
                if len(_present_of(rec, c.fields)) == 0:
                    self.emit(
                        "STRICTSPEC_INTRA_AT_LEAST_ONE_OF",
                        path,
                        rec,
                        {"fields": _str_list(c.fields)},
                    )
            elif form == "co-presence":
                present = _present_of(rec, c.fields)
                if len(present) != 0 and len(present) != len(c.fields):
                    self.emit(
                        "STRICTSPEC_INTRA_CO_PRESENCE",
                        path,
                        rec,
                        {"fields": _str_list(c.fields), "actual": _str_list(present)},
                    )
            elif form == "mutual-exclusion":
                present = _present_of(rec, c.fields)
                if len(present) >= 2:
                    self.emit(
                        "STRICTSPEC_INTRA_MUTUAL_EXCLUSION",
                        path,
                        rec,
                        {"fields": _str_list(c.fields), "actual": _str_list(present)},
                    )
            elif form == "collections-disjoint":
                self.collections_disjoint(rec, path, c)
            elif form == "unique-by":
                self.unique_by(rec, path, c)
            elif form == "pairwise-distinct":
                self.pairwise_distinct(rec, path, c)
            elif form == "ranges-disjoint":
                self.ranges_disjoint(rec, path, c)
            elif form == "ordered-pair":
                self.ordered_pair(rec, path, c)
            elif form == "intra-document-references":
                self.intra_references(rec, path, c)
            elif form == "count-limit":
                self.count_limit(path, c)
            elif form == "sum-limit":
                self.sum_limit(path, c)

    def collections_disjoint(self, rec, path, c) -> None:
        left = self.string_elems(rec, c.left)
        right = self.string_elems(rec, c.right)
        seen = {_normalize(s, c.normalization) for s in left}
        for s in right:
            if _normalize(s, c.normalization) in seen:
                self.emit(
                    "STRICTSPEC_INTRA_COLLECTIONS_DISJOINT",
                    path,
                    rec,
                    {
                        "fields": _str_list([c.left, c.right]),
                        "value": diag.SlotValue(diag.StringVal(s)),
                        "normalization": diag.SlotString(_norm_or(c.normalization)),
                    },
                )
                return

    def unique_by(self, rec, path, c) -> None:
        coll = _entry_of(rec, c.collection)
        if coll is None or coll.kind != doc.Kind.ARRAY:
            return
        seen: set[str] = set()
        for elem in coll.items:
            fn = _entry_of(elem, c.uniq_field)
            if fn is None or fn.kind != doc.Kind.STRING:
                continue
            val = self.decode_string(fn)
            key = _normalize(val, c.normalization)
            if key in seen:
                self.emit(
                    "STRICTSPEC_INTRA_UNIQUE_BY",
                    diag.append_key(path, c.collection),
                    coll,
                    {
                        "value": diag.SlotValue(diag.StringVal(val)),
                        "field": diag.SlotString(c.uniq_field),
                        "normalization": diag.SlotString(_norm_or(c.normalization)),
                    },
                )
                return
            seen.add(key)

    def pairwise_distinct(self, rec, path, c) -> None:
        coll = _entry_of(rec, c.collection)
        if coll is None or coll.kind != doc.Kind.ARRAY:
            return
        seen: set[str] = set()
        for elem in coll.items:
            if elem.kind != doc.Kind.STRING:
                continue
            val = self.decode_string(elem)
            key = _normalize(val, c.normalization)
            if key in seen:
                self.emit(
                    "STRICTSPEC_INTRA_PAIRWISE_DISTINCT",
                    diag.append_key(path, c.collection),
                    coll,
                    {
                        "value": diag.SlotValue(diag.StringVal(val)),
                        "normalization": diag.SlotString(_norm_or(c.normalization)),
                    },
                )
                return
            seen.add(key)

    def ranges_disjoint(self, rec, path, c) -> None:
        coll = _entry_of(rec, c.collection)
        if coll is None or coll.kind != doc.Kind.ARRAY:
            return
        ranges: list[tuple[int, int]] = []
        for elem in coll.items:
            s = _int_field(elem, c.start)
            length = _int_field(elem, c.length)
            if s is None or length is None:
                continue
            ranges.append((s, s + length))

        def fmt_range(r: tuple[int, int]) -> str:
            return "[" + str(r[0]) + ", " + str(r[1]) + ")"

        for i in range(len(ranges)):
            for j in range(i):
                a, b = ranges[j], ranges[i]
                if a[0] < b[1] and b[0] < a[1]:
                    self.emit(
                        "STRICTSPEC_INTRA_RANGES_DISJOINT",
                        diag.append_key(path, c.collection),
                        coll,
                        {
                            "value": diag.SlotString(fmt_range(b)),
                            "actual": diag.SlotString(fmt_range(a)),
                        },
                    )
                    return

    def ordered_pair(self, rec, path, c) -> None:
        ln = _num_field(rec, c.less)
        tn = _num_field(rec, c.than)
        if ln is None or tn is None:
            return
        if not (ln < tn):
            self.emit(
                "STRICTSPEC_INTRA_ORDERED_PAIR",
                path,
                rec,
                {"actual": diag.SlotString(c.less), "value": diag.SlotString(c.than)},
            )

    def intra_references(self, rec, path, c) -> None:
        keyset = self.root_keyset(c.resolves_into)
        if keyset is None:
            return
        if "[]." in c.reference:
            parts = c.reference.split("[].", 1)
            coll = _entry_of(self.root, parts[0])
            if coll is None or coll.kind != doc.Kind.ARRAY:
                return
            for i, elem in enumerate(coll.items):
                fn = _entry_of(elem, parts[1])
                if fn is None or fn.kind != doc.Kind.STRING:
                    continue
                val = self.decode_string(fn)
                if val not in keyset:
                    p = diag.append_key(
                        diag.append_index(diag.append_key(diag.new_path(), parts[0]), i),
                        parts[1],
                    )
                    self.emit(
                        "STRICTSPEC_INTRA_REFERENCE_UNRESOLVED",
                        p,
                        fn,
                        {"value": diag.SlotValue(diag.StringVal(val))},
                    )
            return
        ref_node = _entry_of(rec, c.reference)
        if ref_node is None:
            return
        if ref_node.kind == doc.Kind.ARRAY:
            for i, elem in enumerate(ref_node.items):
                if elem.kind != doc.Kind.STRING:
                    continue
                val = self.decode_string(elem)
                if val not in keyset:
                    self.emit(
                        "STRICTSPEC_INTRA_REFERENCE_UNRESOLVED",
                        diag.append_index(diag.append_key(path, c.reference), i),
                        elem,
                        {"value": diag.SlotValue(diag.StringVal(val))},
                    )
        elif ref_node.kind == doc.Kind.STRING:
            val = self.decode_string(ref_node)
            if val not in keyset:
                self.emit(
                    "STRICTSPEC_INTRA_REFERENCE_UNRESOLVED",
                    diag.append_key(path, c.reference),
                    ref_node,
                    {"value": diag.SlotValue(diag.StringVal(val))},
                )
        elif ref_node.kind == doc.Kind.RECORD:
            for e in ref_node.entries:
                if e.key not in keyset:
                    self.emit(
                        "STRICTSPEC_INTRA_REFERENCE_UNRESOLVED",
                        diag.append_key(path, c.reference),
                        ref_node,
                        {"value": diag.SlotValue(diag.StringVal(e.key))},
                    )
        # Null: nullable reference short-circuits.

    def root_keyset(self, name: str) -> set[str] | None:
        coll = _entry_of(self.root, name)
        if coll is None:
            return None
        s: set[str] = set()
        if coll.kind == doc.Kind.RECORD:
            for e in coll.entries:
                s.add(e.key)
        elif coll.kind == doc.Kind.ARRAY:
            for elem in coll.items:
                if elem.kind == doc.Kind.STRING:
                    s.add(self.decode_string(elem))
                elif elem.kind == doc.Kind.RECORD:
                    nn = _entry_of(elem, "name")
                    if nn is not None and nn.kind == doc.Kind.STRING:
                        s.add(self.decode_string(nn))
        return s

    def count_limit(self, path, c) -> None:
        docs = self.evidence.get(c.selection)
        if docs is None:
            return
        count = len(docs)
        limit = c.limit.i
        violated = (c.compare == "<=" and count > limit) or (c.compare == ">=" and count < limit)
        if violated:
            self.emit(
                "STRICTSPEC_CROSS_COUNT_LIMIT",
                path,
                self.root,
                {
                    "actual": diag.SlotInt(count),
                    "source": diag.SlotString(c.selection),
                    "limit": diag.SlotInt(limit),
                },
            )

    def sum_limit(self, path, c) -> None:
        docs = self.evidence.get(c.selection)
        if docs is None:
            return
        total = 0.0
        all_int = True
        for d in docs:
            present = c.sum_field in d
            f, numeric = _as_float(d.get(c.sum_field))
            if not present or not numeric:
                self.emit(
                    "STRICTSPEC_CROSS_SUM_FIELD_MISSING",
                    path,
                    self.root,
                    {
                        "source": diag.SlotString(c.selection),
                        "field": diag.SlotString(c.sum_field),
                        "actual": diag.SlotString(_doc_name(d)),
                    },
                )
                return
            if f != float(int(f)):
                all_int = False
            total += f
        limit = _sval_num(c.limit)
        violated = (c.compare == "<=" and total > limit) or (c.compare == ">=" and total < limit)
        if violated:
            self.emit(
                "STRICTSPEC_CROSS_SUM_LIMIT",
                path,
                self.root,
                {
                    "field": diag.SlotString(c.sum_field),
                    "source": diag.SlotString(c.selection),
                    "actual": diag.SlotValue(_sum_value(total, all_int)),
                    "limit": diag.SlotValue(_sval_to_value(c.limit)),
                },
            )

    def string_elems(self, rec, field_name) -> list[str]:
        n = _entry_of(rec, field_name)
        if n is None or n.kind != doc.Kind.ARRAY:
            return []
        return [self.decode_string(e) for e in n.items if e.kind == doc.Kind.STRING]


def execute(p: Program, root: doc.Node | None, opts: ExecOptions) -> list[diag.Diagnostic]:
    """Validate one document (root node) against the compiled Program and return
    the ordered diagnostics: gate first (terminal on failure), then one-pass
    structural accumulation in traversal order, then phase-2 constraints over
    records whose phase 1 passed.
    """
    v = _Exec(p, root, opts)
    if not v.gate(root):
        return v.diags.all()
    rt = v.s.types.get(v.s.root)
    if rt is None:
        return v.diags.all()
    v.walk(rt, root, diag.new_path())
    if not opts.structural_only:
        for task in v.phase2:
            if v.clean.get(id(task.rec)):
                v.run_constraints(task)
    return v.diags.all()


# --- module-level helpers ----------------------------------------------------


def _kind_of(n: doc.Node | None) -> doc.Kind:
    return doc.Kind.NULL if n is None else n.kind


def _node_kind_name(k: doc.Kind) -> str:
    if k == doc.Kind.RECORD:
        return "record"
    if k == doc.Kind.ARRAY:
        return "array"
    if k == doc.Kind.STRING:
        return "string"
    if k == doc.Kind.INTEGER:
        return "integer"
    if k == doc.Kind.FLOAT:
        return "float"
    if k == doc.Kind.BOOL:
        return "boolean"
    if k == doc.Kind.NULL:
        return "null"
    if k in (doc.Kind.DATETIME_OFFSET, doc.Kind.DATETIME_LOCAL):
        return "datetime"
    if k == doc.Kind.DATE_LOCAL:
        return "date"
    if k == doc.Kind.TIME_LOCAL:
        return "time"
    return "value"


def _node_category(k: doc.Kind) -> str:
    if k == doc.Kind.RECORD:
        return "record"
    if k == doc.Kind.ARRAY:
        return "array"
    return "scalar"


def _entry_of(rec: doc.Node | None, key: str) -> doc.Node | None:
    if rec is None or rec.kind != doc.Kind.RECORD:
        return None
    for e in rec.entries:
        if e.key == key:
            return e.value
    return None


def _has_key(rec: doc.Node | None, key: str) -> bool:
    return _entry_of(rec, key) is not None


def _sval_int_lexeme(lexeme: str) -> int:
    n = 0
    neg = False
    for i, c in enumerate(lexeme):
        if i == 0 and c == "-":
            neg = True
            continue
        if c < "0" or c > "9":
            break
        n = n * 10 + (ord(c) - ord("0"))
    return -n if neg else n


def _exactly_representable(lexeme: str) -> bool:
    i = schema._go_parse_int(lexeme)
    if i is None:
        return False
    f = float(i)
    if abs(f) >= (1 << 53):
        return int(f) == i
    return True


def _sval_to_value(sv: schema.SVal) -> diag.Value:
    if sv.kind == doc.Kind.STRING:
        return diag.StringVal(sv.s)
    if sv.kind == doc.Kind.INTEGER:
        return diag.NumberVal(lexeme=sv.lexeme, int_class=True)
    if sv.kind == doc.Kind.FLOAT:
        return diag.FloatVal(lexeme=sv.lexeme, has_lexeme=True)
    if sv.kind == doc.Kind.BOOL:
        return diag.BoolVal(sv.b)
    return diag.StringVal(sv.s)


def _sval_num(sv: schema.SVal) -> float:
    if sv.kind == doc.Kind.INTEGER:
        return float(sv.i)
    return sv.f


def _sval_key_string(sv: schema.SVal) -> str:
    if sv.kind == doc.Kind.STRING:
        return sv.s
    return sv.lexeme


def _all_string_enum(t: schema.Type) -> bool:
    for ev in t.enum_values:
        if ev.kind != doc.Kind.STRING:
            return False
    return len(t.enum_values) > 0


def _present_of(rec: doc.Node, fields: list[str]) -> list[str]:
    return [f for f in fields if _has_key(rec, f)]


def _str_list(names: list[str]) -> diag.SlotList:
    return diag.SlotList(tuple(diag.StringVal(n) for n in names))


def _int_field(rec: doc.Node, field_name: str) -> int | None:
    n = _entry_of(rec, field_name)
    if n is None or n.kind != doc.Kind.INTEGER:
        return None
    return _sval_int_lexeme(n.lexeme)


def _num_field(rec: doc.Node, field_name: str) -> float | None:
    n = _entry_of(rec, field_name)
    if n is None:
        return None
    return _num_of(n)


def _num_of(n: doc.Node | None) -> float | None:
    if n is None:
        return None
    if n.kind in (doc.Kind.INTEGER, doc.Kind.FLOAT):
        return schema._go_parse_float(n.lexeme)
    return None


def _normalize(s: str, mode: str) -> str:
    if mode == "case-fold":
        return s.lower()
    if mode == "trim":
        return s.strip()
    return s


def _norm_or(mode: str) -> str:
    return "none" if mode == "" else mode


def _as_float(v) -> tuple[float, bool]:
    if isinstance(v, bool):
        return 0.0, False
    if isinstance(v, float):
        return v, True
    if isinstance(v, int):
        return float(v), True
    return 0.0, False


def _sum_value(total: float, all_int: bool) -> diag.Value:
    if all_int:
        return diag.NumberVal(lexeme=str(int(total)), int_class=True)
    return diag.FloatVal(f=total)


def _doc_name(d: dict) -> str:
    n = d.get("name")
    if isinstance(n, str):
        return n
    return "<document>"


def _render_condition(c: schema.Condition | None) -> str:
    if c is None:
        return ""
    p = c.predicate
    if p == "present":
        return c.field + " present"
    if p == "absent":
        return c.field + " absent"
    if p == "equals":
        return c.field + " == " + _render_literal(c.value)
    if p == "not-equals":
        return c.field + " != " + _render_literal(c.value)
    if p == "in":
        return c.field + " in [" + _join_literals(c.values) + "]"
    if p == "not-in":
        return c.field + " not in [" + _join_literals(c.values) + "]"
    return ""


def _join_literals(vals: list[schema.SVal]) -> str:
    return ", ".join(_render_literal(sv) for sv in vals)


def _render_literal(sv: schema.SVal) -> str:
    if sv.kind == doc.Kind.STRING:
        return '"' + diag.escape_string(sv.s) + '"'
    if sv.kind == doc.Kind.BOOL:
        return "true" if sv.b else "false"
    return sv.lexeme


def _classify_rfc3339(s: str) -> str:
    if re.fullmatch(r"\d{4}-\d{2}-\d{2}", s):
        return "date"
    if re.fullmatch(r"\d{2}:\d{2}:\d{2}(\.\d+)?", s):
        return "time"
    if "T" in s:
        if re.search(r"(Z|[+-]\d{2}:\d{2})$", s):
            return "datetime-offset"
        return "datetime-local"
    return ""


def _form_got(form: str, n: doc.Node | None) -> str:
    if form in ("datetime-offset", "datetime-local"):
        return "datetime"
    if form != "":
        return form
    return _node_kind_name(_kind_of(n))


def _parse_instant(s: str) -> int | None:
    """Parse an RFC 3339 date-time (offset or local) into a comparable instant
    (nanoseconds). Local forms are interpreted as UTC for a naive comparison.
    """
    from datetime import datetime

    candidates = s
    try:
        # Python 3.11 fromisoformat handles 'Z', offsets, and fractional seconds.
        dt = datetime.fromisoformat(candidates.replace("Z", "+00:00"))
    except ValueError:
        return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return int(dt.timestamp() * 1_000_000_000)
