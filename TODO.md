# YaFT – Roadmap

Stand: 2026-09-23 (Phasen 0–2 abgeschlossen)

Dieses Dokument bündelt die offenen Vorhaben rund um das YaFT-Ökosystem
(Go-Backend, `@tehw0lf/yaft` für TypeScript, `yaft-admin`, weitere Sprach-Ports).

## Hier weitermachen

Stand 2026-09-23. **Phase 0 und Phase 2 sind abgeschlossen.**
`https://yaft.tehwolf.de` läuft auf der OCI-Instanz.

| Repo | Version | Stand |
|---|---|---|
| `Go/YaFT` | 0.3.1 | Images publiziert, OpenAPI-Spec, deployt |
| `yaft-conformance` | 1.1.0 | 27 Regeln, 90 Fälle |
| `TypeScript/yaft` | 0.0.16 | besteht alle 90 Fälle; **erstmals importierbar publiziert** |
| `TypeScript/yaft-playground` | 0.1.0 | CI grün inkl. 8 E2E gegen echtes Backend |
| `Docker/tehwolf.de/yaft` | – | läuft auf `yaft.tehwolf.de` |

**Nächster Schritt: Phase 3** (Ports: yaft-java, dann yaft-go).

Deploy verifiziert am 2026-09-23: `GET /features/nothing` → `404` mit
`{"error":"Feature not found"}`, HSTS gesetzt, CORS spiegelt den `Origin`;
`cron.job` enthält die beiden Minuten-Jobs und die Retention (`0 3 * * *`);
`yaft-db` veröffentlicht keinen Port.

Zum Neuaufsetzen der Instanz:

1. `Docker/tehwolf.de/yaft/.env` anlegen (gitignored):
   ```
   POSTGRES_USER=yaft
   POSTGRES_PASSWORD=<openssl rand -hex 24>
   POSTGRES_DB=yaft
   ```
   **`.env` greift nur beim allerersten Start.** User und Passwort schreibt
   das Postgres-Image einmalig ins leere Volume; später geänderte Werte
   ignoriert es. Wer sie ändert, braucht `down -v` (löscht die Daten) oder
   `ALTER ROLE` von Hand.
2. `docker compose -f yaft/docker-compose.yml up -d`
3. Retention scharfschalten (bewusst nicht vorgeplant, nur **einmal**):
   ```
   docker compose -f yaft/docker-compose.yml exec yaft-db psql -U yaft -d yaft \
     -c "SELECT cron.schedule('0 3 * * *', \$\$ SELECT cleanup_stale_feature_toggles(); \$\$);"
   ```
4. Prüfen: `curl https://yaft.tehwolf.de/features/nothing` → `404` mit
   JSON-Fehler; die DB darf **keinen** Port veröffentlichen.

**Offen und nicht im Code lösbar:** die GHCR-Pakete sind privat. Die
Playground-CI baut die Images deshalb aus dem öffentlichen YaFT-Repo statt sie
zu ziehen. Auf `public` umstellen wäre einfacher — dann greift wieder der
normale Pull-Pfad.

**Klein, ohne Eile:** Die CORS-Middleware spiegelt jeden `Origin` und setzt
`Allow-Credentials: true`. Ausnutzbar ist das nicht — die API kennt keine
Cookies, das Secret steht im Pfad —, aber `Allow-Credentials` ist damit
überflüssig, und `Vary: Origin` fehlt für den Fall, dass je ein Cache
davorsteht.

### Was Phase 2 unterwegs gefunden hat

Das Ausprobieren statt Lesen hat sich gelohnt — sieben echte Fehler, der
letzte erst beim Deploy:

- `init.sql` fehlte im `yaft-db`-Image; eine gezogene DB hatte keine
  Cronjobs und **sah dabei gesund aus**, weil AutoMigrate die Tabelle anlegt.
- Volume auf `/var/lib/postgresql/data` statt `/var/lib/postgresql` —
  Postgres 18 startet damit nicht.
- `cron.database_name` wurde zur Buildzeit expandiert, stand also fest auf
  `postgres`; mit dem `init.sql`-Fix führte das zu `Exited (3)`.
- Das `sed` im Entrypoint brach bei einem DB-Namen wie `a|b`.
- `COPY` übernahm die umask des Build-Hosts, `init.sql` wurde unlesbar.
- **`@tehw0lf/yaft` war nie importierbar** — kein `main`, kein `types`, und
  publiziert wurde rohes `src/` statt `dist/yaft/`. Deshalb hat `yaft-admin`
  die Provider nachgebaut, statt die Library zu nutzen.
- **Das Passwort stand im `DB_DSN`-URL.** Ein generiertes Passwort mit `%`
  machte die URL unparsebar (`invalid URL escape "%*F"`), die API startete
  in Schleife neu. Jetzt kommen User und Passwort über `PGUSER`/`PGPASSWORD`,
  die pgx liest, wenn die DSN sie auslässt — geprüft mit `a%*F@/:#?b`, samt
  Gegenprobe mit falschem Passwort.

## Ausgangslage

| Komponente | Ort | Version | Status |
|---|---|---|---|
| Go-Backend (dieses Repo) | `Go/YaFT` | 0.2.0 | Phase 1 erledigt; seit 0.2.0 eine einheitliche, kleingeschriebene Antwortform (R22a); CI publisht `ghcr.io/tehw0lf/yaft` + `yaft-db` |
| TypeScript-Library | `TypeScript/yaft` | 0.0.13 | Decorator `@FeatureToggle`, Provider-Interface, 4 Beispiel-Provider, zentrales `evaluate`/`mapping`, **besteht yaft-conformance v1.1.0 vollständig** |
| Admin-UI (Angular/Nx) | `TypeScript/yaft-admin` | 1.1.9 | nutzt `@tehw0lf/yaft` bereits mit LocalStorage- und API-Provider |

Befunde aus dem Code, die den Plan prägen:

- ~~Die Zeitlogik (`isEnabled`) ist in yaft-ts dreimal identisch dupliziert.~~
  **Erledigt (yaft-ts 0.0.12).** Die Angabe war zudem falsch: sie war **zweimal**
  dupliziert, in den beiden Feature-Providern. Die Boolean-Provider bilden
  direkt auf `isEnabled` ab und haben per Design keine Zeitlogik. Jetzt zentral
  in `evaluate(feature, now)`, Uhr injizierbar.
- Der Klassen-Decorator wertet den Toggle einmal beim Laden der Klasse aus,
  der Methoden-Decorator bei jedem Aufruf. Das ist beobachtbares Verhalten und
  muss für Ports festgelegt werden.
- Die Backend-Cronjobs in `db/init.sql` vergleichen `active_at`/`disabled_at`
  gegen `CURRENT_DATE` (Mitternacht des Tages), die Library gegen die Uhrzeit.
  Ein `disabledAt` von 15:00 Uhr flippt im Backend erst am Folgetag, in der
  Library um 15:00 Uhr.
- Das Backend liefert unbesetzte Daten als `null`, die TS-Fixtures nutzen `""`.
- Es gibt keinen automatisierten End-to-End-Test der TS-Library gegen ein
  echtes Go-Backend. Die Jest-Tests laufen gegen Fixtures und gemockte
  HTTP-Antworten.
- **Neu entdeckt und in Phase 1 behoben:** die Go-Integrationstests testeten
  `main.go` überhaupt nicht. `main_test.go` enthielt eine zweite, vereinfachte
  Kopie aller Handler, und die Tests liefen gegen diese Kopie; die echten
  Routen hingen in `main()` und waren von Tests aus unerreichbar. Die Kopie war
  bereits auseinandergelaufen (kein `tags` in den Responses) und enthielt die
  `...At`-Routen gar nicht — deshalb konnte der Panic oben unbemerkt
  überleben. Routen stecken jetzt in `setupRouter()`, das `main()` und die
  Tests gemeinsam benutzen.
- ~~`GET /features/:key` liefert zwei verschiedene Formen: großgeschrieben in
  der Gruppe, kleingeschrieben einzeln.~~ **Behoben in 0.2.0.** Das DTO hat
  JSON-Tags, beide Hüllen liefern kleingeschrieben, und die sechs
  handgeschriebenen `gin.H`-Antworten sind durch `toDTO` ersetzt. Ports müssen
  die großgeschriebene Form trotzdem lesen können, solange ältere Instanzen
  laufen — steht als R22a in der Suite.
- ~~`time.Parse` in `activateAt`/`deactivateAt` verwirft den Fehler mit `_`.~~
  **Erledigt in Phase 1, und der Befund war zu harmlos formuliert:** die
  Handler schrieben mit `*toggle.ActiveAt, _ = time.Parse(...)` durch genau
  den Pointer, den sie setzen wollten. `ActiveAt`/`DisabledAt` sind
  `*time.Time` und `NULL`, solange kein Datum gesetzt ist — auf jedem frisch
  angelegten Toggle war das ein Nil-Pointer-Dereference, also ein Panic und
  ein `500`, kein stiller Datenfehler. Der verworfene Parse-Fehler war nur die
  zweite Hälfte. Beides behoben, ungültige Daten liefern jetzt `400`.
- Der Porting-Prompt beschrieb an mehreren Stellen eine Implementierung, die
  es nie gab (Bearer-Auth statt Secret im Pfad, erfundene Provider, Decorator
  einmalig auch für Methoden). Er ist deshalb gelöscht; die geprüften Teile
  stehen korrigiert in Phase 3.

## Entscheidungen (2026-09-18)

| Frage | Entscheidung |
|---|---|
| Ort der Konformitäts-Suite | eigenes Repo `yaft-conformance` |
| Wahrheit für `activeAt`/`disabledAt` | Library prüft selbst; Backend-Cron wird auf `now()` angeglichen, damit beide zur Uhrzeit auswerten |
| Mini-App | eigenes Repo `yaft-playground` (Angular, Jest, Playwright, Compose-Backend) |
| ARM-Ziel | bestehende Oracle-OCI-Instanz (hostet u. a. tehwolf.de) |
| Auswertungszeitpunkt Decorator | normativ: Klasse einmal beim Laden, Methode bei jedem Aufruf; steht als Regel in `SPEC.md` und in jeder Port-README |
| Datumsformat | nur RFC 3339 mit Offset; alles andere gilt als ungültig und wird ignoriert |
| Go-Port | eigenes Repo `yaft-go`; dieses Repo bleibt reines Backend |
| Suite-Verteilung | Release-Download, gepinnt über Version + Prüfsumme (siehe Phase 0); Submodule bewusst verworfen |
| Wertvalidierung Backend | `POST /features` lehnt alles außer `"true"`/`"false"` mit `400` ab |
| OCI-Deployment | dauerhaft unter `yaft.tehwolf.de`, Compose-Stack in `Docker/tehwolf.de/yaft/` nach dem Traefik-Muster der anderen Services |
| Schutz der öffentlichen API | Cloudflare-Proxy (vorhanden) gegen volumetrische Angriffe; Traefik-Ratelimit per Label, geschlüsselt auf `CF-Connecting-IP`; Key-Längenlimit und 30-Tage-Aufräumjob im Backend |
| Ort der Port-Vorgaben | `YAFT_PORTING_PROMPT.md` gelöscht; Vorgaben stehen in Phase 3 dieses Dokuments, normative Regeln wandern mit Phase 0 nach `yaft-conformance/SPEC.md` |

## Phase 0 – Konformitäts-Suite (`yaft-conformance`)

Ziel: Die Regeln von YaFT einmal sprachneutral als Daten festschreiben. Jede
Implementierung (yaft-ts, spätere Ports) besteht dieselben Fälle, oder sie ist
nicht konform.

### Repo-Aufbau

```
yaft-conformance/
├── README.md              Wie ein Port die Suite einbindet
├── SPEC.md                Normative Regeln in Prosa, nummeriert (R1, R2, ...)
├── VERSION                Suite-Version, Ports pinnen einen Git-Tag
├── schema/
│   └── case.schema.json   JSON-Schema für die Fall-Dateien
└── cases/
    ├── evaluation.json    isEnabled-Regeln (Wert, Zeitfenster, Fehlerfälle)
    ├── decorator.json     Verhalten von Klassen-/Methoden-Toggles und Fallbacks
    └── mapping.json       Normalisierung von Backend-Antworten und Boolean-Shape
```

### Fall-Format

Jeder Fall bringt seine eigene "Jetzt"-Zeit mit. Das macht die Zeitlogik
deterministisch und zwingt Ports zu einer injizierbaren Uhr.

```json
{
  "suite": "evaluation",
  "version": 1,
  "cases": [
    {
      "name": "not-yet-active",
      "rule": "R3",
      "now": "2026-09-18T12:00:00Z",
      "features": {
        "f": { "key": "f", "value": "true", "activeAt": "2099-12-31T23:59:59Z", "disabledAt": "" }
      },
      "key": "f",
      "expected": false
    }
  ]
}
```

### Abzudeckende Regeln

`evaluation.json` (abgeleitet aus der heutigen `isEnabled`-Implementierung):

- Nur exakt `"true"` aktiviert. `"TRUE"`, `"1"`, `""`, fehlender Wert ergeben `false`.
- Fehlender Key, `null`-Feature ergeben `false`.
- `activeAt` leer, `null` oder ungültig wird ignoriert.
- `activeAt` in der Zukunft ergibt `false`; `now == activeAt` ergibt `true`
  (Vergleich ist `now < activeAt`).
- `disabledAt` in der Vergangenheit ergibt `false`; `now == disabledAt` ergibt
  `false` (Vergleich ist `now >= disabledAt`).
- Beide gesetzt: innerhalb des Fensters `true`, davor und danach `false`.
- `activeAt > disabledAt`: kein Sonderfall, wird normal ausgewertet.
- Zeitzonen-Offsets (`+02:00`) werden korrekt in die Vergleichszeit umgerechnet.
- Nur RFC 3339 mit Offset ist gültig. Reine Datumsangaben (`2026-09-18`) und
  Formate ohne Offset gelten als ungültig und werden ignoriert, weil Sprachen
  sie unterschiedlich parsen (JS: UTC-Mitternacht, andere: lokal).

`decorator.json` (abstrakte Erwartungen, jeder Port übersetzt sie in seine
Semantik):

- Toggle an: Original-Klasse bzw. Original-Methode wird benutzt.
- Toggle aus mit Fallback: Fallback-Klasse bzw. Fallback-Methode wird benutzt,
  Methoden-Fallback erhält dieselben Argumente und denselben Empfänger.
- Toggle aus ohne Fallback: Methode liefert "nichts" (undefined/null/None/Zero
  Value), Klasse wird durch eine Hülle ersetzt, deren Methoden allesamt
  "nichts" liefern; asynchrone Methoden liefern ein aufgelöstes Promise.
- Provider nicht gesetzt: Fehler beim Dekorieren, nicht erst beim Aufruf.
- Auswertungszeitpunkt: Klasse beim Laden, Methode bei jedem Aufruf. Normativ,
  damit ein Reload in jedem Port gleich wirkt; Fälle prüfen, dass ein nach
  dem Laden geänderter Toggle die Klasse nicht mehr, die Methode aber sehr
  wohl beeinflusst.

`mapping.json`:

- Backend-Antwort `{ "toggles": [...] }` und `{ "value": [...] }` werden gleich
  behandelt; Feldnamen `key`/`Key`, `value`/`Value` usw. werden normalisiert.
- `null` und `""` bei Datumsfeldern sind gleichwertig "nicht gesetzt".
- Boolean-Provider-Shape `{ "myToggle": true }` bildet direkt auf `isEnabled` ab.

### Adapter pro Port

Jeder Port hat einen Test, der die Fall-Dateien lädt, aus `features` einen
Provider baut, `now` injiziert und `expected` prüft.

Verteilung per Release-Download statt Submodule, damit Fremd-Toolchains wie
Maven oder `go test` ohne Git-Handling auskommen:

- Jeder Tag von `yaft-conformance` veröffentlicht ein Release-Asset
  `cases.tar.gz` plus `cases.tar.gz.sha256`.
- Jeder Port hat eine Datei `conformance.lock` mit Version und Prüfsumme und
  ein kleines Skript (`scripts/fetch-conformance.sh`), das das Asset lädt,
  die Prüfsumme prüft und nach `test/conformance/` entpackt. Das Verzeichnis
  ist gitignored.
- Die CI ruft das Skript vor den Tests auf. Ein Suite-Bump ist damit eine
  Änderung an `conformance.lock`, sichtbar im Diff und reviewbar.
- Ohne Prüfsumme würde ein stilles Überschreiben eines Tags die Tests
  unbemerkt verändern; deshalb ist sie Pflicht.

### Folgearbeiten in yaft-ts

1. ✅ **Erledigt (0.0.12).** Zeitlogik aus den Feature-Providern in eine
   zentrale Funktion `evaluate(feature, now)` gezogen; die Provider rufen nur
   noch sie auf.
2. ✅ **Erledigt (0.0.12).** Uhr injizierbar (`Clock = () => number`, Default
   `systemClock`). Beide Feature-Provider nehmen sie als optionales
   Konstruktor-Argument, Bestandscode bleibt unverändert.
3. ✅ **Erledigt (yaft-ts 0.0.13).** Adapter in
   `src/test/conformance-adapter/`, Suite per `conformance.lock` gepinnt, über
   `pretest` in jeden Testlauf eingebunden. 90 von 90 Fällen grün; drei Fehler
   dabei gefunden und behoben (R22, R23, R18).
4. ✅ **Erledigt (Suite 1.1.0).** Die normativen Regeln stehen als R1–R26 plus R22a
   in `yaft-conformance/SPEC.md`, die Fall-Dateien zeigen per `rules` darauf.
   Ein CI-Skript erzwingt, dass jede Regel mindestens einen Fall hat oder
   ausdrücklich als strukturell begründet ist. Phase 3 dieses Dokuments bleibt
   als Hintergrund stehen, normativ ist ab jetzt `SPEC.md`.

Mit 1 und 2 ist yaft-ts bereit für den Adapter: `evaluate`, `parseTimestamp`,
`Clock` und `systemClock` sind exportiert, und ein Fall aus den Fall-Dateien
lässt sich direkt als `evaluate(feature, Date.parse(case.now))` prüfen.

**Dabei festgelegt:** Nur RFC 3339 mit Offset gilt als gültig — das war im Plan
schon so entschieden, wurde aber von `Date.parse` nicht durchgesetzt. Bare
Dates (`2026-09-18`) und Zeitstempel ohne Offset werden jetzt ignoriert und
geloggt. Das ist eine Verhaltensänderung gegenüber 0.0.10 und steht in der
README von yaft-ts.

**Fallstrick für jeden Port** (im Review von yaft-ts#20 gefunden): Eine reine
Formatprüfung per Regex reicht nicht. `Date.parse` verwirft ein unmögliches
Kalenderdatum nicht, sondern rollt es weiter — `2027-02-30` wird zum 2. März.
Ein so um Tage verschobener Grenzwert ist schlimmer als ein ignorierter. Die
Komponenten müssen vor dem Parsen auf gültige Bereiche geprüft werden
(Schaltjahre inklusive). Sprachen mit ähnlich nachsichtigem Parser haben
dasselbe Problem; gehört als Regel nach `SPEC.md`.

Beim Testen darauf achten, dass das unmögliche Datum **in der Zukunft** liegt:
Ein Wert wie `2026-02-30` rollt in die Vergangenheit, wo die `activeAt`-Prüfung
zufällig durchgeht und der Test aus dem falschen Grund besteht. Genau dieser
Fehler steckte in der ersten Fassung der Tests.

Schaltsekunden (`23:59:60`) erlaubt RFC 3339, `Date.parse` kann sie nicht — in
yaft-ts werden sie deshalb ignoriert. Ports sollten das gleich handhaben.

### Stand der Suite (2026-09-21)

Angelegt unter `yaft-conformance/` in der Workspace-Wurzel, öffentlich unter
`github.com/tehw0lf/yaft-conformance`, aktueller Tag `v1.1.0` (v1.0.0 war der
erste Release, ohne R22a).

**Nicht** unter `TypeScript/`: die Suite enthält keine Zeile TypeScript,
sondern JSON-Fälle, ein Markdown-Spec, ein Python-Prüfskript und ein
Bash-Skript. Sie ist für jeden Port gleichermaßen da. `workflows/` liegt aus
demselben Grund auf oberster Ebene.

- `SPEC.md`: 27 Regeln. R1–R2 Datenmodell, R3–R9 Auswertung, R10–R13
  Zeitstempel (inkl. Kalender-Rollover und Schaltsekunde), R14–R19 Decorator,
  R20–R21 Provider-Shapes, R22–R25 Mapping, R26 Backend-Abgleich.
- 90 Fälle: 59 `evaluation`, 16 `decorator`, 15 `mapping` (Stand 1.1.0).
- `schema/case.schema.json` je Suite eigene Pflichtfelder; gegen fünf bewusst
  kaputte Dateien geprüft, alle abgelehnt.
- `scripts/validate-cases.py` prüft Regelverweise, doppelte Namen und
  unerzwungene Regeln; beide Fehlerfälle nachgestellt.
- Release-Workflow baut `cases.tar.gz` reproduzierbar (Tag muss zu `VERSION`
  passen), `scripts/fetch-conformance.sh` lädt und prüft die Summe; End-to-End
  gegen einen lokalen Server getestet, auch der Mismatch-Pfad.

Die Fälle für `decorator` und `mapping` sind keine reinen Ein-/Ausgabe-Paare —
"der Fallback bekommt denselben Empfänger" lässt sich nicht als JSON
ausdrücken. Sie benennen stattdessen ein Szenario und ein sprachneutrales
Ergebnis (`original`, `fallback`, `nothing`, `resolved-nothing`,
`empty-shell`, `decoration-error`), das jeder Adapter auf seine Sprache
abbildet.

Beim Verifizieren des Releases fiel noch ein echter Fehler auf: das Tarball
zeichnete die Rechte-Bits des Checkouts auf, also ergab derselbe Baum in der CI
(0644) und lokal bei restriktiver umask (0640) **verschiedene Prüfsummen** bei
identischem Inhalt. Das entwertet die Prüfsumme, die ein Port pinnt. Behoben
mit `--mode='u=rwX,go=rX'`; das damals veröffentlichte v1.0.0 trug bereits die
reproduzierbare Summe, ein Re-Release war nicht nötig. Für v1.1.0 wurde der
Nachbau aus dem Tag gegengeprüft und stimmt überein. Die README zeigt jetzt
den Nachbau-Befehl.

Ergebnis: Tag `yaft-conformance@v1.1.0` steht; das ist der Stand, gegen den ein
Port pinnt.

**Stand der Konformität: yaft-ts 0.0.13 besteht alle 90 Fälle.** Damit ist der
erste Port vollständig konform, und die Suite hat sich als das erwiesen, wofür
sie gebaut wurde: sie hat drei echte Fehler gefunden (R22, R23 und — neu — R18,
die ausgeschaltete `async`-Methode ohne Promise).

## Phase 1 – Backend härten und Zeitlogik angleichen ✅ ERLEDIGT

Umgesetzt auf `fix/phase-1-backend-hardening`, Version 0.1.6.

| # | Vorhaben | Status |
|---|---|---|
| 1 | Cronjobs `CURRENT_DATE` → `now()` | ✅ |
| 2 | Test des Cron-Flips | ✅ echter pg_cron-Lauf, siehe unten |
| 3 | `activateAt`/`deactivateAt` reparieren | ✅ war ein Panic, nicht nur ein Parse-Fehler |
| 4 | `POST /features` validiert `Value` | ✅ `400` außer bei `"true"`/`"false"` |
| 5 | `Key` auf 256 Zeichen begrenzen | ✅ inkl. UUID-Präfix, Test bei 256/257 |
| 6 | `CreatedAt`/`UpdatedAt` + Aufräumjob | ✅ Job existiert, ist aber bewusst **nicht** geplant |
| 7 | `VERSION` bumpen, PR | ✅ |

### Was unterwegs dazukam

**Die Tests testeten den Produktivcode nicht.** `main_test.go` hatte eine
zweite Kopie aller Handler; die Routen in `main.go` waren von Tests aus
unerreichbar. Das musste vor allem anderen weg, sonst hätten die neuen Guards
nur eine Attrappe geprüft. Routen liegen jetzt in `setupRouter()`.

**Der Datums-Bug war ein Crash.** `*toggle.ActiveAt, _ = time.Parse(...)`
dereferenzierte einen Nil-Pointer auf jedem Toggle ohne gesetztes Datum. Der
Endpunkt war für den Normalfall schlicht kaputt.

**Integrationstests gegen echtes PostgreSQL.** `docker-compose-test.yml` fährt
PostgreSQL mit pg_cron hoch, initialisiert aus `db/init.sql`. Abgedeckt:
Cron-Flip unter echtem Tick (~60 s), Retention, `collectionHash` (nutzt
PostgreSQL-only SQL und war auf SQLite nie ehrlich testbar) und ein
Schema-Drift-Check zwischen Struct und `init.sql`. Hinter Build-Tag
`integration`, eigener CI-Job; die schnelle SQLite-Suite bleibt unberührt.

Der Flip-Test wurde als unterscheidungsfähig verifiziert: mit `CURRENT_DATE`
schlägt er fehl, mit `now()` besteht er.

### Retention: standardmäßig aus

`cleanup_stale_feature_toggles()` löscht eine Toggle-Gruppe, wenn seit 30 Tagen
kein Mitglied mehr geändert wurde (Gruppierung über UUID-Präfix, eine aktive
Gruppe verliert also nie einzelne Toggles). Die Funktion ist angelegt, aber
**nicht** über pg_cron geplant — ein lokaler Stack löscht keine Daten. Die
öffentliche Instanz schaltet sie mit einem `cron.schedule`-Aufruf scharf; das
ist in der README dokumentiert und durch einen Test abgesichert.

Zeilen mit `NULL`-Zeitstempeln (Bestand von vor den neuen Spalten) werden nie
eingesammelt.

### Offen für das Deployment

`db/init.sql` läuft nur bei leerem Datenverzeichnis. Eine **bestehende**
Instanz braucht die alten Cronjobs per `cron.unschedule` weg und die neuen
Statements von Hand — Ablauf steht in der README unter "Upgrading an existing
database". Das ist beim Aufsetzen von `yaft.tehwolf.de` (Phase 2) zu tun.

Ergebnis: Backend und Library flippen zur selben Uhrzeit, das Backend nimmt
keine Werte mehr an, die eine Library still als "aus" liest, und die
Voraussetzungen für die öffentliche Instanz sind erfüllt.

## Phase 2 – Mini-App `yaft-playground`

Ziel: Nachweis, dass Decorator + Provider mit dem laufenden Go-Backend
funktionieren, nicht nur gegen Mocks. Die App wird von Claude aus der Matrix
generiert; die Matrix selbst kommt aus Phase 0.

### Aufbau

- Repo `TypeScript/yaft-playground`, Angular (Nx wie die anderen Repos),
  Jest für Unit-Tests, Playwright für E2E.
- `docker-compose.yml` zieht `ghcr.io/tehw0lf/yaft` und `yaft-db`.
- Seed-Skript legt per `POST /features` eine Feature-Gruppe an und schreibt
  UUID und Secret in eine `.env` für die App.
- Eine Seite pro Provider (`LocalStorageBoolean`, `LocalStorageFeature`,
  `ApiServiceBoolean`, `ApiServiceFeature`), darauf pro Matrixzeile eine
  Komponente, die das Ergebnis sichtbar rendert.

### Matrix

- Decorator-Ziel: Klasse, Methode
- Fallback: keiner, Fallback-Klasse, Fallback-Methode
- Provider: die vier oben
- Zeitlogik: kein Datum, `activeAt` Zukunft/Vergangenheit, `disabledAt`
  Zukunft/Vergangenheit, Fenster
- Fehlerfälle: Feature fehlt, Provider nicht initialisiert, Backend nicht
  erreichbar, Collection-Hash ändert sich nach Update im Backend

### CI

- Workflows über `tehw0lf/workflows` (`setup-workflows`-Skill), Backend als
  Service-Container im E2E-Job.
- Läuft zusätzlich als Downstream-Check in der CI von `yaft-ts`, damit ein
  Library-Release nicht ohne Backend-Kompatibilitätstest passiert.

### ARM-Deployment auf OCI (`yaft.tehwolf.de`)

Die OCI-Instanz ist dauerhaft erreichbar. Traefik (mit DNS-Challenge für
Zertifikate) und Watchtower laufen dort bereits und nehmen neue Container
allein über Labels auf. Es ist keine Infrastruktur-Änderung nötig, nur eine
Compose-Datei:

1. `Docker/tehwolf.de/yaft/docker-compose.yml` anlegen, analog zu
   `flowdive/`: Service `yaft` aus `ghcr.io/tehw0lf/yaft:latest` im
   externen Netz `www`, mit denselben Labels wie die anderen Services
   (Traefik: Host-Regel auf `yaft.tehwolf.de`, `websecure`,
   `mydnschallenge`, `hsts@docker`, Port 8080; Watchtower: `enable=true`).
2. Service `yaft-db` aus `ghcr.io/tehw0lf/yaft-db:latest` in einem eigenen
   internen Netz, nicht in `www`, mit benanntem Volume für die Daten.
   Zugangsdaten über `.env` (gitignored), `DB_DSN` für die App daraus.
3. DNS-Eintrag `yaft.tehwolf.de` auf die Instanz; das Zertifikat holt
   Traefik über die vorhandene DNS-Challenge von selbst.
4. Voraussetzung prüfen: Die GHCR-Images müssen `linux/arm64` enthalten.
   Die Workflows bauen multi-arch, das Manifest einmal mit
   `docker manifest inspect ghcr.io/tehw0lf/yaft:latest` bestätigen.
5. `docker compose -f yaft/docker-compose.yml up -d` auf der Instanz.
6. Playwright-Lauf des Playgrounds mit `API_URL=https://yaft.tehwolf.de`,
   auch als optionaler CI-Job, weil die Instanz dauerhaft läuft.

### Schutz der öffentlichen API

`POST /features` ohne UUID ist unauthentifiziert, weil genau dieser Aufruf
das Secret erzeugt. Authentifizierung darauf ist keine Option, also wird der
Missbrauch in Schichten begrenzt:

1. **Cloudflare** steht bereits als Proxy vor tehwolf.de und übernimmt
   volumetrische Angriffe. Dafür ist auf der Instanz nichts zu tun.
2. **Traefik-Ratelimit per Label** auf dem `yaft`-Service, zwei Router auf
   denselben Container:
   - `yaft-write`: Regel `Host` und `Method(POST, PUT, DELETE)`, Limit etwa
     5 Anfragen pro Minute, Burst 5.
   - `yaft-read`: Regel `Host`, Limit etwa 20 Anfragen pro Sekunde.
   - Beide Middlewares mit `sourceCriterion.requestHeaderName=CF-Connecting-IP`.
     Ohne das zählt Traefik auf die Cloudflare-Edge-IPs und drosselt alle
     Nutzer gemeinsam, weil Traefik hier keine `forwardedHeaders`
     konfiguriert hat.
   - Buffering-Middleware mit `maxRequestBodyBytes` von 4096; ein Toggle ist
     nie größer.
3. **Backend-Grenzen** aus Phase 1: Value-Validierung, Key-Längenlimit,
   30-Tage-Aufräumjob. ✅ Im Backend vorhanden (0.1.3). Beim Aufsetzen noch zu
   tun: den Aufräumjob auf der Instanz scharfschalten (`cron.schedule`, siehe
   README) — er ist absichtlich nicht vorgeplant — und bei einer bereits
   laufenden Datenbank die alten `CURRENT_DATE`-Cronjobs ersetzen.
4. **Datenbank nicht im `www`-Netz**, siehe Schritt 2 oben.

Optionale Härtung, falls der Header-Schlüssel nicht reichen soll: Die
OCI-Security-List auf Port 443 nur für Cloudflare-IP-Bereiche öffnen. Dann
kann niemand an Cloudflare vorbei `CF-Connecting-IP` fälschen. Betrifft die
ganze Instanz und ist deshalb eine eigene Entscheidung.

Weiterer Hinweis:

- Der laufende Watchtower zieht `:latest` automatisch, sobald das Label
  gesetzt ist. Ein kaputtes `main` landet damit direkt auf der Instanz; das Playground-E2E in der yaft-CI ist deshalb
  Pflicht, nicht optional.

Ergebnis: reproduzierbare Kompatibilitätssuite Library ↔ Backend, lokal auf
amd64 und öffentlich auf arm64.

## Phase 3 – Ports in weitere Sprachen

Ziel: `@tehw0lf/yaft` in Sprachen mit Decorator-/Annotation-Unterstützung
übertragen. Referenz ist die TS-Implementierung in `TypeScript/yaft/src/`,
Abnahme ist `yaft-conformance`.

Reihenfolge:

1. **yaft-java** – explizit gewünscht, Annotationen sind etabliert.
2. **yaft-go** – eigenes Repo, kein Paket in diesem Backend-Repo. Kein
   natives Decorator-Pattern, deshalb Funktions-Wrapper oder Codegenerierung
   (siehe "Sprachen ohne Decorators"). Sinnvoll, weil das Backend Go ist und
   ein Go-Client die Lücke zwischen Server und Nutzer schließt.
3. Weitere Kandidaten nach Bedarf: Python, C#, Kotlin.

Für jeden Port gilt:

- Eigenes Repo `yaft-<sprache>`, gleiche Lizenz, gleiches Logo, README nach
  Vorlage von `yaft-ts`.
- Core (Modell, Provider-Interface, Decorator, Zeitlogik) und ein
  Memory-/Local-Provider zuerst, der API-Provider erst danach.
- Konformitäts-Suite per `conformance.lock` gepinnt, Adapter in der CI, grün
  vor dem ersten Release.
- CI über `tehw0lf/workflows`, Publish ins jeweilige Registry
  (Maven Central / pkg.go.dev / PyPI / NuGet).

Ergebnis: pro Sprache ein veröffentlichtes Paket, das dieselbe Suite besteht
wie die TS-Referenz.

---

## Port-Vorgaben

Übernommen aus dem gelöschten `YAFT_PORTING_PROMPT.md` und gegen
`TypeScript/yaft/src/FeatureToggle.ts` sowie `Go/YaFT/main.go` geprüft. Die
Stellen, an denen der Prompt von der Implementierung abwich, sind hier
korrigiert; sie sind unter "Korrekturen gegenüber dem alten Prompt" am Ende
aufgeführt, damit ein früherer Leser des Prompts sie wiederfindet.

Mit Phase 0 wandern die normativen Regeln nummeriert nach
`yaft-conformance/SPEC.md`; dieser Abschnitt verweist dann nur noch dorthin.

### Datenmodell

```
Feature {
  key:        string
  value:      string    // "true" oder "false", als String
  activeAt:   string    // RFC 3339 mit Offset, oder leer
  disabledAt: string    // RFC 3339 mit Offset, oder leer
  tags:       string[]  // optional
}
```

`value` ist bewusst ein String, kein Boolean — das Backend speichert ihn so.
Nur exakt `"true"` aktiviert.

### Zeitlogik (normativ)

Die Auswertung ist der Kern jedes Ports. Die Uhr **muss** injizierbar sein
(Default: Systemzeit), sonst kann der Konformitäts-Adapter das `now` aus den
Fall-Dateien nicht setzen.

```
function isEnabled(key, now):
    feature = data[key]
    if feature is null or undefined:        return false
    if feature.value != "true":             return false

    if feature.activeAt is set and non-empty:
        t = parseRFC3339(feature.activeAt)
        if t is valid and now < t:          return false

    if feature.disabledAt is set and non-empty:
        t = parseRFC3339(feature.disabledAt)
        if t is valid and now >= t:         return false

    return true
```

Regeln im Einzelnen:

- Nur exakt `"true"` aktiviert. `"TRUE"`, `"1"`, `""` und ein fehlender Wert
  ergeben `false`.
- Fehlender Key und `null`-Feature ergeben `false`.
- `activeAt`/`disabledAt` leer, `null` oder ungültig werden ignoriert, nicht
  als Fehler behandelt.
- Grenzen: `now == activeAt` ergibt `true` (Vergleich ist `now < activeAt`),
  `now == disabledAt` ergibt `false` (Vergleich ist `now >= disabledAt`).
- `activeAt > disabledAt` ist kein Sonderfall und wird normal ausgewertet.
- **Nur RFC 3339 mit Offset ist gültig.** Reine Datumsangaben (`2026-09-18`)
  und Formate ohne Offset gelten als ungültig und werden ignoriert, weil
  Sprachen sie unterschiedlich parsen (JS: UTC-Mitternacht, andere: lokal).
  Der alte Prompt ließ hier alles zu, was die jeweilige Sprache parsen kann —
  das ist nicht portierbar.
- Ein ungültiges Datum sollte eine Warnung loggen, aber nie werfen.

### Decorator-Semantik (normativ)

**Auswertungszeitpunkt** — beobachtbares Verhalten, deshalb festgeschrieben:

| Ziel | Wann ausgewertet |
|---|---|
| Klasse | **einmal** beim Laden/Dekorieren der Klasse |
| Methode | bei **jedem** Aufruf |

Ein nach dem Laden geänderter Toggle beeinflusst also die Klasse nicht mehr,
die Methode aber sehr wohl. Die Konformitäts-Suite prüft genau das.

**Provider nicht gesetzt**: Fehler beim Dekorieren, nicht erst beim Aufruf.
In der TS-Referenz wirft `FeatureToggle()` sofort `"FeatureToggleProvider not
set"`, bevor die innere Funktion zurückgegeben wird.

**Fallback-Verhalten**, vier Fälle:

| Ziel | Fallback | Toggle an | Toggle aus |
|---|---|---|---|
| Methode | keiner | Original-Methode | "nichts" (`undefined`/`null`/`None`/Zero Value) |
| Methode | Methode | Original-Methode | Fallback, **gleiche Argumente, gleicher Empfänger** |
| Klasse | keiner | Original-Klasse | Leer-Hülle: alle Methoden liefern "nichts" |
| Klasse | Klasse | Original-Klasse | Fallback-Klasse |

Asynchrone Methoden der Leer-Hülle liefern ein bereits aufgelöstes Promise
(bzw. das Äquivalent der Zielsprache), keinen Null-Wert — sonst bricht ein
`await` beim Aufrufer.

### Provider-Pattern

```
FeatureProvider<T> {
  data: Record<string, T>
  getConfig(configPathOrUrl): void
  isEnabled(key): boolean

  // nur für API-Provider
  apiUrl?:    string
  baseUUID?:  string
  getCollectionHash?(url): void
}
```

Die TS-Referenz liefert vier **Beispiel**-Provider in `src/examples/`, keine
fertige Provider-Bibliothek. Ein Port bildet dieselben zwei Achsen ab:

| | Quelle: lokal | Quelle: API |
|---|---|---|
| **Feature-Shape** (volles `Feature`-Objekt) | `LocalStorageFeatureProvider` | `ApiServiceFeatureProvider` |
| **Boolean-Shape** (`{"myToggle": true}`) | `LocalStorageBooleanProvider` | `ApiServiceBooleanProvider` |

Der Boolean-Shape bildet direkt auf `isEnabled` ab und kennt keine Zeitlogik.

Minimal lauffähiger Port: Feature-Shape lokal + Decorator. Der API-Provider
kann danach kommen.

Die Zeitlogik gehört **einmal** in den Core (`evaluate(feature, now)`), nicht
in jeden Provider kopiert. In yaft-ts ist das seit 0.0.12 so umgesetzt und
dient als Referenz für die Ports.

### Sprachen mit Decorators/Annotations

**Java:**
```java
@FeatureToggle(key = "new-algorithm", fallback = OldAlgorithm.class)
public class NewAlgorithm implements Algorithm {
    public String process(String input) { /* ... */ }
}

public class ProcessingService {
    @FeatureToggle(key = "enhanced-processing")
    public String enhancedProcess(String input) { /* ... */ }
}
```

**Python:**
```python
@feature_toggle("new-algorithm", fallback=OldAlgorithm)
class NewAlgorithm:
    def process(self, input_data: str) -> str: ...

class ProcessingService:
    @feature_toggle("enhanced-processing")
    def enhanced_process(self, input_data: str) -> str: ...
```

**C#:**
```csharp
[FeatureToggle("new-algorithm", FallbackType = typeof(OldAlgorithm))]
public class NewAlgorithm : IAlgorithm {
    public string Process(string input) { /* ... */ }
}
```

### Sprachen ohne Decorators

**Go** – Funktions-Wrapper oder Codegenerierung:
```go
func NewFeatureToggled(key string, impl, fallback interface{}) interface{} {
    if YaFT.IsEnabled(key) {
        return impl
    }
    return fallback
}
```

**PHP < 8.0** – Trait-basierte Mixins. **Ruby** – Metaprogrammierung.
**C** – Präprozessor-Makros oder Funktionszeiger.

In allen Fällen gilt der Auswertungszeitpunkt aus der Tabelle oben sinngemäß:
Was einer Klasse entspricht, wird einmal ausgewertet; was einem Methodenaufruf
entspricht, bei jedem Aufruf.

### Go-Backend-API (für den API-Provider)

Nur relevant, wenn der Port einen API-Provider implementiert. Geprüft gegen
`Go/YaFT/main.go`.

**Modell:**
```go
type FeatureToggle struct {
    ID         uint           `gorm:"primaryKey"`
    Key        string         `gorm:"unique;not null"`  // UUID|feature_name
    Value      string         `gorm:"not null"`
    ActiveAt   *time.Time     `gorm:"null"`
    DisabledAt *time.Time     `gorm:"null"`
    Secret     string         `gorm:"null"`
    Tags       pq.StringArray `gorm:"type:text[]"`
}
```
`FeatureToggleDTO` ist dasselbe ohne `ID` und ohne `Secret`.

**Endpunkte** — das Secret steht **im URL-Pfad**, nicht in einem
`Authorization`-Header:

| Methode | Pfad | Auth |
|---|---|---|
| `GET` | `/features/:key` | – |
| `GET` | `/collectionHash/:uuid` | – |
| `POST` | `/features` | Secret im JSON-Body (siehe unten) |
| `PUT` | `/features/activate/:key/:secret` | Pfad |
| `PUT` | `/features/activateAt/:key/:date/:secret` | Pfad |
| `PUT` | `/features/deactivate/:key/:secret` | Pfad |
| `PUT` | `/features/deactivateAt/:key/:date/:secret` | Pfad |
| `DELETE` | `/features/:key/:secret` | Pfad |
| `PUT` | `/secret/update/:uuid/:oldsecret/:newsecret` | Pfad |

`GET /features/:key` hat **zwei Antwortformen**:

- Einzel-Toggle (Key existiert exakt): flaches Objekt mit **kleingeschriebenen**
  Feldern — `{"key", "value", "activeAt", "disabledAt", "tags"}`.
- UUID-Gruppe (Key ist nur ein UUID-Präfix): `{"toggles": [...]}` mit
  **großgeschriebenen** Feldern — `Key`, `Value`, `ActiveAt`, `DisabledAt`,
  `Tags` —, weil `FeatureToggleDTO` keine JSON-Tags trägt.

Ein Port muss beide Schreibweisen normalisieren. Optional filtert
`?tags=a,b` die Gruppe (UND-Verknüpfung).

`POST /features` legt an:

- Key **ohne** UUID-Präfix → Backend erzeugt Präfix und Secret und gibt beide
  zurück (`201` mit `secret` im Body). Das ist der einzige Aufruf, der ein
  Secret liefert.
- Key **mit** UUID-Präfix → `Secret` muss im JSON-Body mitgeschickt werden und
  passen, sonst `401`. Die Antwort enthält dann kein Secret.

Weitere Eigenheiten:

- Datumsangaben in `activateAt`/`deactivateAt` werden als **RFC 3339 mit
  Offset** geparst; alles andere lehnt das Backend seit 0.1.3 mit `400` ab
  (vorher wurde es still zur Nullzeit). Backend und Ports haben damit dieselbe
  Formatregel.
- `POST /features` akzeptiert seit 0.1.3 nur `"true"`/`"false"` als `Value` und
  begrenzt `Key` auf 256 Zeichen inklusive UUID-Präfix; beides sonst `400`.
- Nicht gesetzte Daten liefert das Backend als **`null`**, nicht als `""`.
  Ein Port behandelt beide gleichwertig als "nicht gesetzt".
- Secrets sind drei aneinandergehängte UUIDs und müssen URL-tauglich sein,
  weil sie im Pfad stehen (`isURLParseable` im Backend).
- Zeitgesteuertes Flippen macht ein pg_cron-Job, nicht die API — bis zu 60 s
  Verzögerung. Eine Library, die selbst auswertet, ist dem Backend also kurz
  voraus. Das ist gewollt (siehe Entscheidungstabelle). Seit 0.1.3 vergleicht
  der Job gegen `now()`, nicht mehr gegen `CURRENT_DATE`; der Unterschied
  zwischen Backend und Library ist damit die Polling-Verzögerung statt bis zu
  24 Stunden.

### Secret-Handling im Port

- Secrets **nicht** auf Platte schreiben. Herkunft je nach Einsatz:
  CLI → Umgebungsvariable, Web-App → Session/Server-Seite, Tests →
  In-Memory, CI → Secrets-Management.
- Das beim ersten `POST` zurückgegebene Secret im Speicher halten und den
  Aufrufer warnen, dass er es sichern muss — es ist nicht wiederherstellbar.
- **Read-only-Betrieb ist der Normalfall**: `GET` braucht kein Secret. Ein
  Port, der Toggles nur liest, muss überhaupt kein Secret-Handling haben.

### Abnahme pro Port

- [ ] Feature-Modell inkl. `tags`
- [ ] Decorator für Klasse und Methode, Auswertungszeitpunkt wie festgelegt
- [ ] Alle vier Fallback-Fälle
- [ ] Zeitlogik zentral im Core, Uhr injizierbar
- [ ] Lokaler Provider in Feature- und Boolean-Shape
- [ ] API-Provider (optional) inkl. Normalisierung beider Antwortformen
- [ ] `conformance.lock` gepinnt, Adapter läuft in der CI, Suite grün
- [ ] README nach Vorlage `yaft-ts`, mit der Regel zum Auswertungszeitpunkt
- [ ] CI über `tehw0lf/workflows`, Publish ins Sprach-Registry

### Korrekturen gegenüber dem alten Prompt

Für alle, die den gelöschten Prompt kennen — diese Angaben darin waren falsch:

| Prompt behauptete | Tatsächlich |
|---|---|
| `PUT /features/{key}` + `DELETE /features/{key}`, `Authorization: Bearer` | Secret im Pfad, getrennte Routen je Operation (Tabelle oben) |
| `GET /features` | `GET /features/:key`, zwei Antwortformen |
| `MemoryProvider`, `FileProvider`, `EnvironmentProvider`, `ApiProvider` | vier Beispiel-Provider über die Achsen lokal/API × Feature/Boolean |
| Verzeichnisbaum mit `core/`, `utils/HttpClient`, `TimeBasedEvaluator`, `ConfigLoader` | existiert nicht; TS hat `FeatureToggle.ts` + `examples/` |
| Decorator wertet für Klasse **und** Methode einmal beim Laden aus | Klasse einmal, Methode bei jedem Aufruf |
| `Tags` fehlte im Go-Modell | `Tags pq.StringArray` in Struct und DTO |
| Boolean-Provider-Shape kam nicht vor | zwei der vier Beispiel-Provider nutzen ihn |
| beliebige parsebare Datumsformate, statische Testfälle "as of 2025" | nur RFC 3339 mit Offset; Fälle bringen ihr eigenes `now` mit |

## Offene Fragen

Keine. Alle Entscheidungen stehen in der Tabelle oben; die einzige noch
nicht getroffene Wahl ist die optionale OCI-Security-List-Härtung, die die
ganze Instanz betrifft und nicht diesen Plan blockiert.

## Abhängigkeiten

```
Phase 0 (Suite, yaft-ts grün) ──► Phase 2 (Playground aus Matrix) ──► Phase 3 (Ports, Abnahme gegen Suite)
Phase 1 (Backend now()) ✅    ──► Phase 2 (API-Fälle mit Uhrzeit)
```

Phase 1 ist erledigt und blockiert nichts mehr. **Phase 0 ist damit der
kritische Pfad**: Phase 2 braucht die Matrix daraus, Phase 3 die Abnahme.

Phase 3 kann für den Core-Teil (ohne ApiProvider) parallel zu Phase 2
beginnen; `yaft-conformance@v1.1.0` ist getaggt, die Abnahme steht also bereit.
