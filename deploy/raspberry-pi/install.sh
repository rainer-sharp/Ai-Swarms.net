#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Installiert oder aktualisiert swarmainet als systemd-Dienst auf einem
# Raspberry Pi (deploy/raspberry-pi/README.md). Aufruf im Verzeichnis mit
# swarmainet-arm64, config.yaml und swarmainet.service:
#
#   sudo sh install.sh
#
# Eine vorhandene Konfiguration und vorhandene Schlüssel bleiben unverändert.
set -eu

DIR=/var/lib/swarmainet
BIN=${1:-./swarmainet-arm64}
HERE=$(dirname "$0")

if [ "$(id -u)" -ne 0 ]; then
	echo "Bitte mit sudo ausführen: sudo sh $0" >&2
	exit 1
fi
if [ ! -f "$BIN" ]; then
	echo "$BIN fehlt. Auf dem Entwicklungsrechner bauen (siehe README, Abschnitt 3) und hierher kopieren." >&2
	exit 1
fi
case "$(uname -m)" in
aarch64 | arm64) ;;
*) echo "Warnung: $(uname -m) ist kein 64-Bit-ARM. swarmainet-arm64 läuft nur auf 64-Bit-Raspberry-Pi-OS." >&2 ;;
esac

running=false
if [ -d /run/systemd/system ] && systemctl is-active --quiet swarmainet; then
	running=true
	systemctl stop swarmainet
fi

install -m 0755 "$BIN" /usr/local/bin/swarmainet
echo "Programm installiert: /usr/local/bin/swarmainet"

if ! id swarmainet >/dev/null 2>&1; then
	useradd --system --home-dir "$DIR" --shell /usr/sbin/nologin swarmainet
	echo "Benutzer swarmainet angelegt"
fi
install -d -m 0700 -o swarmainet -g swarmainet "$DIR"

if [ -e "$DIR/config.yaml" ]; then
	echo "Konfiguration vorhanden, nicht verändert: $DIR/config.yaml"
else
	install -m 0600 -o swarmainet -g swarmainet "$HERE/config.yaml" "$DIR/config.yaml"
	echo "Konfiguration angelegt: $DIR/config.yaml"
fi

if [ -d /run/systemd/system ]; then
	install -m 0644 "$HERE/swarmainet.service" /etc/systemd/system/swarmainet.service
	systemctl daemon-reload
	systemctl enable --quiet swarmainet
	echo "Dienst eingerichtet und für den Systemstart aktiviert"
	if $running; then
		systemctl start swarmainet
		echo "Dienst neu gestartet"
	fi
else
	echo "Kein systemd gefunden; Dienst nicht eingerichtet" >&2
fi

if ! $running; then
	cat <<EOT

Nächste Schritte (README, Abschnitt 3):
  1. Netz beitreten, mit einem Link aus "swarmainet invite -with-key" eines Mitglieds:
       sudo -u swarmainet swarmainet join -config $DIR/config.yaml '<link>'
  2. Öffentlichen Namen eintragen (announce):
       sudoedit $DIR/config.yaml
  3. Neu starten und prüfen (der Dienst startet nach jedem Hochfahren von
     selbst und liest Änderungen an der Konfiguration erst beim Neustart):
       sudo systemctl restart swarmainet
       sudo -u swarmainet swarmainet status -config $DIR/config.yaml
EOT
fi
