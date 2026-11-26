#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later
# Schreibt den Text eines GitHub-Releases: den Abschnitt der Version aus
# CHANGELOG.md, relative Links auf die Dateien zum Tag umgeschrieben, dazu
# Hinweise zu den Downloads. Aufruf: scripts/release-notes.py v0.4.0 OWNER/REPO
import re
import sys

tag, repo = sys.argv[1], sys.argv[2]
version = tag.removeprefix("v")
base = f"https://github.com/{repo}/blob/{tag}/"

text = open("CHANGELOG.md", encoding="utf-8").read()
m = re.search(rf"^## \[{re.escape(version)}\][^\n]*\n(.*?)(?=^## \[|\Z)", text, re.S | re.M)
body = m.group(1).strip() if m else f"Changes: see [CHANGELOG.md]({base}CHANGELOG.md)."
# Relative Links (nicht http, mailto oder Anker) auf das Repository zum Tag.
body = re.sub(r"\]\((?!https?:|mailto:|#)([^)\s]+)\)", lambda l: f"]({base}{l.group(1)})", body)

print(body)
print(f"""
### Downloads

Archives for Linux, macOS and Windows (amd64 and arm64). Check them with `sha256sum -c SHA256SUMS --ignore-missing`. The binaries are not signed: on macOS, remove the quarantine flag with `xattr -d com.apple.quarantine swarmainet`; Windows may show a SmartScreen warning. Details: [installation]({base}docs/installation.md#prebuilt-binaries).""")
