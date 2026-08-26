# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Ein Discord-Bot, der es Menschen mit unterschiedlichen Sprachen ermöglicht, in Echtzeit und in ihrer jeweiligen Muttersprache auf demselben Discord-Server miteinander zu kommunizieren.

Indem für jede Sprache ein eigener Kanal zu einer **Übersetzungsgruppe** verknüpft wird, wird jede Nachricht automatisch durch ein OpenAI-kompatibles LLM (Chat Completions API) übersetzt und mit dem **Namen und Avatar des ursprünglichen Absenders** in alle anderen Kanäle der Gruppe gespiegelt. So liest sich jeder Kanal wie ein ganz normales Gespräch in der eigenen Sprache.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. Benutzer- & Server-Administrator-Handbuch

### Hauptfunktionen & Benutzererlebnis

- **Ganz natürlich wie gewohnt chatten**
  Keine speziellen Befehle oder Präfixe erforderlich. Sende deine Nachrichten wie gewohnt – sie werden automatisch in Echtzeit übersetzt und an alle verknüpften Kanäle übertragen.
- **Nachrichten behalten das Profil des Absenders**
  Gespiegelte Nachrichten werden über Discord-Webhooks versendet, sodass Name und Avatar des Originalautors unverändert erhalten bleiben.
- **Vollständige bidirektionale Synchronisierung in Echtzeit**
  - **Neue Nachrichten & Anhänge**: Unterstützt Texte, Bilder (inklusive Bildbeschreibungen / Alt-Texte) und Datei-Anhänge.
  - **Bearbeitungen & Löschungen**: Das Bearbeiten oder Löschen einer Nachricht wird sofort auf alle übersetzten Versionen übertragen.
  - **Antworten (Replies)**: Zitiert einen Ausschnitt der referenzierten Nachricht in der Zielsprache und verlinkt auf die entsprechende Nachricht (Pseudo-Antwort).
  - **Weitergeleitete Nachrichten**: Synchronisiert weitergeleitete Inhalte mit lokalisierten Kopfzeilen.
  - **Reaktionen & Pins**: Emoji-Reaktionen und das Anpinnen von Nachrichten werden in beide Richtungen synchronisiert.
  - **Threads & Foren**: Unterstützt normale Threads sowie Forums- und Medienkanäle inklusive automatischer Tag-Zuordnung.
  - **Umfragen (Polls)**: Übersetzt Fragen und Auswahlmöglichkeiten in ein Embed-Format und postet das Endergebnis nach Abschluss der Umfrage.
- **„View Original“ (Original anzeigen)**
  Klicke mit der rechten Maustaste (oder tippe auf dem Smartphone lange) auf eine übersetzte Nachricht und wähle **„Apps“ → „View Original“**, um einen direkten Link zur Originalnachricht und eine Textvorschau (nur für dich sichtbar) zu erhalten.
- **Intelligente Link- und Medienverarbeitung**
  - Links und Erwähnungen von verwalteten Kanälen, Nachrichten oder Threads werden automatisch auf die IDs der Zielsprache umgeschrieben.
  - Externe Web-Links mit `hreflang`-Sprachversionen werden automatisch durch die Version in der Zielsprache ersetzt.

---

### Server-Einrichtung

#### 1. Bot einladen
Erstelle im Discord Developer Portal einen Einladungslink mit folgenden Berechtigungen:

- **OAuth2 Scopes**: `bot`, `applications.commands`
- **Bot-Berechtigungen (Bot Permissions)**:
  - **Allgemein**: `View Channels` (Kanäle ansehen), `Read Message History` (Nachrichtenverlauf lesen)
  - **Text**: `Send Messages` (Nachrichten senden), `Send Messages in Threads` (Nachrichten in Threads senden)
  - **Moderation**: `Pin Messages` (Nachrichten anpinnen)
  - **Webhooks**: `Manage Webhooks` (Webhooks verwalten)
  - **Threads**: `Create Public Threads` (Öffentliche Threads erstellen), `Manage Threads` (Threads verwalten)
  - **Reaktionen**: `Add Reactions` (Reaktionen hinzufügen)
- **Berechtigungs-Integer**: `2252126768139328`
  - *Hinweis: Um auch benutzerdefinierte Emoji-Reaktionen von externen Servern zu synchronisieren, aktiviere zusätzlich `Use External Emojis` (Berechtigungs-Integer: `2252126768401472`).*

#### 2. Privilegierten Intent aktivieren
Stelle sicher, dass im Discord Developer Portal unter dem Reiter **Bot** der `MESSAGE CONTENT INTENT` aktiviert ist.

---

### Kanal-Konfiguration (Grundlegende Befehle)

#### Übersetzungsgruppe erstellen
Führe im japanischen Kanal (z. B. `#general-ja`) den Befehl `/new-channel` aus:

```
/new-channel language:ja
```
*Hinweis: Wenn `group` weggelassen wird, dient der aktuelle Kanalname als Gruppenname.*

#### Weitere Sprachkanäle hinzufügen
Führe im englischen Kanal (z. B. `#general-en`) `/join-channel` aus:

```
/join-channel group:general language:en
```

Um einen deutschen Kanal hinzuzufügen (z. B. `#general-de`):

```
/join-channel group:general language:de
```

Jetzt sind `#general-ja`, `#general-en` und `#general-de` miteinander verknüpft und die automatische Übersetzung ist aktiv.

#### Kanäle entfernen und Gruppen löschen
- Kanal aus Gruppe entfernen: `/leave-channel group:general`
- Eine Übersetzungsgruppe vollständig löschen: `/delete-group group:general`
- Aktive Gruppen und Kanäle auflisten: `/list-groups`

---

### Befehlsreferenz

#### Slash-Befehle für Administratoren
Standardmäßig können administrative Befehle nur von Benutzern mit **Administrator-Rechten** ausgeführt werden. Weitere Rollen können in Discord unter **Servereinstellungen → Integrationen → (Bot-Name) → Verwalten → Befehlsberechtigungen** freigeschaltet werden.

| Befehl | Beschreibung | Wichtige Optionen |
|---|---|---|
| `/new-channel` | Erstellt eine neue Übersetzungsgruppe und registriert einen Kanal | `language` (erforderlich): BCP-47 Sprachcode<br>`channel` (optional): Zielkanal (Standard: aktueller Kanal)<br>`group` (optional): Gruppenname (Standard: Kanalname) |
| `/join-channel` | Fügt einen Kanal zu einer bestehenden Gruppe hinzu | `group` (erforderlich): Gruppenname<br>`language` (erforderlich): BCP-47 Sprachcode<br>`channel` (optional): Zielkanal (Standard: aktueller Kanal) |
| `/leave-channel` | Entfernt einen Kanal aus einer Gruppe | `group` (erforderlich): Gruppenname<br>`channel` (optional): Zielkanal (Standard: aktueller Kanal) |
| `/delete-group` | Löscht eine Übersetzungsgruppe vollständig | `group` (erforderlich): Name der zu löschenden Gruppe |
| `/list-groups` | Listet alle Übersetzungsgruppen und verknüpften Kanäle auf | Keine |
| `/set-style` | Legt den Übersetzungsstil oder Tonfall für eine Gruppe fest | `group` (erforderlich): Gruppenname<br>`preset` (optional): Stil-Voreinstellung (siehe unten)<br>`custom` (optional): Eigene Anweisung in natürlicher Sprache (max. 200 Zeichen) |
| `/add-glossary` | Registriert eine bevorzugte Begriff-Übersetzung im Server-Glossar | `term` (erforderlich): Quellbegriff<br>`translation` (erforderlich): Bevorzugte Übersetzung<br>`attribute` (optional): Begriffskategorie (z. B. Personenname, Slang)<br>`always_include` (optional): Auch ohne Treffer immer im Prompt übergeben (Standard: `false`) |
| `/list-glossary` | Zeigt alle Glossareinträge des Servers an | Keine |
| `/remove-glossary`| Entfernt einen Eintrag aus dem Glossar | `term` (erforderlich): Zu entfernender Begriff |
| `/edit-forum-tags` | Bearbeitet Tag-Zuordnungen für Forums-/Medienkanäle | `group` (erforderlich): Gruppenname<br>`channel` (optional): Ziel-Forumskanal |
| `/bot-whitelist` | Verwaltet die Whitelist für automatisierte Bots und Webhooks | Unterbefehle: `add`, `remove`, `list`<br>`source_type`: `bot` oder `webhook`<br>`source_id`: Bot-Benutzer-ID oder Webhook-ID |

#### Nachrichten-Befehl (Für alle Benutzer)
- **`View Original` (App-Menü)**
  Rechtsklick oder langes Antippen einer Nachricht → **„Apps“ → „View Original“**, um einen direkten Link zur Originalnachricht und eine Vorschau des Originaltextes zu erhalten.

---

### Erweiterte Anpassungen

#### 1. Übersetzungsstil (`/set-style`)
Passe den Ton der Übersetzung an die Atmosphäre deines Servers an (`preset` und `custom` schließen sich gegenseitig aus):

| Voreinstellung | Beschreibung & Verwendung |
|---|---|
| `default` | Natürlicher Konversationsstil von Muttersprachlern im Chat |
| `casual` | Entspannter und freundschaftlicher Umgangston |
| `gaming` | Gamer-Slang und Gaming-Community-Stil |
| `friendly` | Herzlicher, höflicher und sympathischer Tonfall |
| `business` | Prägnanter, sachlicher und professioneller Stil |
| `formal` | Formelle Sprache mit Siezen und Höflichkeitsformen |
| `netslang` | Internet-Slang und Foren-Stil |
| `tweet` | Kurzer, prägnanter Social-Media-Stil (X / Twitter) |
| `literal` | Wörtliche Übersetzung bei mehrdeutigen Formulierungen |

#### 2. Server-Glossar (`/add-glossary`)
Lege Übersetzungen für Charakternamen, Gaming-Begriffe oder Community-Jargon fest (bis zu 50 Einträge pro Server):
- **Attribute (`attribute`)**: Die Angabe von Kategorien wie „Personenname“, „Ortsname“, „Slang“, „Abkürzung“ oder „Fachbegriff“ hilft der KI, den Kontext präzise zu verstehen.
- **Immer einschließen (`always_include`)**: Wenn auf `true` gesetzt, wird der Begriff dauerhaft als Kontext übergeben, auch wenn das Wort nicht direkt in der Nachricht vorkommt.

#### 3. Tag-Zuordnung für Foren (`/edit-forum-tags`)
Beim Verknüpfen von Forums- oder Medienkanälen können Tags sprachübergreifend zugeordnet werden. Erstellt ein Benutzer einen Beitrag mit einem Tag, erhält der gespiegelte Beitrag automatisch das entsprechende Partner-Tag.

#### 4. Whitelist für Bots & Webhooks (`/bot-whitelist`)
Standardmäßig werden Nachrichten von Bots und Webhooks ignoriert, um Endlosschleifen zu verhindern. Mit `/bot-whitelist add` können Ankündigungs-Bots, RSS-Feeds oder Benachrichtigungen explizit für die Übersetzung freigeschaltet werden.

---

## 2. Entwickler- & Self-Hosting-Handbuch

### Systemanforderungen & Tech-Stack

- **Programmiersprache**: Go 1.24 oder neuer
- **Datenbank**: SQLite (Reiner Go-Treiber via `modernc.org/sqlite`, kein CGO erforderlich)
- **Discord-Bibliothek**: `github.com/bwmarrin/discordgo`
- **Übersetzungs-Engine**: OpenAI-kompatible Chat Completions API (OpenAI, OpenRouter, Azure OpenAI, lokale LLMs etc.)
- **Cross-Compilation**: Vollständige Unterstützung mit `CGO_ENABLED=0` für schlanke Standalone-Binaries auf Linux, Windows und macOS.

---

### Self-Hosting & Inbetriebnahme

#### 1. Discord-Bot erstellen
1. Öffne das [Discord Developer Portal](https://discord.com/developers/applications) und erstelle eine neue Application.
2. Aktiviere im Tab **Bot** den `MESSAGE CONTENT INTENT` und kopiere das Bot-Token.
3. Wähle unter **OAuth2 → URL Generator** die Scopes `bot` und `applications.commands` mit den erforderlichen Berechtigungen aus und lade den Bot auf deinen Server ein.

#### 2. OpenAI-kompatible API vorbereiten
Beschaffe API-Endpunkt-URL, API-Schlüssel und Modell-ID deines bevorzugten LLM-Anbieters.

#### 3. Umgebungsvariablen konfigurieren
Kopiere `.env.example` nach `.env` und passe die Einstellungen an:

```sh
cp .env.example .env
```

```env
DISCORD_TOKEN=your-discord-bot-token
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=your-openai-api-key
OPENAI_MODEL=your-model-id
# OPENAI_REASONING_EFFORT=none
DB_PATH=./translator.db
HTTP_ADDR=:8080
# PUBLIC_BASE_URL=https://your-public-domain.example
TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
# MESSAGE_LINK_RETENTION_DAYS=60
# GUILD_DATA_RETENTION_DAYS=30
```

#### 4. Bauen & Ausführen

Direkte lokale Ausführung:
```sh
go run ./cmd/discord-auto-translator
```

Als eigenständige Binary kompilieren und starten:
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**Modell-Vorabprüfung (`--model-prewarm`)**:
Überprüft vor dem Deployment API-Authentifizierung, Modellverbindung und JSON-Schema-Verträge (startet keinen Discord- oder HTTP-Dienst):
```sh
./discord-auto-translator --model-prewarm
```

---

### Umgebungsvariablen-Referenz

| Variable | Erforderlich | Standard | Beschreibung |
|---|---|---|---|
| `DISCORD_TOKEN` | **Ja** | - | Discord-Bot-Authentifizierungstoken |
| `OPENAI_BASE_URL` | **Ja** | - | Basis-URL der OpenAI-kompatiblen Chat Completions API (z. B. `https://api.openai.com/v1`) |
| `OPENAI_API_KEY` | **Ja** | - | Bearer-API-Schlüssel |
| `OPENAI_MODEL` | **Ja** | - | Modell-ID beim LLM-Anbieter |
| `OPENAI_REASONING_EFFORT` | Nein | (nicht gesetzt) | Optionaler `reasoning_effort` Parameter. Auf `none` setzen, um Denk-Tokens bei Hybrid-Modellen zu deaktivieren |
| `DB_PATH` | Nein | `./translator.db` | Pfad zur SQLite-Datenbankdatei |
| `HTTP_ADDR` | Nein | `:8080` | Adresse für den Avatar-Badge-HTTP-Server |
| `PUBLIC_BASE_URL` | Nein | (nicht gesetzt) | Öffentliche Basis-URL für Avatar-Farbringe. Rendert einen Ring in der Farbe der höchsten Serverrolle |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | Nein | `100000` | Token-Limit pro Server (Guild) pro Minute |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | Nein | `120` | Anfragelimit pro Minute pro IP für den `/avatar`-Endpunkt |
| `MESSAGE_LINK_RETENTION_DAYS` | Nein | `0` | Aufbewahrungsdauer für Nachrichtenverknüpfungen in Tagen. `0` = unbegrenzt |
| `GUILD_DATA_RETENTION_DAYS` | Nein | `0` | Aufbewahrungsdauer für Serverdaten nach Verlassen des Bots |

---

### Architektur & Design

#### 1. Übersetzungspipeline
1. **Kontextsammlung**: Erfasst Kanalthemen, Gesprächsverlauf-Bursts, Zitate, OGP-Metadaten von URLs und skalierte Bildanhänge.
2. **Platzhalter-Maskierung**: Ersetzt Erwähnungen (`<@id>`), Emojis (`<:name:id>`), Kanäle (`<#id>`), URLs und Codeblöcke durch Tokens wie `[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]`, um Prompt-Injection und fehlerhafte Übersetzungen zu verhindern.
3. **Prompt-Zusammensetzung & Caching**: Schichtet System-Prompt, stabilen Kontext und dynamischen Inhalt, um Prefix Prompt Caching der KI-Anbieter optimal zu nutzen.
4. **Structured Outputs**: Nutzt `response_format.type=json_schema` (`strict: true`), um alle Zielsprachen in einem einzigen API-Aufruf strukturiert als JSON abzurufen.
5. **Nachbereitung & Versand**: Stellt Platzhalter wieder her, passt interne Discord-Links an, ersetzt `hreflang`-URLs und verteilt die Webhook-Nachrichten parallel.

#### 2. Sicherheit & Fail-Closed-Verhalten
- **Schutz vor Prompt-Injection**: Alle Benutzereingaben werden XML-escaped und in isolierten Tags verpackt.
- **Fail-Closed-Prinzip**: Bei Token-Überschreitung (`finish_reason=length`), ungültigem JSON oder Netzwerkfehlern wird keine fehlerhafte Nachricht gespiegelt, sondern eine lokalisierte Fehlermeldung im Quellkanal gepostet.

#### 3. Zuverlässigkeit & Datenkonsistenz
- **Idempotenz**: Verhindert doppelte Nachrichten bei mehrfach empfangenen Gateway-Events über `message_links` und `processed_events`.
- **Kompensationstransaktionen**: Schlägt das Speichern in der Datenbank nach einem Webhook-Post fehl, wird die gesendete Discord-Nachricht sofort wieder gelöscht.
- **Bidirektionale Synchronisierung**: Reaktionen und Pins werden anhand der Nachrichtenverknüpfungen in allen Kanälen der Gruppe synchron gehalten.

---

### Entwicklung & Tests

#### Tests ausführen
```sh
go test ./...
```

#### Mehrsprachiger UI-Katalog (i18n)
Alle Benutzeroberflächen-Texte und Fehlermeldungen werden in `internal/translatorbot/ui_strings.go` für 13 Sprachen verwaltet. Neue Texte müssen für alle Sprachen definiert und durch `TestUIStringCatalogIsComplete` geprüft werden.

---

## 3. Lizenz

Dieses Projekt ist unter der [MIT License](LICENSE) lizenziert.
