# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Ein Discord-Bot, der es Personen, die verschiedene Sprachen sprechen, ermöglicht, zusammen im selben Server zu chatten.

Verknüpfe je einen Channel pro Sprache zu einer **Übersetzungsgruppe**. Jede Nachricht, die in einem Channel gepostet wird, wird sofort von `google.gemma-4-26b-a4b` über Amazon Bedrock übersetzt und in alle anderen Channels der Gruppe gespiegelt — mit dem Namen und Avatar des Originalsenders — sodass sich jeder Channel wie eine natürliche Unterhaltung in der jeweiligen Sprache liest.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-de (Deutsch)
```

## Funktionen

- **Alles bleibt synchronisiert** — nicht nur neue Nachrichten: Bearbeitungen, Löschungen, Antworten, weitergeleitete Nachrichten, Reaktionen, Pins, Threads (Text- / Forum- / Medien-Channel) und Nachrichten, die nur Anhänge enthalten, werden alle in der Gruppe gespiegelt.
- **Nachrichten sehen aus, als kämen sie vom Absender** — gespiegelte Nachrichten werden über Webhooks mit dem Namen und Avatar des ursprünglichen Autors gesendet.
- **Natürliche Übersetzungen** — Gemma 4 26B-A4B verwendet Channel-Name, Thema und den jüngsten Gesprächsverlauf als Kontext; ein serverseitiges Glossar erlaubt es, bevorzugte Übersetzungen für Namen und Fachbegriffe festzulegen.
- **Intelligente Link-Behandlung** — Links und Erwähnungen, die auf verwaltete Channel oder Nachrichten zeigen, werden in die jeweiligen Entsprechungen jeder Sprache umgeschrieben; URLs mit `hreflang`-Alternativen werden durch die Zielsprachversion ersetzt.
- **Effizient und sicher** — Nachrichten ohne übersetzbaren Text (URLs, Erwähnungen, benutzerdefinierte Emojis, Code) werden gespiegelt, ohne die Übersetzungs-API aufzurufen; pro Server gelten Token-Ratenlimits; URLs, Erwähnungen und Code-Blöcke sind gegen Prompt-Injection geschützt. Bei Übersetzungsfehlern fail-closed (kein Spiegeln, lokalisierte Benachrichtigung im Quell-Channel).
- **Lokalisierte Benutzeroberfläche** — Befehlsantworten richten sich nach der Discord-Client-Sprache des Nutzers, Channel-Benachrichtigungen verwenden die für den Channel konfigurierte Sprache (13 Sprachen, Englisch als Fallback).

## Voraussetzungen

- Go 1.24 oder neuer
- Ein Discord-Bot-Konto mit aktiviertem privilegierten Intent `MESSAGE CONTENT`
- Ein AWS-Konto mit Zugriff auf Amazon Bedrock und ein IAM-Zugriffsschlüssel, der Inferenz im Mantle-Standardprojekt in `your-aws-bedrock-region` erstellen darf.

## Einrichtung

### 1. Discord-Bot vorbereiten

1. Erstelle eine Anwendung im [Discord Developer Portal](https://discord.com/developers/applications)
2. Auf der **Bot**-Seite:
   - Aktiviere den `MESSAGE CONTENT INTENT` (erforderlich)
   - Kopiere den Bot-Token
3. Lade den Bot über **OAuth2 → URL Generator** auf deinen Server ein:
   - Scopes: `bot`, `applications.commands`
   - Berechtigungen (wie im Developer Portal angezeigt):
     - **Allgemein**: `View Channel`, `Read Message History`
     - **Nachrichten**: `Send Messages`, `Send Messages in Threads`
     - **Moderation**: `Pin Messages`
     - **Webhooks**: `Manage Webhooks`
     - **Threads**: `Create Public Threads`, `Manage Threads`
     - **Reaktionen**: `Add Reactions`
   - Der Berechtigungsganzzahlwert für das Obige ist `2252126768139328`
   - Um auch benutzerdefinierte Emoji-Reaktionen von anderen Servern zu synchronisieren, erteile zusätzlich `Use External Emojis`; der Berechtigungsganzzahlwert wird dann `2252126768401472`

### 2. Amazon Bedrock einrichten

Aktiviere `google.gemma-4-26b-a4b` in Amazon Bedrock in `your-aws-bedrock-region`. Erstelle einen IAM-Benutzer mit ausschließlich `bedrock-mantle:CreateInference` für das Modell und setze `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_BEDROCK_REGION` und `AWS_BEDROCK_PROJECT_ID` in `.env`. Modell, 30-Sekunden-Timeout und 4096-Token-Ausgabelimit sind im Code festgelegt; Region und Project ID sind erforderliche lokale Deployment-Einstellungen.

### 3. Umgebungsvariablen konfigurieren

```sh
cp .env.example .env
```

Bearbeite `.env` und setze folgende Werte:

```env
DISCORD_TOKEN=your-discord-bot-token
AWS_ACCESS_KEY_ID=your-aws-access-key-id
AWS_SECRET_ACCESS_KEY=your-aws-secret-access-key
AWS_BEDROCK_REGION=your-aws-bedrock-region
AWS_BEDROCK_PROJECT_ID=your-aws-bedrock-project-id
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
# MESSAGE_LINK_RETENTION_DAYS=60
# GUILD_DATA_RETENTION_DAYS=30
```

| Variable | Erforderlich | Beschreibung |
|---|---|---|
| `DISCORD_TOKEN` | Ja | Discord-Bot-Token |
| `AWS_ACCESS_KEY_ID` | Yes | Access key ID for the dedicated Bedrock IAM user |
| `AWS_SECRET_ACCESS_KEY` | Yes | Secret access key for the dedicated Bedrock IAM user |
| `AWS_BEDROCK_REGION` | Yes | Bedrock Mantle region, such as `your-aws-bedrock-region` |
| `AWS_BEDROCK_PROJECT_ID` | Yes | Bedrock Mantle Project ID, such as `your-aws-bedrock-project-id` |
| `DB_PATH` | Nein | Pfad zur SQLite-Datei (Standard: `./translator.db`) |
| `HTTP_ADDR` | Nein | Adresse des Avatar-Badge-Servers (Standard: `:8080`) |
| `PUBLIC_BASE_URL` | Nein | Öffentliche Basis-URL für Avatar-Ring-Badges. Wenn nicht gesetzt, verwenden gespiegelte Nachrichten die ursprüngliche Discord-Avatar-URL und der Badge-Server wird nicht genutzt |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | Nein | Gemma 4 26B-A4B-Token-Limit pro Server und Minute (Standard: `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | Nein | Anfrage-Limit pro IP und Minute für den `/avatar`-Badge-Endpunkt (Standard: `120`) |
| `MESSAGE_LINK_RETENTION_DAYS` | Nein | Aufbewahrungsdauer von `message_links` in SQLite in Tagen vor automatischer Bereinigung. `0` (Standard) deaktiviert die Bereinigung; z. B. `60` löscht Links älter als 60 Tage beim Start und alle 24 Stunden |
| `GUILD_DATA_RETENTION_DAYS` | Nein | Tage, die SQLite-Daten eines Servers nach Entfernung des Bots aufbewahrt werden. `0` (Standard) deaktiviert die Bereinigung; z. B. `30` löscht Daten von seit mehr als 30 Tagen entfernten Servern beim Start und alle 24 Stunden. Ein erneuter Beitritt vor Ablauf hebt die geplante Löschung auf |

### Amazon Bedrock — Betriebsvereinbarung

Die Übersetzung verwendet die nicht streamende Mantle Responses API mit `google.gemma-4-26b-a4b` in `your-aws-bedrock-region`; jede Anfrage wird über den Header `OpenAI-Project` dem Project `your-aws-bedrock-project-id` zugeordnet: Timeout **30 s**, **provider-default temperature 1.0**, **max_output_tokens 4096** und schema-instruiertes JSON mit strikter Bot-Validierung. Alle Sprachen werden in einer Anfrage erzeugt. Beim 4K-Limit, anormalem Stopp oder ungültigem JSON schlägt alles fail-closed fehl; es gibt keine Wiederholungen, Aufteilung oder Provider-Fallbacks. Der Bot protokolliert keine Prompts, Antworten oder Zugangsdaten. Das GCE-Deployment prüft Anmeldedaten, Modellzugriff und Antwortvertrag vor dem Austausch mit `--bedrock-prewarm` und einem Fünf-Minuten-Limit.

### 4. Starten

```sh
go run ./cmd/discord-auto-translator
```

Oder kompilieren und ausführen:

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## Verwendung

Nach dem Start des Bots werden Slash-Befehle in jedem Server registriert.

### Channel einrichten

#### Eine Übersetzungsgruppe erstellen

Führe `/new-channel` im japanischen Channel aus, um eine Übersetzungsgruppe zu erstellen:

```
/new-channel language:ja
```

#### Channels in anderen Sprachen hinzufügen

Führe `/join-channel` im englischen Channel aus, um ihn zur Gruppe hinzuzufügen:

```
/join-channel group:general language:en
```

Um auch einen deutschen Channel hinzuzufügen:

```
/join-channel group:general language:de
```

Jetzt sind `#general-ja`, `#general-en` und `#general-de` verknüpft.

### Befehle

Standardmäßig können die Admin-Slash-Befehle nur von **Server-Administratoren** ausgeführt werden. Um weiteren Rollen den Zugriff zu erlauben, gehe in den Discord-„Servereinstellungen" → „Integrationen" → „Verwalten" des Bots → „Befehlsberechtigungen" und konfiguriere den Zugriff global oder pro Befehl. Der Bot ändert niemals selbstständig Rollen oder Befehlsberechtigungen.

| Befehl | Beschreibung |
|---|---|
| `/new-channel language:[Sprache] channel:<Channel> group:<Gruppe>` | Neue Übersetzungsgruppe erstellen. `channel` ist standardmäßig der aktuelle Channel; `group` ist standardmäßig der Channel-Name |
| `/join-channel group:[Gruppe] language:[Sprache] channel:<Channel>` | Channel zu einer Gruppe hinzufügen. `channel` ist standardmäßig der aktuelle Channel |
| `/leave-channel group:[Gruppe] channel:<Channel>` | Channel aus einer Gruppe entfernen. `channel` ist standardmäßig der aktuelle Channel |
| `/delete-group group:[Gruppe]` | Gesamte Gruppe löschen |
| `/list-groups` | Übersetzungsgruppen und Channels dieses Servers anzeigen |
| `/add-glossary term:[Begriff] translation:[Übersetzung] attribute:<Attribut> always_include:<Bool>` | Bevorzugte Übersetzung im Server-Glossar registrieren. `attribute` ist Freitext mit Vorschlägen; `always_include` ist standardmäßig `false` |
| `/list-glossary` | Glossar des Servers anzeigen |
| `/remove-glossary term:[Begriff]` | Glossareintrag entfernen |
| `/set-style group:[Gruppe] preset:<Voreinstellung> custom:<eigene Anweisung>` | Übersetzungsstil für eine Gruppe festlegen. `preset` oder `custom` angeben, nicht beides |
| `/bot-whitelist add source_type:[bot\|webhook] source_id:[ID]` | Eine automatisierte Nachrichtenquelle auf diesem Server zulassen. Bei `source_type:bot` ist `source_id` die Bot-Benutzer-ID, bei `source_type:webhook` die Webhook-ID |
| `/bot-whitelist remove source_type:[bot\|webhook] source_id:[ID]` | Die entsprechende automatisierte Nachrichtenquelle aus der Zulassungsliste dieses Servers entfernen |
| `/bot-whitelist list` | Die auf diesem Server zugelassenen Bot- und Webhook-Quellen anzeigen |

- Quellen-Zulassungslisten werden in SQLite gespeichert und gelten jeweils nur für einen Discord-Server (eine Guild). Vom Übersetzer verwaltete Ausgabe-Webhooks und Nachrichten dieses Übersetzungsbots selbst bleiben ausgeschlossen, auch wenn ihre IDs hinzugefügt werden

- `language` verwendet BCP-47-Codes (`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr` usw.)
- Maximal 50 Glossareinträge pro Server
- `attribute` schlägt „Personenname", „Ortsname", „Slang", „Abkürzung" und „Fachbegriff" vor, aber jeder Wert kann frei eingegeben werden. Das Attribut wird als Kontext genutzt, damit Gemma 4 26B-A4B die Bedeutung des Begriffs versteht
- Normale Begriffe werden den Systemanweisungen nur hinzugefügt, wenn die zu übersetzende Nachricht `term` enthält (Groß-/Kleinschreibung ignoriert). Begriffe mit `always_include:true` werden immer hinzugefügt
- Wird die Option `channel` weggelassen, gilt der Befehl für den Channel, in dem er ausgeführt wurde
- Unterstützte Channel-Typen: Text, Ankündigungen, Forum und Medien

## Tests

```sh
go test ./...
```

## Auf GCE deployen

Ein Deployment-Skript für Google Compute Engine ist unter `deploy/deploy-gce.ps1` enthalten (Windows PowerShell).

Erstellen Sie `deploy/deploy.json` aus der Beispieldatei für die GCE-Verbindungseinstellungen. App-Einstellungen und Secrets verwenden standardmäßig `.env`; ein anderes File kann über `envFile` in `deploy.json` oder `-EnvFile` angegeben werden.

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# deploy.json und .env bearbeiten

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # Ersteinrichtung
.\deploy\deploy-gce.ps1                          # Nur Code-Updates
.\deploy\deploy-gce.ps1 -UploadEnv               # Secrets aktualisieren
```

## Lizenz

Die Lizenz dieses Projekts findest du in der Datei [LICENSE](LICENSE).
