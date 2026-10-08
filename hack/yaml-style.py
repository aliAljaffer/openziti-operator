#!/usr/bin/env python3
"""Expands flow style to block style. Lists are listed out, objects get one key per line.

Usage: hack/yaml-style.py [--check] [path ...]
  no flag   rewrite the files in place; exits 1 when it changed something, so a commit hook stops
  --check   change nothing; exits 1 when it finds flow style, for CI

Paths default to the tracked yaml files, the tracked markdown files, and the yaml code blocks in them.
Empty collections stay as they are: YAML has no block form for them. Generated files are skipped.
Run with --self-test to check the expander.
"""
import pathlib
import re
import subprocess
import sys

GENERATED = ("config/crd/", "charts/chart/templates/crd/", "docs/reference/")
FLOW = re.compile(r"(:\s*\[)|(:\s*\{(?!\{))|(^\s*-\s*\[)|(^\s*-\s*\{(?!\{))")
COMMENT = re.compile(r"^(?P<mark>[ \t]*#+)(?P<gap>[ \t]*)(?P<rest>.*)$")
FENCE_OPEN = re.compile(r"^\s*```yaml\s*$")
FENCE = re.compile(r"^\s*```\s*$")
DASH_FLOW = re.compile(r"^-(?P<gap>[ \t]+)(?P<value>\{.*\}|\[.*\])\s*$")
DASH_KEY_FLOW = re.compile(r"^-(?P<gap>[ \t]+)(?P<key>[^:]+):\s*(?P<value>\{.*\}|\[.*\])\s*$")
KEY_FLOW = re.compile(r"^(?P<key>[^:]+):\s*(?P<value>\{.*\}|\[.*\])\s*$")


def split_top(body):
    """Splits a flow collection body on commas that are not inside quotes or a nested collection."""
    items, depth, quote, cur = [], 0, "", ""
    for ch in body:
        if quote:
            cur += ch
            if ch == quote:
                quote = ""
            continue
        if ch in "\"'":
            quote = ch
        elif ch in "[{":
            depth += 1
        elif ch in "]}":
            depth -= 1
        if ch == "," and depth == 0:
            items.append(cur.strip())
            cur = ""
            continue
        cur += ch
    if cur.strip():
        items.append(cur.strip())
    return items


def is_collection(text):
    text = text.strip()
    return len(text) > 1 and text[0] in "[{" and text[-1] in "]}" and text[1:-1].strip()


def pairs(body):
    """Splits a flow mapping body into (key, value) pairs."""
    out = []
    for item in split_top(body):
        key, sep, value = item.partition(":")
        if not sep:
            raise ValueError(f"flow mapping item without a colon: {item}")
        out.append((key.strip(), value.strip()))
    return out


def under(head, value, key_indent, aligned):
    """Returns the lines for one key. A collection value moves to its own lines.

    aligned says the key sits inside a sequence entry, so a list under it lines up with the key. That is
    the shape controller-gen writes for rbac rules.
    """
    if is_collection(value):
        child = key_indent if aligned and value.startswith("[") else key_indent + 2
        return [head + ":"] + render(value, child)
    return [f"{head}: {value}" if value else f"{head}: {{}}"]


def render(value, indent):
    """Returns the block lines for one flow collection, every line padded to indent spaces."""
    value = value.strip()
    if not is_collection(value):
        return None
    body, pad, lines = value[1:-1].strip(), " " * indent, []
    if value[0] == "[":
        for item in split_top(body):
            if item.startswith("{"):
                for i, (key, val) in enumerate(pairs(item[1:-1])):
                    lead = f"{pad}- " if i == 0 else f"{pad}  "
                    lines.extend(under(f"{lead}{key}", val, indent + 2, True))
            elif item.startswith("["):
                lines.append(f"{pad}-")
                lines.extend(render(item, indent + 2))
            else:
                lines.append(f"{pad}- {item}")
        return lines
    for key, val in pairs(body):
        lines.extend(under(f"{pad}{key}", val, indent, False))
    return lines


def expand_body(body, indent, aligned=False):
    """Returns the block lines for the text after the indent, or None when it has no flow style."""
    for pattern in (DASH_FLOW, DASH_KEY_FLOW, KEY_FLOW):
        m = pattern.match(body)
        if not m:
            continue
        value = m.group("value")
        if value.startswith("{{") or not is_collection(value):
            return None
        if pattern is DASH_FLOW:
            lines = render(value, 0)
            if lines is None:
                return None
            pad = " " * indent
            return [pad + "- " + lines[0].lstrip()] + [pad + "  " + l.lstrip() for l in lines[1:]]
        dashed = pattern is DASH_KEY_FLOW
        head = " " * indent + ("-" + m.group("gap") if dashed else "") + m.group("key")
        return under(head, value, indent + (2 if dashed else 0), dashed or aligned)
    return None


def expand_line(line, aligned=False):
    """Returns the block lines for one line, or None when it has no flow style."""
    comment = COMMENT.match(line)
    if comment:
        rest, gap = comment.group("rest"), comment.group("gap")
        if not rest.strip():
            return None
        lines = expand_body(rest.strip(), len(gap), aligned)
        return None if lines is None else [comment.group("mark") + l for l in lines]
    stripped = line.lstrip(" ")
    return expand_body(stripped, len(line) - len(stripped), aligned)


def expand_lines(lines):
    """Expands a run of yaml lines. Returns the new lines and whether it changed anything."""
    out, changed, entry = [], False, None
    for line in lines:
        stripped = line.lstrip(" ")
        indent = len(line) - len(stripped)
        if entry is not None and indent <= entry:
            entry = None
        aligned = False
        if stripped.startswith("-"):
            entry = indent
        elif entry is not None and indent == entry + 2:
            aligned = True
        block = expand_line(line, aligned)
        if block:
            out.extend(block)
            changed = True
        else:
            out.append(line)
    return out, changed


def expand_yaml(text):
    lines, changed = expand_lines(text.split("\n"))
    return "\n".join(lines), changed


def expand_markdown(text):
    """Expands flow style inside yaml code blocks only. Returns the new text and whether it changed."""
    out, changed, buf, inblock = [], False, [], False
    for line in text.split("\n"):
        if inblock and not FENCE.match(line):
            buf.append(line)
            continue
        if inblock:
            lines, ch = expand_lines(buf)
            out.extend(lines)
            changed, buf, inblock = changed or ch, [], False
        out.append(line)
        inblock = bool(FENCE_OPEN.match(line))
    if buf:
        lines, ch = expand_lines(buf)
        out.extend(lines)
        changed = changed or ch
    return "\n".join(out), changed


def expand(text, markdown):
    return expand_markdown(text) if markdown else expand_yaml(text)


def targets(paths):
    """Returns the files to work on, as (path, is_markdown) pairs."""
    if paths:
        found = []
        for arg in paths:
            path = pathlib.Path(arg)
            if path.is_dir():
                found.extend(targets([str(p) for p in sorted(path.rglob("*")) if p.is_file()]))
            elif path.is_file():
                found.append((path, path.suffix == ".md"))
        return found
    listed = subprocess.run(
        ["git", "ls-files", "*.yaml", "*.yml", "*.md"], capture_output=True, text=True, check=True
    )
    return [(pathlib.Path(l), l.endswith(".md")) for l in listed.stdout.split("\n") if l]


def self_test():
    cases = [
        ("spec:\n  hostingRouters: [edge-1]\n", "spec:\n  hostingRouters:\n    - edge-1\n"),
        ("metadata: {name: a, namespace: b}\n", "metadata:\n  name: a\n  namespace: b\n"),
        ("  - {name: https, port: 443}\n", "  - name: https\n    port: 443\n"),
        ("spec:\n  ports: [443, \"8000-8005\"]\n", "spec:\n  ports:\n    - 443\n    - \"8000-8005\"\n"),
        ("  a: {b: {c: 1}}\n", "  a:\n    b:\n      c: 1\n"),
        ("  a: {b: [x, y]}\n", "  a:\n    b:\n      - x\n      - y\n"),
        ("  a: {}\n", "  a: {}\n"),
        ("  a: []\n", "  a: []\n"),
        ("  a: {b: []}\n", "  a:\n    b: []\n"),
        ("  a: {b: [1, 2], c: {d: e}}\n", "  a:\n    b:\n      - 1\n      - 2\n    c:\n      d: e\n"),
        ("  apiGroups: [\"\"]\n", "  apiGroups:\n    - \"\"\n"),
        ("  image: {{ .Values.image }}\n", "  image: {{ .Values.image }}\n"),
        ("  portRanges: [{low: 80, high: 80}]\n", "  portRanges:\n    - low: 80\n      high: 80\n"),
        ("  - serviceAccountToken: {audience: ziti, path: token}\n",
         "  - serviceAccountToken:\n      audience: ziti\n      path: token\n"),
        ("  # secretRef: {namespace: a, name: b}\n", "  # secretRef:\n  #   namespace: a\n  #   name: b\n"),
        ("  #   - secret: {name: x, key: ca.crt}\n",
         "  #   - secret:\n  #       name: x\n  #       key: ca.crt\n"),
        ("  # plain comment\n", "  # plain comment\n"),
        # controller-gen writes rbac rules with the list aligned under the key
        ("rules:\n- apiGroups: [\"\"]\n  resources: [secrets]\n  verbs: [get, list]\n",
         "rules:\n- apiGroups:\n  - \"\"\n  resources:\n  - secrets\n  verbs:\n  - get\n  - list\n"),
        ("- apiGroups: [\"\"]\n  nested: [{a: 1}]\n", "- apiGroups:\n  - \"\"\n  nested:\n  - a: 1\n"),
        ("spec:\n  rules:\n  - apiGroups: [\"\"]\n    verbs: [get]\n",
         "spec:\n  rules:\n  - apiGroups:\n    - \"\"\n    verbs:\n    - get\n"),
    ]
    for src, want in cases:
        got, _ = expand_yaml(src)
        assert got == want, f"expand_yaml({src!r})\ngot  {got!r}\nwant {want!r}"
    md = "text\n```yaml\na: [b]\n```\nmore\n```yaml\n  c: [d]\n```\n"
    got, changed = expand_markdown(md)
    want = "text\n```yaml\na:\n  - b\n```\nmore\n```yaml\n  c:\n    - d\n```\n"
    assert got == want and changed, f"expand_markdown\ngot  {got!r}\nwant {want!r}"
    plain = "a: [b]\n"
    assert expand_markdown(plain) == (plain, False), "flow style outside a code block must be left alone"
    print(f"self test ok ({len(cases) + 2} cases)")


def main():
    args = sys.argv[1:]
    if args and args[0] == "--self-test":
        self_test()
        return 0
    check = "--check" in args
    paths = [a for a in args if a != "--check"]

    changed_files = 0
    for path, markdown in targets(paths):
        if any(part in str(path) for part in GENERATED):
            continue
        text, changed = expand(path.read_text(), markdown)
        if not changed:
            continue
        changed_files += 1
        if check:
            print(f"{path}", file=sys.stderr)
        else:
            path.write_text(text)
            print(f"expanded {path}")
    if changed_files:
        verb = "uses flow style" if check else "expanded"
        print(f"{changed_files} file(s) {verb}, run hack/yaml-style.py to fix them", file=sys.stderr)
        return 1
    return 0


sys.exit(main())
