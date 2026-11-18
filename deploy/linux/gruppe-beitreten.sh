#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
# Richtet auf einem Linux-Rechner einen Knoten für ein Gruppennetz ein und startet
# ihn (spec/membership.md). Ausführen im geklonten Repository, ohne root:
#
#   deploy/linux/gruppe-beitreten.sh           installieren, beitreten, starten
#   deploy/linux/gruppe-beitreten.sh status    Verbindungen anzeigen
#   deploy/linux/gruppe-beitreten.sh test      Probeauftrag über das Gruppennetz
#   deploy/linux/gruppe-beitreten.sh stop      Knoten beenden
#   deploy/linux/gruppe-beitreten.sh entfernen Knoten und Testverzeichnis löschen
#
# Go wird bei Bedarf nach ~/.local/go geladen und geprüft. Der Knoten läuft getrennt
# von einem vorhandenen SwarmAiNet-Knoten, mit eigenem Verzeichnis, Port und Gateway.
# Den Einladungslink fragt das Skript ab, damit er nicht in der Shell-Historie landet.
set -euo pipefail

DIR="${GRUPPE_DIR:-$HOME/gruppe-test}"
PORT="${GRUPPE_PORT:-4714}"
GATEWAY="${GRUPPE_GATEWAY:-127.0.0.1:7715}"
UNIT="${GRUPPE_UNIT:-swarmainet-gruppe}"
GOROOT_DIR="${GRUPPE_GOROOT:-$HOME/.local/go}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SW="$DIR/swarmainet"
CFG="$DIR/config.yaml"

schritt() { printf '\n== %s\n' "$*"; }
fehler() {
	printf 'Fehler: %s\n' "$*" >&2
	exit 1
}

status() {
	"$SW" status -config "$CFG" | python3 -c '
import json, sys
s = json.load(sys.stdin)
n = s["networks"][0]
m = n.get("membership") or {}
print("Netz:      ", n["network_id"])
print("Peer-ID:   ", s["peer_id"])
print("Rolle:     ", m.get("role", "-"), "| gültig bis", str(m.get("expires_at", "-"))[:10])
if m.get("revoked"):
    print("ACHTUNG:    Dieser Knoten wurde aus dem Netz entfernt.")
print("Relay:     ", "ja" if n["relay_reservations"] else "noch keins")
print("Verbunden: ")
for p in s["peers"]:
    agents = [a["agent_id"] for a in p.get("agents") or []]
    print("  ", p["peer_id"][-6:], p.get("role") or "(nicht zugelassen)", p["via"], *agents)
if not s["peers"]:
    print("   niemand")
for h in n.get("hints") or []:
    print("Hinweis:   ", h)
'
}

probeauftrag() {
	curl -s -m 180 "http://$GATEWAY/v1/chat/completions" -H 'Content-Type: application/json' \
		-d '{"model":"swarmainet/single","messages":[{"role":"user","content":"Sag nur: ok"}]}' | python3 -c '
import json, sys
d = json.load(sys.stdin)
if "error" in d:
    sys.exit("Fehler: " + d["error"]["message"])
print("Antwort:", d["choices"][0]["message"]["content"])
for x in d.get("swarmainet", {}).get("steps", []):
    print("Agent:  ", x.get("agent_id"), "auf Peer", x.get("peer_id", "")[-6:], "-", x.get("duration_ms"), "ms")
'
}

laeuft() { systemctl --user is-active --quiet "$UNIT"; }

case "${1:-}" in
"") ;;
status)
	status
	exit 0
	;;
test)
	probeauftrag
	exit 0
	;;
stop)
	systemctl --user stop "$UNIT" 2>/dev/null || true
	echo "Knoten beendet."
	exit 0
	;;
entfernen)
	systemctl --user stop "$UNIT" 2>/dev/null || true
	rm -rf "$DIR"
	echo "Knoten beendet und $DIR gelöscht. Go bleibt in $GOROOT_DIR."
	exit 0
	;;
*)
	fehler "unbekannter Befehl: $1 (erlaubt: status, test, stop, entfernen)"
	;;
esac

[ "$(id -u)" -ne 0 ] || fehler "bitte ohne root ausführen; sudo wird nur bei Bedarf für curl abgefragt"
[ -f "$REPO/go.mod" ] || fehler "$REPO ist kein SwarmAiNet-Repository"
command -v python3 >/dev/null || fehler "python3 fehlt: sudo apt install python3"

schritt "Voraussetzungen"
if ! command -v curl >/dev/null; then
	echo "curl fehlt und wird installiert (fragt nach dem Kennwort)."
	sudo apt-get update -qq
	sudo apt-get install -y -qq curl
fi
case "$(uname -m)" in
x86_64) ARCH=amd64 ;;
aarch64 | arm64) ARCH=arm64 ;;
*) fehler "Prozessor $(uname -m) wird nicht unterstützt" ;;
esac
if command -v ip >/dev/null && [ -n "$(ip link show type wireguard 2>/dev/null)" ]; then
	echo "Hinweis: Eine WireGuard-Schnittstelle ist aktiv. Für den Test eines zweiten"
	echo "Standorts sollte sie aus sein, sonst läuft der Verkehr evtl. durch den Tunnel."
fi
echo "ok ($ARCH)"

schritt "Go"
GOVER="$(sed -n 's/^go \([0-9.]*\).*/\1/p' "$REPO/go.mod")"
[ -n "$GOVER" ] || fehler "keine Go-Version in go.mod gefunden"
if [ "$("$GOROOT_DIR/bin/go" env GOVERSION 2>/dev/null || true)" = "go$GOVER" ]; then
	echo "Go $GOVER ist schon installiert: $GOROOT_DIR"
else
	FILE="go$GOVER.linux-$ARCH.tar.gz"
	SUM="$(curl -fsS -m 60 'https://go.dev/dl/?mode=json&include=all' | python3 -c '
import json, sys
name = sys.argv[1]
for r in json.load(sys.stdin):
    for f in r["files"]:
        if f["filename"] == name:
            print(f["sha256"])
            sys.exit(0)
' "$FILE")"
	[ -n "$SUM" ] || fehler "Prüfsumme für $FILE nicht gefunden"
	TMP="$(mktemp -d)"
	trap 'rm -rf "$TMP"' EXIT
	echo "Lade $FILE ..."
	curl -fL# -m 900 -o "$TMP/$FILE" "https://go.dev/dl/$FILE"
	echo "$SUM  $TMP/$FILE" | sha256sum -c --quiet || fehler "Prüfsumme von $FILE stimmt nicht"
	tar -C "$TMP" -xzf "$TMP/$FILE"
	mkdir -p "$(dirname "$GOROOT_DIR")"
	rm -rf "$GOROOT_DIR"
	mv "$TMP/go" "$GOROOT_DIR"
	echo "Go $GOVER installiert: $GOROOT_DIR"
fi

schritt "Programm bauen"
mkdir -p "$DIR"
chmod 700 "$DIR"
(cd "$REPO" && CGO_ENABLED=0 "$GOROOT_DIR/bin/go" build -o "$SW.neu" ./cmd/swarmainet)
laeuft && systemctl --user stop "$UNIT"
mv "$SW.neu" "$SW"
echo "ok: $SW"

schritt "Konfiguration"
if [ -f "$CFG" ]; then
	echo "vorhanden, bleibt erhalten: $CFG"
else
	cat >"$CFG" <<EOF
# Knoten für ein Gruppennetz, angelegt von deploy/linux/gruppe-beitreten.sh.
node:
  data_dir: $DIR
  listen: [/ip4/0.0.0.0/udp/$PORT/quic-v1, /ip4/0.0.0.0/tcp/$PORT]
  gateway: $GATEWAY
  mdns: false
  dns_seeds: [none]
backends: []
resources:
  energy: { mode: "off" }
EOF
	echo "angelegt: $CFG"
fi

schritt "Gruppennetz beitreten"
if grep -q '^ *membership:' "$CFG"; then
	echo "Dieser Knoten ist schon beigetreten."
else
	echo "Bitte den Einladungslink einfügen (swarmainet://join?...) und Enter drücken."
	echo "Er gilt einmal und höchstens 24 Stunden; der Aussteller muss gerade laufen."
	read -r -p "Link: " LINK
	[ -n "$LINK" ] || fehler "kein Link eingegeben"
	"$SW" join -config "$CFG" "$LINK"
	unset LINK
fi

schritt "Knoten starten"
systemctl --user reset-failed "$UNIT" 2>/dev/null || true
systemd-run --user --quiet --unit="$UNIT" --collect "$SW" run -config "$CFG"
for _ in $(seq 1 30); do
	"$SW" status -config "$CFG" >/dev/null 2>&1 && break
	sleep 1
done
laeuft || {
	journalctl --user -u "$UNIT" -n 15 --no-pager -o cat >&2 || true
	fehler "der Knoten ist nicht gestartet"
}
sleep 5 # Zeit für Zulassung und Relay
status

cat <<EOF

Der Knoten läuft, bis Sie ihn beenden oder den Rechner neu starten.
  $0 status      Verbindungen anzeigen
  $0 test        Probeauftrag über das Gruppennetz
  $0 stop        beenden, ein erneuter Aufruf ohne Befehl startet ihn wieder
  $0 entfernen   Knoten und $DIR löschen
  journalctl --user -u $UNIT -f   Protokoll mitlesen
EOF
