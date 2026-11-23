#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
# Baut die Release-Archive aller Zielplattformen nach dist/ und schreibt
# SHA256SUMS. Aufruf im Repository: scripts/build-release.sh v0.4.0
# Läuft im Release-Workflow (.github/workflows/release.yml), geht aber auch
# lokal. Die Archive sind reproduzierbar: gleicher Commit, gleiche Go-Version,
# gleiche Bytes.
set -euo pipefail

version=${1:?Version fehlt, z. B. v0.4.0}
[[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "ungültige Version: $version" >&2; exit 1; }
cd "$(git rev-parse --show-toplevel)"
out=dist
rm -rf "$out"
mkdir -p "$out"

# Zeitstempel im Archiv: der des Commits, nicht der des Bauens.
epoch=$(git log -1 --format=%ct)
export SOURCE_DATE_EPOCH=$epoch

targets=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)
for t in "${targets[@]}"; do
	os=${t%/*} arch=${t#*/}
	name="swarmainet_${version#v}_${os}_${arch}"
	dir="$out/$name"
	mkdir -p "$dir"
	bin=swarmainet
	[[ $os == windows ]] && bin=swarmainet.exe
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$version" \
		-o "$dir/$bin" ./cmd/swarmainet
	cp LICENSE LICENSE-DOCS README.md CHANGELOG.md config.example.yaml "$dir/"
	chmod 755 "$dir" "$dir/$bin"
	chmod 644 "$dir"/*.md "$dir"/LICENSE* "$dir"/*.yaml
	touch -d "@$epoch" "$dir" "$dir"/*
	if [[ $os == windows ]]; then
		(cd "$out" && find "$name" | LC_ALL=C sort | TZ=UTC zip -qX -@ "$name.zip")
	else
		tar -C "$out" --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner \
			-cf - "$name" | gzip -9n > "$out/$name.tar.gz"
	fi
	rm -rf "$dir"
	echo "gebaut: $name"
done
(cd "$out" && sha256sum -- *.tar.gz *.zip > SHA256SUMS)
echo "fertig: $out/"
