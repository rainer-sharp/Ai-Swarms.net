# Freigabeklassen

[English](../privacy-classes.md) · **Deutsch**

Transportverschlüsselung schützt den Weg, nicht den Inhalt vor dem ausführenden Peer. Wer einen Prompt verarbeitet, kann ihn lesen. Die Freigabeklasse legt deshalb fest, wer einen Auftrag überhaupt sehen darf.

| Klasse | Wert | Verarbeitung | Umsetzung Phase 1 |
|---|---|---|---|
| Nur lokal | `local` | ausschließlich auf eigenen Geräten | nur lokale Agenten; Anbieter lehnen ab |
| Vertrauter Kreis | `trusted` | nur bei selbst freigegebenen Schlüsseln oder in privaten Schwärmen | `trusted_peers` oder PSK-Netz |
| Reduziert | `reduced` | nur minimierte, pseudonymisierte Fragmente | eigene und vertraute Agenten sehen das Original, fremde nur eine gekürzte Aufgabe mit Platzhaltern (Regel 9); Anbieter nehmen es von Fremden an wie `public` |
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
7. Tonaufnahmen (Transkription, Fähigkeit `audio.transcribe`) gehen höchstens bis `trusted`, nie `public`: Stimmen sind personenbezogene Daten, und der Inhalt einer Aufnahme lässt sich nicht minimieren. Auch nicht als `reduced`, denn eine Aufnahme lässt sich nicht pseudonymisieren. Koordinator und Anbieter setzen das beide durch ([Protokoll](protocol-v0.1.md) § 4.2).
8. Lokale Wissensindizes (RAG) werden nicht geteilt (offene Frage, Standard: nein).
9. **Minimierung und Pseudonymisierung (`reduced`):** Eigene und vertraute Agenten sehen das Original. Bevor ein Teilauftrag an einen fremden Agenten geht, kürzt der Koordinator das Gespräch und ersetzt persönliche Daten durch Platzhalter wie `[PERSON_1]`, `[ORG_1]`, `[LOCATION_1]` oder `[EMAIL_1]`, weist das Modell an, sie beizubehalten, und tauscht sie in der Antwort zurück, auch beim Streaming und in den Argumenten von Werkzeug-Aufrufen.
   - **Minimierung:** Enthält die Anfrage mehr als eine einzelne Frage (Verlauf, Systemanweisungen), schreibt ein eigenes oder vertrautes Modell zuerst die letzte Anfrage als in sich geschlossene Aufgabe, nur mit dem, was zur Beantwortung nötig ist (Schritt mit der Rolle `router`). Fremde Agenten, auch als Prüfer oder Synthese, bekommen diese Aufgabe statt des Gesprächs. Mit Werkzeugen bleibt der Verlauf vollständig, denn das Modell braucht seine Aufrufe und deren Ergebnisse.
   - **Feste Regeln** finden E-Mail-Adressen, Telefonnummern, IBAN (mit Prüfsumme), Kartennummern (Luhn-Prüfung), IP-Adressen (außer Loopback) und URLs mit Zugangsdaten.
   - **Namen, Firmen und Orte** markiert ein eigenes oder vertrautes Modell (Schritt mit der Rolle `router`; es sieht das Original ohnehin). Es zählen nur Funde, die wörtlich im Original oder in der gekürzten Aufgabe stehen. Dazu werden die Begriffe aus `tasks.pseudonymize_terms` immer ersetzt.
   - **Ohne Minimierung und Erkennung geht nichts an Fremde:** Ist kein eigenes oder vertrautes Modell verfügbar oder scheitert einer der beiden Schritte, bekommen fremde Agenten für die ganze Anfrage nichts; gibt es nur fremde, scheitert die Anfrage mit 400.
   - **Anbieter** nehmen `reduced` von Fremden nur mit `accept_public_tasks` an, wie bei `public`.
   - **Grenzen:** Das Kürzen übernimmt ein Modell; es kann Nötiges weglassen oder Unnötiges behalten. Ein Modell kann Namen übersehen, und was eine Person mittelbar erkennbar macht (eine Rolle, ungewöhnliche Einzelheiten), bleibt im Text. Was den vertrauten Kreis auf keinen Fall verlassen darf, gehört zu `trusted`.
10. **Bilder** in Anfragen gehen nur an Agenten, die Bilder verstehen (Modalität `image`, in der Konfiguration `vision: true`), auch als Prüfer oder Synthese. Ein Bild lässt sich nicht pseudonymisieren: Bei `reduced` bleiben Anfragen mit Bildern im vertrauten Kreis. `public` ist erlaubt wie bei Text: Wer eine Anfrage öffentlich freigibt, entscheidet, dass Fremde ihre Bilder sehen dürfen; Fotos von Menschen und eingescannte Dokumente gehören zu `trusted`. Koordinator und Anbieter setzen das beide durch ([Protokoll](protocol-v0.1.md) § 4.2).

## Offen

- Wie stark darf automatische Pseudonymisierung den fachlichen Kontext verändern? Platzhalter erhalten den Aufbau eines Textes, verlieren aber etwa Geschlecht oder Region; ob realistische Ersatzwerte besser wären, ist offen. Ebenso, wie viel beim Kürzen verloren gehen darf.
- Confidential Computing (TEE) bleibt eine spätere Option, kein Versprechen.
