# YaFT – Der nächste Port

Stand: 2026-10-01. TS, Java, Go und Python bestehen die ganze Suite 5.0.0 und
sind veröffentlicht. Es ist nichts offen.

Dieses Dokument ist das Nachschlagewerk für den Fall, dass ein weiterer Port
dazukommt: der Stand des YaFT-Ökosystems, die feststehenden Entscheidungen
und was ein Port mitbringen muss. Die frühere Roadmap (`TODO.md`, Phasen 0–3,
Befunde, Review-Protokolle) steht im Git-Verlauf.

**Nicht hier:** Der Betrieb von `yaft.tehwolf.de` (Compose-Stack, Traefik,
Ratelimits, Cloudflare, OCI, OpenTofu) gehört zu `Docker/tehwolf.de` und wird
dort geplant.

## Stand

| Repo | Version | Stand |
|---|---|---|
| `Go/YaFT` | 0.3.16 | Images publiziert, OpenAPI-Spec, deployt; leere Gruppe antwortet `200`; `/collectionHash` nur für Gruppen |
| `yaft-conformance` | 5.0.0 | 34 Regeln, 119 Fälle; zuletzt R33 (Key und Value nur als String, keine Umwandlung), R30–R32 (Refresh ersetzt ganz oder scheitert, leere Gruppe `200`, Refresh meldet sein Ergebnis); Mapping-Format 4 mit `held`/`rejected`/`retry` |
| `TypeScript/yaft` | 0.0.22 | auf npm, Suite 5.0.0 inkl. R32 (`refresh()`) |
| `TypeScript/yaft-playground` | 0.1.1 | CI grün inkl. 8 E2E gegen echtes Backend; 8/8 auch gegen `yaft.tehwolf.de` |
| `Java/yaft-java` | 0.2.7 | auf Maven Central (`de.tehwolf:yaft`), Suite 5.0.0 inkl. R32, API-Provider |
| `Java/yaft-java-playground` | 0.1.3 | Spring Boot 4.1.1 mit yaft 0.2.6 gegen echtes Backend, CI grün |
| `Go/yaft-go` | 0.1.3 | `go get github.com/tehw0lf/yaft-go`, Suite 5.0.0 inkl. R32, API-Provider, `yaft-shell` |
| `Python/yaft-python` | 0.2.0 | auf PyPI (`yaft`), Suite 5.0.0 inkl. R32, Local- und API-Provider, Decorator |
| `workflows` | – | `java_version` (#163), Maven-Central-Publish (#164), `tool: go` (#165) |

## Wo was steht

- **Normative Regeln:** `yaft-conformance/SPEC.md` (R1–R33). Wo dieses
  Dokument und die SPEC abweichen, gilt die SPEC.
- **Backend-API:** `openapi.yaml` in diesem Repo. `TestOpenAPIVersionMatchesVERSION`
  verlangt, dass `info.version` und `VERSION` gleich sind, auch bei reinen
  Docs-PRs. Vor jedem Commit die Docker-Validierung aus `CLAUDE.md` laufen
  lassen.
- **Bestehende Datenbank aktualisieren:** README, „Upgrading an existing
  database“.

## Entscheidungen

Die Punkte mit „nicht wieder aufmachen“ wurden schon einmal diskutiert. Schlägt
ein Review sie wieder vor, mit dem genannten Grund zurückweisen.

| Thema | Entscheidung |
|---|---|
| Ports (2026-09-29, nicht wieder aufmachen) | Ein Port ist minimal und nutzt die Mittel seiner Sprache. Er ahmt die Referenz nicht nach, wo die Spezifikation nichts verlangt (Anlass: yaft-python hatte JavaScripts `String()` und den Bereich von `Date.parse` nachgebaut). Wo zwei Ports nur zufällig übereinstimmen, gehört eine Regel in die Suite, nicht Nachahmung in den Port. |
| `workflow_dispatch` (2026-09-29, nicht wieder aufmachen) | Nicht in den `build.yml` der Ports. Ein verlorenes Push-Event wird mit der nächsten Version nachgeholt (so geschehen bei yaft-java 0.2.5, das es deshalb nicht gibt). |
| Reusable Workflows (nicht wieder aufmachen) | Die Caller bleiben auf `tehw0lf/workflows@main`, kein SHA-Pinning: Es sind die eigenen Workflows, `main` ist per Ruleset geschützt. Build- und Publish-Rechte sind schon getrennt, weil jeder Sub-Job nur seine Permissions und Secrets bekommt. |
| GHCR-Images | bleiben privat. Die Playground-CI baut die Images deshalb aus dem öffentlichen Repo, statt sie zu ziehen. |
| `main` ist Produktion | Watchtower zieht `:latest` auf die Instanz. Ein kaputtes `main` ist sofort live; das Playground-E2E in der CI ist deshalb Pflicht. |
| Auswertung von `activeAt`/`disabledAt` | Die Library prüft selbst; der Backend-Cron vergleicht gegen `now()`. Unterschied ist nur die Cron-Verzögerung (bis 60 s). |
| Datumsformat | nur RFC 3339 mit Offset; alles andere gilt als ungültig und wird ignoriert |
| Auswertungszeitpunkt Decorator | Klasse einmal beim Laden, Methode bei jedem Aufruf (R14/R15) |
| Wertvalidierung Backend | `POST /features` lehnt alles außer `"true"`/`"false"` mit `400` ab; Key höchstens 256 Zeichen inkl. UUID-Präfix |
| Retention | `cleanup_stale_feature_toggles()` existiert, ist aber nicht vorgeplant; nur die öffentliche Instanz schaltet ihn scharf |
| Repos | Suite in `yaft-conformance`, Go-Port in `yaft-go`; dieses Repo bleibt reines Backend |
| Suite-Verteilung | Release-Download, gepinnt über Version und Prüfsumme in `conformance.lock`; keine Submodule |
| Beispiel-Apps | pro Port ein `yaft-<sprache>-playground` gegen das publizierte Artefakt und das echte Backend, erst nach API-Provider und Publish |
| Java | JDK-Dynamic-Proxy über Interfaces, Kern ohne Abhängigkeiten; Gradle (Kotlin DSL), Java 25; Koordinaten `de.tehwolf:yaft` |

## Weitere Ports

Kandidaten nach Bedarf: Rust, PHP. Kotlin nutzt yaft-java direkt (höchstens
ein Beispiel im Java-Playground). C# erst mit einem Abnehmer, denn
`tehw0lf/workflows` hat keinen NuGet-Publish.

### Für jeden Port gilt

- Eigenes Repo `yaft-<sprache>`, gleiche Lizenz, gleiches Logo, README nach
  Vorlage von `yaft-ts`, mit der Regel zum Auswertungszeitpunkt.
- Zuerst Kern (Modell, Provider-Interface, Decorator, Zeitlogik zentral in
  einer `evaluate(feature, now)` mit injizierbarer Uhr) und ein lokaler
  Provider in Feature- und Boolean-Shape. Der API-Provider kommt danach.
- Suite per `conformance.lock` gepinnt, Adapter in der CI, grün vor dem ersten
  Release. Gegen eingebaute Fehler gegenprüfen, dass die Fälle wirklich
  greifen.
- CI über `tehw0lf/workflows`, Publish ins Registry der Sprache.

### Abnahme pro Port

- [ ] Feature-Modell inkl. `tags`
- [ ] Decorator für Klasse und Methode, Auswertungszeitpunkt wie festgelegt
- [ ] Alle vier Fallback-Fälle, inkl. Leer-Hülle
- [ ] Zeitlogik zentral im Kern, Uhr injizierbar
- [ ] Lokaler Provider in Feature- und Boolean-Shape
- [ ] API-Provider (optional): sucht den Namen auch innerhalb der Gruppe, `refresh` meldet sein Ergebnis (R32)
- [ ] `conformance.lock` gepinnt, Adapter läuft in der CI, Suite grün
- [ ] Rauchtest gegen `https://yaft.tehwolf.de` vor dem ersten Release
- [ ] README, CI über `tehw0lf/workflows`, Publish ins Registry

### Fallstricke aus den bisherigen Ports

- **Nachsichtige Datumsparser.** `Date.parse` rollt ein unmögliches Datum
  weiter (`2027-02-30` wird zum 2. März). Die Komponenten vor dem Parsen auf
  gültige Bereiche prüfen, Schaltjahre inklusive. Im Test muss das unmögliche
  Datum **in der Zukunft** liegen, sonst besteht der Test aus dem falschen
  Grund.
- **Annotation-Werte sind Konstanten, Backend-Keys tragen die Gruppen-UUID.**
  Ein API-Provider, der nur den vollen Key nachschlägt, schaltet jeden
  annotierten Toggle aus. Er muss auch den Namen innerhalb der Gruppe finden.
- **Ein Body, der keine Gruppe ist** (`null`, `[]`, Fehlerseite eines Proxys,
  `{"toggles": [null]}`), darf die Daten nicht ersetzen. Der Refresh scheitert,
  die alten Daten bleiben, und der Hash wird erst nach Erfolg gemerkt (R30).
  Betraf alle ersten drei Ports.
- **Proxy-basierte Ports (Java):** Das `accessible`-Flag gehört zum einzelnen
  `Method`-Objekt; der Proxy übergibt eine andere Kopie als `getMethods()`.
  Eine schon von einem Framework geproxte Instanz muss beim Start scheitern,
  nicht still ignoriert werden.
- **Cloudflare vor `yaft.tehwolf.de` blockt den User-Agent `Python-urllib/*`**
  mit `403`. Erst der Rauchtest gegen das Live-Backend fand das. Jeder Port
  sendet einen eigenen UA (`yaft-<sprache>/<version>`).
- **Publish:** Ein PyPI Trusted Publisher kann keinen fremden Reusable Workflow
  nennen; yaft-python hat deshalb eine eigene `publish.yml`. Maven Central
  löscht nie, und jeder Merge mit neuer Version veröffentlicht; zum Prüfen
  ohne Upload `maven_central_dry_run: true`. Signaturschlüssel `tehw0lf`,
  Fingerprint `2A0351C28EB122B8946E52E39A12B17723327580`, Backup in KeePass.
- **Secrets:** nie auf Platte schreiben. Das beim ersten `POST` gelieferte
  Secret ist nicht wiederherstellbar; der Aufrufer muss gewarnt werden. Ein
  Port, der nur liest, braucht überhaupt kein Secret-Handling.
