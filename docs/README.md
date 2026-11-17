# Dokumentation

**Für Nutzer**

| Seite | Inhalt |
|---|---|
| [Installation](installation.md) | Bauen, erster Knoten, wichtigste Einstellungen, Dauerbetrieb |
| [Nutzung](nutzung.md) | API, Modellnamen und Muster, Open WebUI, Coding-Agenten und Werkzeuge, Befehle, Diagnose |
| [Netzarten](netze.md) | Privates Netz, Mitgliedsnetz, offenes Netz; mehrere Standorte, Einladungen, Helfer |
| [Konfiguration](konfiguration.md) | Schlüssel der Konfiguration und Dateien des Knotens |
| [Stand](stand.md) | Was erprobt ist, was fehlt, Benchmark |

**Betrieb**

| Anleitung | Inhalt |
|---|---|
| [`deploy/linux/`](../deploy/linux/README.md) | systemd-Benutzerdienst; Skript für den Beitritt zu einem Gruppennetz |
| [`deploy/docker/`](../deploy/docker/compose.yaml) | Image und Compose-Vorlage für Helferknoten und Tracker |
| [`deploy/vps/`](../deploy/vps/README.md) | Startknoten des offenen Netzes und Helfer auf einem VPS |
| [`deploy/raspberry-pi/`](../deploy/raspberry-pi/README.md) | Helfer hinter einer Portfreigabe, abgeschottet vom Heimnetz |

**Für Entwickler**

| Seite | Inhalt |
|---|---|
| [Aufbau](architektur.md) | Module und Pakete |
| [Spezifikation](../spec/README.md) | Protokoll, P2P-Schicht, Mitgliedschaft, Freigabeklassen, Bedrohungsmodell, Kooperation, Messplan |
| [Benchmark](../bench/README.md) | Aufgaben, Ablauf, Ergebnisse |
| [Gesamtkonzept](gesamtkonzept.pdf) | Ursprüngliches Konzept (PDF) |
| [Mitwirken](../CONTRIBUTING.md) | Bauen, Testen, Konventionen, Beitragsvereinbarung |
| [Lizenzen](../COMMERCIAL.md) | Code: AGPL-3.0 und kommerzielle Lizenz; Dokumentation: CC BY 4.0 |
