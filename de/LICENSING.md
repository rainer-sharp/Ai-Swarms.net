# Lizenzen

[English](../LICENSING.md) · **Deutsch**

AI-Swarms.net ist ein Community-Projekt. Eine kommerzielle Lizenz gibt es nicht, und sie ist auch nicht geplant.

## Code: GNU AGPL-3.0-or-later

Der Programmcode ist freie Software unter der [GNU Affero General Public License](../LICENSE), Version 3 oder nach Ihrer Wahl jeder späteren Version (`AGPL-3.0-or-later`). Sie dürfen ihn ohne Einschränkung nutzen, verändern und weitergeben, auch in einer Firma und auch für Geld. Die Bedingung: Wer veränderte Fassungen weitergibt **oder anderen über ein Netz zur Verfügung stellt**, muss deren vollständigen Quellcode unter der AGPL offenlegen.

Ohne Weiteres erlaubt sind zum Beispiel:

- eigene Knoten, Helfer und Tracker betreiben, privat oder in einer Firma,
- AI-Swarms.net unverändert weitergeben oder als Dienst anbieten,
- Änderungen, die Sie unter der AGPL veröffentlichen.

## Dokumentation: CC BY 4.0

Die Dokumentation steht unter der [Creative Commons Namensnennung 4.0 International](../LICENSE-DOCS) (CC BY 4.0). Sie dürfen sie für jeden Zweck kopieren, verändern und weitergeben, wenn Sie den Urheber nennen, auf die Lizenz verweisen und Änderungen kenntlich machen. Das erleichtert insbesondere eigene Umsetzungen des Protokolls nach der Spezifikation.

Als Dokumentation gelten:

- alles unter `docs/`, einschließlich des Gesamtkonzepts (`docs/concept.md`, `docs/de/concept.md`, Grafiken) und seiner archivierten ersten Fassung (`docs/archive/concept-2026-10-14-de.pdf`),
- alles unter `spec/`,
- die Anleitungen `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `LICENSING.md`, `deploy/*/README.md`, `bench/README.md` und `bench/report-r1.md` sowie ihre deutschen Fassungen in den Unterordnern `de/` (z. B. `de/README.md`, `deploy/vps/de/README.md`),
- die Grafiken `deploy/raspberry-pi/*.svg`,
- die Testvektoren der Mitgliedschaft unter `internal/membership/testdata/`, damit andere Umsetzungen sie frei verwenden können.

Namensnennung zum Beispiel: „AI-Swarms.net-Dokumentation, © 2026 Rainer Sestig und Mitwirkende, CC BY 4.0, github.com/rainer-sharp/Ai-Swarms.net“.

Nicht dazu gehören der Programmcode, Konfigurationsvorlagen, Skripte und die übrigen Testdaten (AGPL-3.0-or-later) sowie die Rechtstexte `LICENSE`, `LICENSE-DOCS` und `DCO`.

## Bestandteile mit eigener Lizenz

- Die Benchmark-Aufgaben in `bench/` stammen aus HumanEval und GSM8K (© OpenAI, MIT-Lizenz, siehe `bench/LICENSE-human-eval` und `bench/LICENSE-gsm8k`).
- Eingebundene Go-Bibliotheken stehen unter ihren eigenen Lizenzen (MIT, BSD, ISC, Apache-2.0, MPL-2.0).

## Beiträge

Beiträge stehen unter den Lizenzen oben: Code unter AGPL-3.0-or-later, Dokumentation unter CC BY 4.0. Jeder Commit trägt eine Zeile `Signed-off-by`, mit der Beitragende das [Developer Certificate of Origin](../DCO) bestätigen, siehe [CONTRIBUTING.md](CONTRIBUTING.md#lizenz-und-sign-off). Die Urheberrechte bleiben bei den Beitragenden; Rechte werden nicht übertragen.
