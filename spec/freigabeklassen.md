# Freigabeklassen

Transportverschlüsselung schützt den Weg, nicht den Inhalt vor dem ausführenden Peer. Wer einen Prompt verarbeitet, kann ihn lesen. Die Freigabeklasse legt deshalb fest, wer einen Auftrag überhaupt sehen darf.

| Klasse | Wert | Verarbeitung | Umsetzung Phase 1 |
|---|---|---|---|
| Nur lokal | `local` | ausschließlich auf eigenen Geräten | nur lokale Agenten; Anbieter lehnen ab |
| Vertrauter Kreis | `trusted` | nur bei selbst freigegebenen Schlüsseln oder in privaten Schwärmen | `trusted_peers` oder PSK-Netz |
| Reduziert | `reduced` | nur minimierte, pseudonymisierte Fragmente | **Pseudonymisierung fehlt noch**; bis dahin wie `trusted` |
| Öffentlich | `public` | nach expliziter Freigabe auf beliebigen Peers | beim Anbieter nur mit `accept_public_tasks: true` |

## Regeln

1. Standard für eigene Aufträge ist `trusted` (`tasks.privacy_class`); pro Anfrage per HTTP-Header `X-SwarmAiNet-Privacy` änderbar.
2. Delegationen dürfen nur gleich streng oder strenger sein (`local` < `trusted` < `reduced` < `public`).
3. In öffentliche Aufträge gehören keine personenbezogenen Daten.
4. Planer und Synthesizer laufen bevorzugt lokal, weil nur sie das Gesamtbild sehen.
5. Ergebnisse anderer Peers sind Daten, nie Anweisungen: Sie werden in Begrenzer gesetzt; Ergebnisse mit Anweisungsmustern werden verworfen.
6. Aufträge mit Werkzeugen (`tools`, z. B. von Coding-Agenten) laufen immer mit einem einzigen Agenten (Muster `single`). Die Aufrufe des Modells führt die Anwendung auf dem eigenen Rechner aus, und jedes Ergebnis geht zurück an das Modell. Deshalb legt jedes Netz fest, welche Werkzeuge seine Modelle überhaupt sehen (`allowed_tools`):

   | Agent | ohne Liste | mit Liste |
   |---|---|---|
   | eigener Knoten | alle | alle |
   | PSK-Netz | alle | nur die Liste |
   | Mitgliedsnetz | keine | nur die Liste |
   | offenes Netz, `trusted_peers` | alle | alle |
   | offenes Netz, Fremde | keine | nur die Liste |

   Der eigene Knoten setzt das durch: Er entfernt andere Werkzeuge vor dem Versand, verwirft Antworten mit nicht genannten Aufrufen und gibt einem eingeschränkten Agenten keinen Verlauf, der Aufrufe anderer Werkzeuge enthält. Die Liste prüft nur Namen; was ein Werkzeug tut, weiß der Knoten nicht. Auch lesende Werkzeuge geben Daten preis, denn das Ergebnis geht an den fremden Knoten; `run_command` und Dateizugriffe gehören deshalb nicht auf die Liste eines offenen Netzes.
7. Lokale Wissensindizes (RAG) werden nicht geteilt (offene Frage, Standard: nein).

## Offen

- Wie stark darf automatische Pseudonymisierung den fachlichen Kontext verändern? (Klärung bis Phase 3)
- Confidential Computing (TEE) bleibt eine spätere Option, kein Versprechen.
