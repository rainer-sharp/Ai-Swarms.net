# Knoten unter Linux automatisch starten

Für Rechner, auf denen ein Knoten dauerhaft laufen soll (VM, Server, Arbeitsplatz), ohne root: als **systemd-Benutzerdienst** des Benutzers, dem `~/.swarmainet/` gehört. Für einen eigenen Systembenutzer, wie beim Helfer auf dem Raspberry Pi, siehe [`deploy/raspberry-pi/`](../raspberry-pi/README.md).

```sh
# Programm bauen und an einen festen Ort kopieren (ein späteres go build im
# Projekt ändert den laufenden Dienst so nicht versehentlich)
go build -o swarmainet ./cmd/swarmainet
install -D -m 0755 swarmainet ~/.local/bin/swarmainet

# Dienst einrichten
install -D -m 0644 deploy/linux/swarmainet.service ~/.config/systemd/user/swarmainet.service
systemctl --user daemon-reload

# Auch ohne Anmeldung beim Hochfahren starten
loginctl enable-linger "$USER"

# Einen im Terminal laufenden Knoten vorher beenden (Strg+C), dann:
systemctl --user enable --now swarmainet
```

Bedienen:

```sh
systemctl --user status swarmainet
journalctl --user -u swarmainet -f        # Protokoll
systemctl --user restart swarmainet       # nach Änderungen an der Konfiguration
swarmainet status                          # wie gewohnt
```

**Aktualisieren:** neu bauen, nach `~/.local/bin/swarmainet` kopieren, `systemctl --user restart swarmainet`.

## Knoten für ein Gruppennetz einrichten

`gruppe-beitreten.sh` erledigt auf einem frischen Rechner alles nach dem `git clone`, auch die Installation von Go, und läuft ohne root. Getestet mit Ubuntu 24.04; `git`, `python3` und `systemd` werden vorausgesetzt.

```sh
git clone https://github.com/rainer-sharp/Ai-Swarms.net.git
cd Ai-Swarms.net
deploy/linux/gruppe-beitreten.sh
```

Das Skript lädt die Go-Version aus `go.mod` nach `~/.local/go` und prüft die Prüfsumme, baut das Programm, legt `~/gruppe-test` an, fragt nach dem Einladungslink und startet den Knoten. Ein vorhandener SwarmAiNet-Knoten bleibt unberührt, weil Verzeichnis, Port (4714) und Gateway (7715) eigene sind. Ein erneuter Aufruf aktualisiert das Programm und startet den Knoten neu, ohne erneut beizutreten.

Den Link erzeugt der Aussteller: `swarmainet member invite -net m-… -peer <adresse des helfers>`.

```sh
deploy/linux/gruppe-beitreten.sh status      # Verbindungen
deploy/linux/gruppe-beitreten.sh test        # Probeauftrag
deploy/linux/gruppe-beitreten.sh stop        # beenden
deploy/linux/gruppe-beitreten.sh entfernen   # Knoten und Verzeichnis löschen
```

Der Knoten läuft als vorübergehender Dienst und übersteht keinen Neustart des Rechners; dann das Skript erneut aufrufen. Für den Dauerbetrieb gilt die Anleitung oben.
