"""The strictspec meta-schema reader.

A faithful port of go/internal/schema (model.go + sval.go + read.go + load.go).
Parses a schema file (or a type-definition file) authored in the pinned TOML
surface into a typed Schema model, and emits the catalogued
STRICTSPEC_SCHEMA_*/STRICTSPEC_IMPORT_* authoring diagnostics.

Type-reference resolution is DEFERRED to the IR executor, which resolves
against builtins + named types + the manifest's custom scalars. The reader
records reference names verbatim; it never emits a dangling-ref diagnostic, so
it can load a schema without its manifest (the examples/ sweep relies on this).
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field as dfield
from enum import IntEnum

from . import _diag as diag
from . import _doc as doc
from . import _strdecode as strdecode
from . import _tomldoc as tomldoc


class SKind(IntEnum):
    """The category of a type site (appendix-surface-syntax.md sec.3)."""

    REF = 0
    RECORD = 1
    MAP = 2
    ARRAY = 3
    TUPLE = 4
    ENUM = 5
    LITERAL = 6
    DISCRIMINATED_UNION = 7
    NODE_KIND_UNION = 8
    NULLABLE = 9
    OPAQUE = 10


@dataclass
class SVal:
    """A schema-authored literal value: an enum member, a `literal` value, a
    numeric/datetime bound, or a condition operand.
    """

    kind: Kind = doc.Kind.NULL  # type: ignore[name-defined]
    lexeme: str = ""
    s: str = ""
    i: int = 0
    is_int: bool = False
    f: float = 0.0
    b: bool = False


Kind = doc.Kind  # local alias


def sval_from_node(n: doc.Node | None) -> SVal:
    if n is None:
        return SVal()
    s = SVal(kind=n.kind, lexeme=n.lexeme)
    if n.kind == doc.Kind.STRING:
        s.s = strdecode.decode_toml(n.lexeme)
    elif n.kind == doc.Kind.INTEGER:
        v = _go_parse_int(n.lexeme)
        if v is not None:
            s.i = v
            s.is_int = True
    elif n.kind == doc.Kind.FLOAT:
        f = _go_parse_float(n.lexeme)
        if f is not None:
            s.f = f
    elif n.kind == doc.Kind.BOOL:
        s.b = n.lexeme == "true"
    return s


@dataclass
class Field:
    name: str
    type: "Type"
    required: bool = False
    aliases: list[str] = dfield(default_factory=list)


@dataclass
class Arm:
    name: str
    type: "Type"


@dataclass
class Condition:
    field: str = ""
    predicate: str = ""
    value: SVal = dfield(default_factory=SVal)
    has_value: bool = False
    values: list[SVal] = dfield(default_factory=list)


@dataclass
class Constraint:
    form: str = ""
    field: str = ""
    when: Condition | None = None
    equals_literal: SVal = dfield(default_factory=SVal)
    has_equals: bool = False
    fields: list[str] = dfield(default_factory=list)
    left: str = ""
    right: str = ""
    collection: str = ""
    uniq_field: str = ""
    normalization: str = ""
    start: str = ""
    length: str = ""
    less: str = ""
    than: str = ""
    reference: str = ""
    resolves_into: str = ""
    resolves_by: str = ""
    source: str = ""
    selection: str = ""
    compare: str = ""
    limit: SVal = dfield(default_factory=SVal)
    has_limit: bool = False
    sum_field: str = ""


@dataclass
class Type:
    kind: SKind = SKind.REF
    ref: str = ""
    # scalar refinements
    min: SVal | None = None
    max: SVal | None = None
    exclusive_min: SVal | None = None
    exclusive_max: SVal | None = None
    min_length: int | None = None
    max_length: int | None = None
    non_empty: bool = False
    regex: str = ""
    has_regex: bool = False
    datetime_kind: str = ""
    # record / discriminated
    fields: list[Field] = dfield(default_factory=list)
    # map
    key_pattern: str = ""
    order: str = ""
    value: "Type | None" = None
    # array
    min_len: int | None = None
    max_len: int | None = None
    item: "Type | None" = None
    # tuple
    elements: list[str] = dfield(default_factory=list)
    # enum
    enum_values: list[SVal] = dfield(default_factory=list)
    sourced: bool = False
    baked: list[str] = dfield(default_factory=list)
    source_doc: str = ""
    source_sel: str = ""
    # literal
    literal: SVal = dfield(default_factory=SVal)
    # union
    discriminator: str = ""
    arms: list[Arm] = dfield(default_factory=list)
    # nullable
    inner: "Type | None" = None
    # opaque
    consumer_check: str = ""
    has_consumer_check: bool = False
    unchecked: bool = False
    has_unchecked: bool = False
    unchecked_reason: str = ""
    has_reason: bool = False
    # constraints
    constraints: list[Constraint] = dfield(default_factory=list)
    # location within the schema document
    schema_path: diag.Path = dfield(default_factory=diag.new_path)


@dataclass
class Import:
    file: str = ""
    types: list[str] = dfield(default_factory=list)


@dataclass
class Scalar:
    name: str = ""
    base: str = ""
    lexeme_rule: str = ""
    len_min: int | None = None
    len_max: int | None = None
    non_empty: bool = False


@dataclass
class Schema:
    name: str = ""
    meta_version: int = 0
    has_meta_version: bool = False
    meta_version_kind: doc.Kind = doc.Kind.NULL
    format_version: int = 0
    has_format_version: bool = False
    format_version_kind: doc.Kind = doc.Kind.NULL
    document_syntax: str = ""
    role: str = ""
    description: str = ""
    root: str = ""
    targets: list[str] = dfield(default_factory=list)
    safe_integers: bool = False
    imports: list[Import] = dfield(default_factory=list)
    types: dict[str, Type] = dfield(default_factory=dict)
    type_order: list[str] = dfield(default_factory=list)
    dir: str = ""

    def lookup_type(self, name: str) -> Type | None:
        return self.types.get(name)


_COMPLEX_KINDS = {
    "record": SKind.RECORD,
    "map": SKind.MAP,
    "array": SKind.ARRAY,
    "tuple": SKind.TUPLE,
    "enum": SKind.ENUM,
    "literal": SKind.LITERAL,
    "discriminated-union": SKind.DISCRIMINATED_UNION,
    "node-kind-union": SKind.NODE_KIND_UNION,
    "nullable": SKind.NULLABLE,
    "opaque": SKind.OPAQUE,
}


class _Reader:
    def __init__(self) -> None:
        self.diags = diag.Diagnostics()
        self.is_type = False
        self.file_name = ""


def read_schema(root: doc.Node | None, directory: str) -> tuple[Schema, list[diag.Diagnostic]]:
    s = Schema(dir=directory)
    r = _Reader()
    if root is None or root.kind != doc.Kind.RECORD:
        return s, r.diags.all()
    _parse_header(s, root)
    r.is_type = s.role == "type-definitions"
    r.file_name = s.name + ".toml"

    if not s.has_meta_version:
        r.diags.emit_code(
            "STRICTSPEC_SCHEMA_MISSING_META_VERSION",
            diag.new_path(),
            {"schema": diag.SlotIdentifier(s.name)},
        )
    if s.role == "schema" and not s.has_format_version:
        r.diags.emit_code(
            "STRICTSPEC_SCHEMA_MISSING_FORMAT_VERSION",
            diag.new_path(),
            {"schema": diag.SlotIdentifier(s.name)},
        )
    if r.is_type and len(s.imports) > 0:
        r.diags.emit_code(
            "STRICTSPEC_IMPORT_TRANSITIVE",
            diag.new_path(),
            {"file": diag.SlotString(r.file_name)},
        )

    types_node = _entry_of(root, "types")
    if types_node is not None and types_node.kind == doc.Kind.RECORD:
        for e in types_node.entries:
            name = e.key
            sp = diag.new_path(diag.Key("types"), diag.Key(name))
            t = _parse_type(r, e.value, sp)
            s.types[name] = t
            s.type_order.append(name)
            if r.is_type and _has_any_constraint(t):
                r.diags.emit_code(
                    "STRICTSPEC_IMPORT_CROSS_FILE_CONSTRAINT",
                    diag.new_path(),
                    {"file": diag.SlotString(r.file_name)},
                )
                r.is_type = False  # emit once
    return s, r.diags.all()


def _parse_header(s: Schema, root: doc.Node) -> None:
    for e in root.entries:
        k = e.key
        v = e.value
        if k == "name":
            s.name = _decode_str(v)
        elif k == "meta_version":
            s.has_meta_version = True
            s.meta_version_kind = v.kind
            iv = _int_of(v)
            if iv is not None:
                s.meta_version = iv
        elif k == "format_version":
            s.has_format_version = True
            s.format_version_kind = v.kind
            iv = _int_of(v)
            if iv is not None:
                s.format_version = iv
        elif k == "document_syntax":
            s.document_syntax = _decode_str(v)
        elif k == "role":
            s.role = _decode_str(v)
        elif k == "description":
            s.description = _decode_str(v)
        elif k == "root":
            s.root = _decode_str(v)
        elif k == "safe_integers":
            s.safe_integers = v.kind == doc.Kind.BOOL and v.lexeme == "true"
        elif k == "targets":
            for it in _items(v):
                s.targets.append(_decode_str(it))
        elif k == "imports":
            for it in _items(v):
                imp = Import(file=_decode_str(_child(it, "file")))
                for t in _items(_child(it, "types")):
                    imp.types.append(_decode_str(t))
                s.imports.append(imp)


def _parse_type(r: _Reader, node: doc.Node | None, sp: diag.Path) -> Type:
    t = Type(schema_path=sp)
    if node is None or node.kind != doc.Kind.RECORD:
        return t
    type_name, has_type = _str_entry(node, "type")

    if has_type:
        ck = _COMPLEX_KINDS.get(type_name)
        if ck is not None:
            t.kind = ck
        else:
            t.kind = SKind.REF
            t.ref = type_name
    else:
        t.kind = _infer_kind(node)

    _parse_refinements(t, node)

    if t.kind == SKind.RECORD:
        _parse_fields(r, t, node, sp)
    elif t.kind == SKind.MAP:
        kp, ok = _str_entry(node, "key_pattern")
        if ok:
            t.key_pattern = kp
        o, ok = _str_entry(node, "order")
        if ok:
            t.order = o
        v = _entry_of(node, "value")
        if v is not None:
            t.value = _parse_type(r, v, _append_key(sp, "value"))
    elif t.kind == SKind.ARRAY:
        v = _entry_of(node, "min_len")
        if v is not None:
            t.min_len = _int_ptr(v)
        v = _entry_of(node, "max_len")
        if v is not None:
            t.max_len = _int_ptr(v)
        it = _entry_of(node, "item")
        if it is not None:
            t.item = _parse_type(r, it, _append_key(sp, "item"))
    elif t.kind == SKind.TUPLE:
        for el in _items(_child(node, "elements")):
            t.elements.append(_decode_str(el))
    elif t.kind == SKind.ENUM:
        vs = _entry_of(node, "values")
        if vs is not None:
            for v in _items(vs):
                t.enum_values.append(sval_from_node(v))
        src_node = None
        src = _entry_of(node, "source")
        if src is not None and src.kind == doc.Kind.RECORD:
            t.sourced = True
            src_node = src
            t.source_doc = _str_or(src, "document")
            t.source_sel = _str_or(src, "selector")
        b = _entry_of(node, "baked")
        if b is not None:
            t.sourced = True
            for v in _items(b):
                t.baked.append(_decode_str(v))
        elif src_node is not None:
            b2 = _entry_of(src_node, "baked")
            if b2 is not None:
                for v in _items(b2):
                    t.baked.append(_decode_str(v))
    elif t.kind == SKind.LITERAL:
        v = _entry_of(node, "value")
        if v is not None:
            t.literal = sval_from_node(v)
    elif t.kind in (SKind.DISCRIMINATED_UNION, SKind.NODE_KIND_UNION):
        d, ok = _str_entry(node, "discriminator")
        if ok:
            t.discriminator = d
        arms = _entry_of(node, "arms")
        if arms is not None and arms.kind == doc.Kind.RECORD:
            for e in arms.entries:
                arm_sp = _append_key(_append_key(sp, "arms"), e.key)
                t.arms.append(Arm(name=e.key, type=_parse_type(r, e.value, arm_sp)))
    elif t.kind == SKind.NULLABLE:
        inn = _entry_of(node, "inner")
        if inn is not None:
            t.inner = _parse_type(r, inn, _append_key(sp, "inner"))
    elif t.kind == SKind.OPAQUE:
        cc, ok = _str_entry(node, "consumer_check")
        if ok:
            t.consumer_check = cc
            t.has_consumer_check = True
        u = _entry_of(node, "unchecked")
        if u is not None:
            t.has_unchecked = True
            t.unchecked = u.kind == doc.Kind.BOOL and u.lexeme == "true"
        ur, ok = _str_entry(node, "unchecked_reason")
        if ok:
            t.unchecked_reason = ur
            t.has_reason = True
        _check_opaque_stance(r, t)

    _parse_constraints(t, node)
    return t


def _check_opaque_stance(r: _Reader, t: Type) -> None:
    if t.has_consumer_check:
        return
    if t.has_unchecked and t.unchecked:
        if not t.has_reason:
            r.diags.emit_code("STRICTSPEC_SCHEMA_UNCHECKED_NO_REASON", t.schema_path, None)
        return
    r.diags.emit_code("STRICTSPEC_SCHEMA_OPAQUE_NO_STANCE", t.schema_path, None)


def _parse_fields(r: _Reader, t: Type, node: doc.Node, sp: diag.Path) -> None:
    fnode = _entry_of(node, "fields")
    if fnode is None or fnode.kind != doc.Kind.RECORD:
        return
    for e in fnode.entries:
        fsp = _append_key(_append_key(sp, "fields"), e.key)
        ft = _parse_type(r, e.value, fsp)
        f = Field(name=e.key, type=ft)
        req = _entry_of(e.value, "required")
        if req is not None:
            f.required = req.kind == doc.Kind.BOOL and req.lexeme == "true"
        for a in _items(_child(e.value, "aliases")):
            f.aliases.append(_decode_str(a))
        t.fields.append(f)


def _parse_refinements(t: Type, node: doc.Node) -> None:
    for attr, key in (
        ("min", "min"),
        ("max", "max"),
        ("exclusive_min", "exclusive_min"),
        ("exclusive_max", "exclusive_max"),
    ):
        v = _entry_of(node, key)
        if v is not None:
            setattr(t, attr, sval_from_node(v))
    v = _entry_of(node, "min_length")
    if v is not None:
        t.min_length = _int_ptr(v)
    v = _entry_of(node, "max_length")
    if v is not None:
        t.max_length = _int_ptr(v)
    v = _entry_of(node, "non_empty")
    if v is not None:
        t.non_empty = v.kind == doc.Kind.BOOL and v.lexeme == "true"
    rg, ok = _str_entry(node, "regex")
    if ok:
        t.regex = rg
        t.has_regex = True
    dk, ok = _str_entry(node, "datetime_kind")
    if ok:
        t.datetime_kind = dk


def _parse_constraints(t: Type, node: doc.Node) -> None:
    cnode = _entry_of(node, "constraints")
    if cnode is None:
        return
    for c in _items(cnode):
        if c.kind != doc.Kind.RECORD:
            continue
        con = Constraint(form=_str_or(c, "form"))
        con.field = _str_or(c, "field")
        con.left = _str_or(c, "left")
        con.right = _str_or(c, "right")
        con.collection = _str_or(c, "collection")
        con.uniq_field = _str_or(c, "field")
        con.normalization = _str_or(c, "normalization")
        con.start = _str_or(c, "start")
        con.length = _str_or(c, "length")
        con.less = _str_or(c, "less")
        con.than = _str_or(c, "than")
        con.reference = _str_or(c, "reference")
        con.resolves_into = _str_or(c, "resolves_into")
        con.resolves_by = _str_or(c, "resolves_by")
        con.source = _str_or(c, "source")
        con.selection = _str_or(c, "selection")
        con.compare = _str_or(c, "compare")
        con.sum_field = _str_or(c, "sum_field")
        lim = _entry_of(c, "limit")
        if lim is not None:
            con.limit = sval_from_node(lim)
            con.has_limit = True
        el = _entry_of(c, "equals_literal")
        if el is not None:
            con.equals_literal = sval_from_node(el)
            con.has_equals = True
        for f in _items(_child(c, "fields")):
            con.fields.append(_decode_str(f))
        w = _entry_of(c, "when")
        if w is not None and w.kind == doc.Kind.RECORD:
            con.when = _parse_condition(w)
        t.constraints.append(con)


def _parse_condition(w: doc.Node) -> Condition:
    c = Condition(field=_str_or(w, "field"), predicate=_str_or(w, "predicate"))
    v = _entry_of(w, "value")
    if v is not None:
        c.value = sval_from_node(v)
        c.has_value = True
    for v in _items(_child(w, "values")):
        c.values.append(sval_from_node(v))
    return c


def _infer_kind(node: doc.Node) -> SKind:
    if _entry_of(node, "fields") is not None:
        return SKind.RECORD
    if _entry_of(node, "arms") is not None:
        return SKind.DISCRIMINATED_UNION
    if _entry_of(node, "item") is not None:
        return SKind.ARRAY
    if _entry_of(node, "value") is not None:
        return SKind.MAP
    if _entry_of(node, "inner") is not None:
        return SKind.NULLABLE
    if _entry_of(node, "elements") is not None:
        return SKind.TUPLE
    return SKind.RECORD


def _has_any_constraint(t: Type | None) -> bool:
    if t is None:
        return False
    if len(t.constraints) > 0:
        return True
    for f in t.fields:
        if _has_any_constraint(f.type):
            return True
    for a in t.arms:
        if _has_any_constraint(a.type):
            return True
    if _has_any_constraint(t.item) or _has_any_constraint(t.value) or _has_any_constraint(t.inner):
        return True
    return False


# --- small node accessors ----------------------------------------------------


def _entry_of(rec: doc.Node | None, key: str) -> doc.Node | None:
    if rec is None or rec.kind != doc.Kind.RECORD:
        return None
    for e in rec.entries:
        if e.key == key:
            return e.value
    return None


def _child(rec: doc.Node | None, key: str) -> doc.Node | None:
    return _entry_of(rec, key)


def _str_entry(rec: doc.Node | None, key: str) -> tuple[str, bool]:
    n = _entry_of(rec, key)
    if n is None or n.kind != doc.Kind.STRING:
        return "", False
    return strdecode.decode_toml(n.lexeme), True


def _str_or(rec: doc.Node | None, key: str) -> str:
    s, _ = _str_entry(rec, key)
    return s


def _decode_str(n: doc.Node | None) -> str:
    if n is None or n.kind != doc.Kind.STRING:
        return ""
    return strdecode.decode_toml(n.lexeme)


def _int_of(n: doc.Node | None) -> int | None:
    if n is None or n.kind != doc.Kind.INTEGER:
        return None
    return _go_parse_int(n.lexeme)


def _int_ptr(n: doc.Node | None) -> int | None:
    return _int_of(n)


def _items(n: doc.Node | None) -> list[doc.Node]:
    if n is None or n.kind != doc.Kind.ARRAY:
        return []
    return list(n.items)


def _append_key(p: diag.Path, key: str) -> diag.Path:
    return diag.Path(steps=p.steps + (diag.Key(key),), anchor=p.anchor)


# --- numeric parsing mirroring Go strconv (base-10 ints; underscore floats) --


def _go_parse_int(lexeme: str) -> int | None:
    """Mirror Go strconv.ParseInt(s, 10, 64): base-10, no underscores, int64
    range. Returns None on failure (matching Go's err path).
    """
    s = lexeme
    if s == "":
        return None
    neg = False
    if s[0] in "+-":
        neg = s[0] == "-"
        s = s[1:]
    if s == "" or not s.isdigit() or not s.isascii():
        return None
    v = int(s)
    if neg:
        v = -v
    if v < -(2**63) or v > 2**63 - 1:
        return None
    return v


def _go_parse_float(lexeme: str) -> float | None:
    try:
        return float(lexeme)
    except (ValueError, OverflowError):
        return None


# --- loading (in-memory FileSet + on-disk) -----------------------------------

FileSet = dict


def parse_from(files: dict[str, str], name: str) -> tuple[Schema, list[diag.Diagnostic]]:
    src = files.get(name)
    if src is None:
        raise ValueError(f"strictspec: embedded file {name!r} not found")
    d = tomldoc.parse(src.encode("utf-8"))
    s, diags = read_schema(d.root, "")
    return s, diags


def resolve_imports_from(s: Schema, files: dict[str, str]) -> list[diag.Diagnostic]:
    out: list[diag.Diagnostic] = []
    for imp in s.imports:
        try:
            ts, tdiags = parse_from(files, imp.file)
        except ValueError:
            out.append(
                diag.Diagnostic(
                    "STRICTSPEC_IMPORT_MISSING_TYPE_FILE",
                    diag.new_path(),
                    {"file": diag.SlotString(imp.file), "schema": diag.SlotIdentifier(s.name)},
                )
            )
            continue
        out.extend(tdiags)
        for name in imp.types:
            if name not in ts.types:
                out.append(
                    diag.Diagnostic(
                        "STRICTSPEC_IMPORT_UNKNOWN_TYPE",
                        diag.new_path(),
                        {"name": diag.SlotIdentifier(name), "file": diag.SlotString(imp.file)},
                    )
                )
        for name, t in ts.types.items():
            if name not in s.types:
                s.types[name] = t
    return out


def load_manifest_scalars_from(files: dict[str, str]) -> dict[str, Scalar]:
    out: dict[str, Scalar] = {}
    for src in files.values():
        try:
            d = tomldoc.parse(src.encode("utf-8"))
        except doc.ParseError:
            continue
        _merge_scalars(out, d.root)
    return out


def load_file(path: str) -> tuple[Schema, list[diag.Diagnostic]]:
    with open(path, "rb") as f:
        src = f.read()
    d = tomldoc.parse(src)
    s, diags = read_schema(d.root, os.path.dirname(path))
    return s, diags


def resolve_imports(s: Schema) -> list[diag.Diagnostic]:
    out: list[diag.Diagnostic] = []
    for imp in s.imports:
        path = os.path.join(s.dir, imp.file)
        try:
            ts, tdiags = load_file(path)
        except (OSError, doc.ParseError):
            out.append(
                diag.Diagnostic(
                    "STRICTSPEC_IMPORT_MISSING_TYPE_FILE",
                    diag.new_path(),
                    {"file": diag.SlotString(imp.file), "schema": diag.SlotIdentifier(s.name)},
                )
            )
            continue
        out.extend(tdiags)
        for name in imp.types:
            if name not in ts.types:
                out.append(
                    diag.Diagnostic(
                        "STRICTSPEC_IMPORT_UNKNOWN_TYPE",
                        diag.new_path(),
                        {"name": diag.SlotIdentifier(name), "file": diag.SlotString(imp.file)},
                    )
                )
        for name, t in ts.types.items():
            if name not in s.types:
                s.types[name] = t
    return out


def load_manifest_scalars(directory: str) -> dict[str, Scalar]:
    out: dict[str, Scalar] = {}
    try:
        names = os.listdir(directory)
    except OSError:
        return out
    for name in names:
        full = os.path.join(directory, name)
        if os.path.isdir(full) or not name.endswith(".toml"):
            continue
        try:
            with open(full, "rb") as f:
                src = f.read()
            d = tomldoc.parse(src)
        except (OSError, doc.ParseError):
            continue
        _merge_scalars(out, d.root)
    return out


def _merge_scalars(out: dict[str, Scalar], root: doc.Node) -> None:
    sc = _entry_of(root, "scalars")
    if sc is None:
        return
    for s in _items(sc):
        cs = Scalar(
            name=_str_or(s, "name"),
            base=_str_or(s, "base"),
            lexeme_rule=_str_or(s, "lexeme_rule"),
        )
        length = _entry_of(s, "length")
        if length is not None:
            mn = _entry_of(length, "min")
            if mn is not None:
                cs.len_min = _int_ptr(mn)
            mx = _entry_of(length, "max")
            if mx is not None:
                cs.len_max = _int_ptr(mx)
            ne = _entry_of(length, "non_empty")
            if ne is not None:
                cs.non_empty = ne.lexeme == "true"
        if cs.name != "":
            out[cs.name] = cs
