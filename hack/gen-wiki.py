#!/usr/bin/env python3
"""Builds GitHub wiki pages from README.md and docs/. Usage: hack/gen-wiki.py <output-dir>

The wiki is a separate git repository. This script only writes files. hack/publish-wiki.sh pushes them.
Links between pages are rewritten from repository paths to wiki page names. Unknown links fail the build.
"""
import pathlib
import re
import shutil
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent

# source file (relative to the repository) -> wiki page name
PAGES = {
    "README.md": "Home",
    "docs/install.md": "Installation",
    "docs/first-app.md": "Your-First-App",
    "docs/service-account-tokens.md": "Service-Account-Tokens",
    "docs/cert-manager.md": "cert-manager-CA",
    "docs/full-control.md": "Full-Control",
    "docs/routers.md": "Edge-Routers",
    "docs/role-scope.md": "Role-Scope",
    "docs/existing-resources.md": "Existing-Resources",
    "docs/audit.md": "Audit",
    "docs/operations.md": "Operations",
    "docs/uninstall.md": "Uninstall",
    "docs/conditions.md": "Conditions-and-Reasons",
    "docs/compatibility.md": "Compatibility",
    "docs/interop-ziti-k8s-agent.md": "Interop-with-ziti-k8s-agent",
    "docs/adr/0001-build-or-contribute.md": "ADR-0001-Build-or-contribute",
    "docs/adr/0002-service-account-tokens.md": "ADR-0002-Service-account-tokens",
    "CONTRIBUTING.md": "Contributing",
    "SECURITY.md": "Security",
}
for kind in ("ZitiConnection", "ZitiApp", "ZitiIdentity", "ZitiAccessPolicy", "ZitiJwtSigner", "ZitiCA",
             "ZitiRouter", "ZitiConfig", "ZitiService", "ZitiServicePolicy", "ZitiEdgeRouterPolicy", "ZitiServiceEdgeRouterPolicy", "ZitiTerminator"):
    PAGES[f"docs/reference/{kind}.md"] = kind

LINK = re.compile(r"\]\(((?:\.\./|\./)*(?:docs/|reference/|adr/)*)([A-Za-z0-9_.-]+\.md)(#[^)]*)?\)")

SIDEBAR = """**[Home](Home)**

**Get started**
- [Installation](Installation)
- [Your first app](Your-First-App)
- [Role scope](Role-Scope)
- [Existing Ziti resources](Existing-Resources)
- [Service account tokens](Service-Account-Tokens)
- [cert-manager CA](cert-manager-CA)
- [Full control (one resource per Ziti object)](Full-Control)
- [Edge routers](Edge-Routers)

**Resource reference**
- [ZitiConnection](ZitiConnection)
- [ZitiApp](ZitiApp)
- [ZitiIdentity](ZitiIdentity)
- [ZitiAccessPolicy](ZitiAccessPolicy)
- [ZitiJwtSigner](ZitiJwtSigner)
- [ZitiCA](ZitiCA)
- [ZitiRouter](ZitiRouter)
- [ZitiConfig](ZitiConfig)
- [ZitiService](ZitiService)
- [ZitiServicePolicy](ZitiServicePolicy)
- [ZitiEdgeRouterPolicy](ZitiEdgeRouterPolicy)
- [ZitiServiceEdgeRouterPolicy](ZitiServiceEdgeRouterPolicy)
- [ZitiTerminator](ZitiTerminator)

**Run it**
- [Conditions and reasons](Conditions-and-Reasons)
- [Operations](Operations)
- [Audit a network](Audit)
- [Uninstall](Uninstall)
- [Compatibility](Compatibility)

**More**
- [Interop with ziti-k8s-agent](Interop-with-ziti-k8s-agent)
- [Design decisions](ADR-0001-Build-or-contribute)
- [Contributing](Contributing)
- [Security](Security)
"""


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    out = pathlib.Path(sys.argv[1])
    if out.exists():
        for old in out.glob("*.md"):
            old.unlink()
    out.mkdir(parents=True, exist_ok=True)

    by_name = {pathlib.PurePosixPath(src).name: page for src, page in PAGES.items()}
    problems = []

    def rewrite(text, src):
        def sub(m):
            name = m.group(2)
            page = by_name.get(name)
            if page is None:
                problems.append(f"{src}: link to {m.group(0)} has no wiki page")
                return m.group(0)
            return f"]({page}{m.group(3) or ''})"
        return LINK.sub(sub, text)

    for src, page in PAGES.items():
        path = ROOT / src
        if not path.exists():
            problems.append(f"{src}: missing")
            continue
        text = path.read_text()
        text = re.sub(r"<!--.*?-->\n*", "", text, flags=re.S)
        (out / f"{page}.md").write_text(rewrite(text, src))

    (out / "_Sidebar.md").write_text(SIDEBAR)
    (out / "_Footer.md").write_text("Apache-2.0. Generated from the repository docs. Edit the docs, not the wiki.\n")

    names = {p.stem for p in out.glob("*.md")}
    for page in sorted(out.glob("*.md")):
        for target in re.findall(r"\]\(([A-Za-z0-9_-]+)(?:#[^)]*)?\)", page.read_text()):
            if target not in names:
                problems.append(f"{page.name}: link to page {target} does not exist")
    if problems:
        print("\n".join(sorted(set(problems))), file=sys.stderr)
        sys.exit(1)
    print(f"wrote {len(names)} pages to {out}")


main()
