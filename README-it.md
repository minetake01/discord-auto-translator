# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Un bot per Discord che permette a persone che parlano lingue diverse di chattare insieme nello stesso server.

Collega un canale per lingua formando un **gruppo di traduzione**. Ogni messaggio pubblicato in un canale viene tradotto istantaneamente da Google Gemini e rispecchiato in tutti gli altri canali del gruppo — mantenendo il nome e l'avatar dell'autore originale — così ogni canale si legge come una conversazione naturale nella propria lingua.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-it (Italiano)
```

## Funzionalità

- **Tutto rimane sincronizzato** — non solo i nuovi messaggi: modifiche, eliminazioni, risposte, messaggi inoltrati, reazioni, messaggi fissati, thread (canali testo / forum / media) e messaggi con soli allegati vengono tutti rispecchiati nel gruppo.
- **I messaggi sembrano inviati dal mittente originale** — i messaggi rispecchiati vengono consegnati tramite webhook con il nome e l'avatar dell'autore originale.
- **Traduzioni naturali** — Gemini utilizza il nome del canale, l'argomento e la cronologia recente della conversazione come contesto; un glossario per server permette di fissare le traduzioni preferite per nomi e termini tecnici.
- **Gestione intelligente dei link** — i link e le menzioni che puntano a canali o messaggi gestiti vengono riscritti verso i loro equivalenti in ogni lingua, e gli URL con alternative `hreflang` vengono sostituiti con la versione nella lingua di destinazione.
- **Efficiente e sicuro** — i messaggi senza testo da tradurre (URL, menzioni, emoji personalizzate, codice) vengono rispecchiati senza chiamare l'API di Gemini; si applicano limiti di frequenza di token per server; URL, menzioni e blocchi di codice sono protetti contro l'iniezione di prompt.
- **Interfaccia localizzata** — le risposte ai comandi seguono la lingua del client Discord dell'utente, e le notifiche del canale usano la lingua configurata per quel canale (13 lingue, inglese come fallback).

## Requisiti

- Go 1.24 o versione successiva
- Un account bot Discord con l'intent privilegiato `MESSAGE CONTENT` abilitato
- Una chiave API di Google Gemini

## Configurazione

### 1. Preparare il bot Discord

1. Crea un'applicazione nel [Discord Developer Portal](https://discord.com/developers/applications)
2. Nella pagina **Bot**:
   - Abilita il `MESSAGE CONTENT INTENT` (obbligatorio)
   - Copia il token del bot
3. Invita il bot nel tuo server tramite **OAuth2 → URL Generator**:
   - Scopes: `bot`, `applications.commands`
   - Permessi (come visualizzati nel Developer Portal):
     - **Generali**: `View Channel`, `Read Message History`
     - **Messaggi**: `Send Messages`, `Send Messages in Threads`
     - **Moderazione**: `Pin Messages`
     - **Webhook**: `Manage Webhooks`
     - **Thread**: `Create Public Threads`, `Manage Threads`
     - **Reazioni**: `Add Reactions`
   - Il valore intero dei permessi è `2252126768139328`
   - Per sincronizzare anche le reazioni con emoji personalizzate da altri server, concedi in aggiunta `Use External Emojis`; il valore intero dei permessi diventa `2252126768401472`

### 2. Ottenere una chiave API di Gemini

Ottieni una chiave API da [Google AI Studio](https://aistudio.google.com/).

### 3. Configurare le variabili d'ambiente

```sh
cp .env.example .env
```

Modifica `.env` e imposta i seguenti valori:

```env
DISCORD_TOKEN=your-discord-bot-token
GEMINI_API_KEY=your-gemini-api-key
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
GEMINI_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
# MESSAGE_LINK_RETENTION_DAYS=60
```

| Variabile | Obbligatorio | Descrizione |
|---|---|---|
| `DISCORD_TOKEN` | Sì | Token del bot Discord |
| `GEMINI_API_KEY` | Sì | Chiave API di Gemini |
| `DB_PATH` | No | Percorso del file SQLite (predefinito: `./translator.db`) |
| `HTTP_ADDR` | No | Indirizzo del server di badge avatar (predefinito: `:8080`) |
| `PUBLIC_BASE_URL` | No | URL base pubblico per i badge ad anello degli avatar. Se non impostato, i messaggi rispecchiati usano l'URL avatar Discord originale e il server di badge non viene utilizzato |
| `GEMINI_RATE_LIMIT_TOKENS_PER_MIN` | No | Limite di token Gemini per server al minuto (predefinito: `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | No | Limite di richieste per IP al minuto per l'endpoint badge `/avatar` (predefinito: `120`) |
| `MESSAGE_LINK_RETENTION_DAYS` | No | Giorni di conservazione di `message_links` in SQLite prima della purge automatica. `0` (predefinito) disabilita la purge; es. `60` elimina i link più vecchi di 60 giorni all'avvio e ogni 24 ore |

### 4. Avviare

```sh
go run ./cmd/discord-auto-translator
```

Oppure compilare ed eseguire:

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## Utilizzo

Una volta avviato il bot, i comandi slash vengono registrati in ogni server.

### Configurare i canali

#### Creare un gruppo di traduzione

Esegui `/new-channel` nel tuo canale giapponese per creare un gruppo di traduzione:

```
/new-channel language:ja
```

#### Aggiungere canali in altre lingue

Esegui `/join-channel` nel tuo canale inglese per aggiungerlo al gruppo:

```
/join-channel group:general language:en
```

Per aggiungere anche un canale italiano:

```
/join-channel group:general language:it
```

Ora `#general-ja`, `#general-en` e `#general-it` sono collegati.

### Comandi

Per impostazione predefinita, i comandi slash di amministrazione possono essere eseguiti solo dagli **amministratori del server**. Per consentire ad altri ruoli di usarli, vai nelle "Impostazioni server" di Discord → "Integrazioni" → "Gestisci" del bot → "Autorizzazioni dei comandi" e configura l'accesso globalmente o per singolo comando. Il bot non modifica mai autonomamente ruoli o autorizzazioni dei comandi.

| Comando | Descrizione |
|---|---|
| `/new-channel language:[lingua] channel:<canale> group:<gruppo>` | Creare un nuovo gruppo di traduzione. `channel` usa il canale corrente per impostazione predefinita; `group` usa il nome del canale per impostazione predefinita |
| `/join-channel group:[gruppo] language:[lingua] channel:<canale>` | Aggiungere un canale a un gruppo. `channel` usa il canale corrente per impostazione predefinita |
| `/leave-channel group:[gruppo] channel:<canale>` | Rimuovere un canale da un gruppo. `channel` usa il canale corrente per impostazione predefinita |
| `/delete-group group:[gruppo]` | Eliminare un intero gruppo |
| `/list-groups` | Elencare i gruppi di traduzione e i relativi canali in questo server |
| `/add-glossary term:[termine] translation:[traduzione] attribute:<attributo> always_include:<bool>` | Registrare una traduzione preferita nel glossario del server. `attribute` è testo libero con suggerimenti; `always_include` è `false` per impostazione predefinita |
| `/list-glossary` | Visualizzare il glossario del server |
| `/remove-glossary term:[termine]` | Rimuovere una voce dal glossario |
| `/set-style group:[gruppo] preset:<preset> custom:<istruzione personalizzata>` | Impostare lo stile di traduzione di un gruppo. Specificare `preset` o `custom`, non entrambi |
| `/bot-whitelist add source_type:[bot\|webhook] source_id:[ID]` | Consentire una fonte di messaggi automatizzata in questo server. Con `source_type:bot`, `source_id` è l'ID utente del bot; con `source_type:webhook`, è l'ID del webhook |
| `/bot-whitelist remove source_type:[bot\|webhook] source_id:[ID]` | Rimuovere la fonte di messaggi automatizzata corrispondente dall'elenco consentito di questo server |
| `/bot-whitelist list` | Elencare le fonti bot e webhook consentite in questo server |

- Gli elenchi delle fonti consentite vengono salvati in SQLite e sono limitati a ciascun server Discord (guild). I webhook di output gestiti dal traduttore e i messaggi del bot traduttore stesso restano esclusi anche se vengono aggiunti i relativi ID

- `language` usa codici BCP-47 (`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr`, ecc.)
- Massimo 50 voci nel glossario per server
- `attribute` suggerisce "nome di persona", "nome di luogo", "slang", "abbreviazione" e "termine tecnico", ma può essere inserito qualsiasi valore liberamente. L'attributo viene usato come contesto affinché Gemini comprenda il significato del termine
- I termini normali vengono aggiunti alle istruzioni di sistema solo se il corpo del messaggio da tradurre contiene `term` (senza distinzione tra maiuscole e minuscole). I termini con `always_include:true` vengono sempre aggiunti
- Se l'opzione `channel` viene omessa, il comando si applica al canale in cui è stato eseguito
- Tipi di canale supportati: testo, notizie, forum e media

## Test

```sh
go test ./...
```

## Distribuzione su GCE

Uno script di distribuzione per Google Compute Engine è incluso in `deploy/deploy-gce.ps1` (Windows PowerShell).

Crea `deploy/deploy.json` dall'esempio per le impostazioni di connessione GCE. Configurazione app e segreti usano `.env` per impostazione predefinita; un file diverso può essere indicato tramite `envFile` in `deploy.json` o `-EnvFile`.

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# Modificare deploy.json e .env

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # Configurazione iniziale
.\deploy\deploy-gce.ps1                          # Solo aggiornamenti codice
.\deploy\deploy-gce.ps1 -UploadEnv               # Aggiornare i segreti
```

## Licenza

Consulta il file [LICENSE](LICENSE) per la licenza di questo progetto.
