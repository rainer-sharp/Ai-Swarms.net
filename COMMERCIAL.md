# Lizenzen

Der Programmcode von SwarmAiNet steht unter zwei Lizenzen zur Wahl (Doppellizenz):

1. **GNU Affero General Public License 3.0** ([`LICENSE`](LICENSE)), kostenlos.
2. **Kommerzielle Lizenz** von Rainer Sestig, auf Anfrage.

Die Dokumentation steht unter [CC BY 4.0](#dokumentation-cc-by-40).

## Wann reicht die AGPL-3.0?

In den meisten Fällen. Sie dürfen SwarmAiNet ohne Einschränkung nutzen, verändern und weitergeben, auch in einer Firma und auch für Geld. Die Bedingung: Wer veränderte Fassungen weitergibt **oder anderen über ein Netz zur Verfügung stellt**, muss deren vollständigen Quellcode unter der AGPL-3.0 offenlegen.

Ohne Weiteres erlaubt sind zum Beispiel:

- eigene Knoten, Helfer und Tracker betreiben, privat oder in einer Firma,
- SwarmAiNet unverändert weitergeben oder als Dienst anbieten,
- Änderungen, die Sie unter der AGPL-3.0 veröffentlichen.

## Wann brauchen Sie die kommerzielle Lizenz?

Wenn Sie die Bedingungen der AGPL-3.0 nicht erfüllen können oder wollen, zum Beispiel:

- SwarmAiNet oder Teile davon in ein Produkt einbauen, dessen Quellcode Sie nicht offenlegen,
- einen veränderten Knoten, Helfer oder Tracker als Dienst für andere betreiben, ohne die Änderungen zu veröffentlichen,
- SwarmAiNet mit Software kombinieren, deren Lizenz sich mit der AGPL-3.0 nicht verträgt.

Die kommerzielle Lizenz wird je Fall vereinbart.

**Kontakt:** [rsestig@sestig.de](mailto:rsestig@sestig.de)

## Dokumentation: CC BY 4.0

Die Dokumentation steht unter der [Creative Commons Namensnennung 4.0 International](LICENSE-DOCS) (CC BY 4.0). Sie dürfen sie für jeden Zweck kopieren, verändern und weitergeben, auch kommerziell, wenn Sie den Urheber nennen, auf die Lizenz verweisen und Änderungen kenntlich machen. Das erleichtert insbesondere eigene Umsetzungen des Protokolls nach der Spezifikation.

Als Dokumentation gelten:

- alles unter `docs/`, einschließlich des Gesamtkonzepts (`docs/gesamtkonzept.pdf`),
- alles unter `spec/`,
- die Anleitungen `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `deploy/*/README.md`, `bench/README.md` und `bench/report-r1.md`,
- die Grafiken `deploy/raspberry-pi/*.svg`,
- die Testvektoren der Mitgliedschaft unter `internal/membership/testdata/`, damit andere Umsetzungen sie frei verwenden können.

Namensnennung zum Beispiel: „SwarmAiNet-Dokumentation, © 2026 Rainer Sestig, CC BY 4.0, github.com/rainer-sharp/Ai-Swarms.net“.

Nicht dazu gehören der Programmcode, Konfigurationsvorlagen, Skripte und die übrigen Testdaten (AGPL-3.0 bzw. kommerzielle Lizenz) sowie die Rechtstexte `LICENSE`, `LICENSE-DOCS`, `COMMERCIAL.md` und `CLA.md`.

## Bestandteile mit eigener Lizenz

- Die Benchmark-Aufgaben in `bench/` stammen aus HumanEval und GSM8K (© OpenAI, MIT-Lizenz, siehe `bench/LICENSE-human-eval` und `bench/LICENSE-gsm8k`).
- Eingebundene Go-Bibliotheken stehen unter ihren eigenen Lizenzen (MIT, BSD, ISC, Apache-2.0, MPL-2.0). Sie gelten unverändert auch bei der kommerziellen Lizenz.

## Beiträge

Damit beide Lizenzen möglich bleiben, gelten Beiträge nur mit der [Beitragsvereinbarung](CLA.md).
