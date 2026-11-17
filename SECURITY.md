# Sicherheit

## Schwachstellen melden

Bitte melden Sie Schwachstellen **nicht** in öffentlichen Issues, sondern vertraulich über GitHub: *Security → Report a vulnerability* in diesem Repository. Nennen Sie die betroffene Version (Commit), die Netzart und, wenn möglich, Schritte zum Nachstellen. Sie erhalten eine Rückmeldung, sobald die Meldung gesichtet ist.

## Was als Schwachstelle zählt

- Ein Knoten ohne Schlüssel oder gültiges Zertifikat kommt in ein privates oder ein Gruppennetz.
- Ein gesperrtes Mitglied oder ein Helfer kann Aufträge stellen, annehmen oder mitlesen.
- Anfragen erreichen Knoten, die sie nach ihrer Freigabeklasse nicht sehen dürfen.
- Fremde Modelle sehen oder rufen Werkzeuge auf, die für ihr Netz nicht freigegeben sind.
- Das Gateway ist von einem anderen Rechner aus erreichbar.
- Ein fremder Knoten kann einen anderen zum Absturz bringen oder seine Ressourcen über die Grenzen hinaus belegen.

Bekannte Grenzen stehen im [Bedrohungsmodell](spec/bedrohungsmodell.md) und unter [Stand](docs/stand.md#bekannte-lücken).

## Hinweise für den Betrieb

- SwarmAiNet ist nicht unabhängig geprüft. Vertrauliche Inhalte gehören nur in private Netze oder Gruppennetze.
- Schlüsseldateien unter `~/.swarmainet/` und Einladungslinks mit Schlüssel sind Zugangsdaten. Wer sie hat, ist Mitglied.
- Werkzeuge führt Ihre Anwendung aus, nicht das Modell. Ein Modell, das Werkzeuge sieht, kann Ihre Anwendung zu deren Aufruf bewegen. Geben Sie fremden Netzen nur Werkzeuge frei, deren Aufruf unbedenklich ist.
- Den Wurzelschlüssel eines Gruppennetzes offline lagern.
