# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Un bot per Discord che permette a persone che parlano lingue diverse di chattare insieme nello stesso server.

Collega un canale per lingua formando un **gruppo di traduzione**. Ogni messaggio pubblicato in un canale viene tradotto istantaneamente da `@cf/google/gemma-4-26b-a4b-it` tramite Cloudflare AI Gateway e rispecchiato in tutti gli altri canali del gruppo — mantenendo il nome e l'avatar dell'autore originale — così ogni canale si legge come una conversazione naturale nella propria lingua.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-it (Italiano)
```

## Funzionalità

- **Tutto rimane sincronizzato** — non solo i nuovi messaggi: modifiche, eliminazioni, risposte, messaggi inoltrati, reazioni, messaggi fissati, thread (canali testo / forum / media) e messaggi con soli allegati vengono tutti rispecchiati nel gruppo.
- **I messaggi sembrano inviati dal mittente originale** — i messaggi rispecchiati vengono consegnati tramite webhook con il nome e l'avatar dell'autore originale.
- **Traduzioni naturali** — Gemma 4 utilizza il nome del canale, l'argomento e la cronologia recente della conversazione come contesto; un glossario per server permette di fissare le traduzioni preferite per nomi e termini tecnici.
- **Gestione intelligente dei link** — i link e le menzioni che puntano a canali o messaggi gestiti vengono riscritti verso i loro equivalenti in ogni lingua, e gli URL con alternative `hreflang` vengono sostituiti con la versione nella lingua di destinazione.
- **Efficiente e sicuro** — i messaggi senza testo da tradurre (URL, menzioni, emoji personalizzate, codice) vengono rispecchiati senza chiamare l'API di traduzione; si applicano limiti di frequenza di token per server; URL, menzioni e blocchi di codice sono protetti contro l'iniezione di prompt. In caso di errore di traduzione: fail-closed (nessun rispecchiamento, notifica localizzata nel canale di origine).
- **Interfaccia localizzata** — le risposte ai comandi seguono la lingua del client Discord dell'utente, e le notifiche del canale usano la lingua configurata per quel canale (13 lingue, inglese come fallback).

## Requisiti

- Go 1.24 o versione successiva
- Un account bot Discord con l'intent privilegiato `MESSAGE CONTENT` abilitato
- Un ID account Cloudflare
- Un token API Cloudflare per ambiente di deployment
- Un ID Cloudflare AI Gateway

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

### 2. Configurare Cloudflare Workers AI e AI Gateway

1. Crea **un** token API Cloudflare per ambiente di deployment; l'ambito è definito dall'operatore.
2. Crea un [AI Gateway](https://developers.cloudflare.com/ai-gateway/get-started/) per quell'account e annota l'ID (`CLOUDFLARE_AI_GATEWAY_ID`).
3. Nel pannello Gateway, **abilita la cache** e **disabilita il logging dei payload** (solo metadati).
4. Lascia retry, fallback, routing dinamico, DLP e guardrails **disabilitati** — il bot non li usa.

Imposta `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_API_TOKEN` e `CLOUDFLARE_AI_GATEWAY_ID` in `.env`. Opzionalmente regola il throughput con `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` (predefinito `100000`).

### 3. Configurare le variabili d'ambiente

```sh
cp .env.example .env
```

Modifica `.env` e imposta i seguenti valori:

```env
DISCORD_TOKEN=your-discord-bot-token
CLOUDFLARE_ACCOUNT_ID=your-cloudflare-account-id
CLOUDFLARE_API_TOKEN=your-cloudflare-api-token
CLOUDFLARE_AI_GATEWAY_ID=your-cloudflare-ai-gateway-id
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
# MESSAGE_LINK_RETENTION_DAYS=60
# GUILD_DATA_RETENTION_DAYS=30
```

| Variabile | Obbligatorio | Descrizione |
|---|---|---|
| `DISCORD_TOKEN` | Sì | Token del bot Discord |
| `CLOUDFLARE_ACCOUNT_ID` | Sì | ID account Cloudflare per Workers AI / AI Gateway |
| `CLOUDFLARE_API_TOKEN` | Sì | Un token API per ambiente di deployment (ambito definito dall'operatore) |
| `CLOUDFLARE_AI_GATEWAY_ID` | Sì | ID AI Gateway; valore diverso per ambiente se necessario |
| `DB_PATH` | No | Percorso del file SQLite (predefinito: `./translator.db`) |
| `HTTP_ADDR` | No | Indirizzo del server di badge avatar (predefinito: `:8080`) |
| `PUBLIC_BASE_URL` | No | URL base pubblico per i badge ad anello degli avatar. Se non impostato, i messaggi rispecchiati usano l'URL avatar Discord originale e il server di badge non viene utilizzato |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | No | Limite di token Gemma 4 per server al minuto (predefinito: `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | No | Limite di richieste per IP al minuto per l'endpoint badge `/avatar` (predefinito: `120`) |
| `MESSAGE_LINK_RETENTION_DAYS` | No | Giorni di conservazione di `message_links` in SQLite prima della purge automatica. `0` (predefinito) disabilita la purge; es. `60` elimina i link più vecchi di 60 giorni all'avvio e ogni 24 ore |
| `GUILD_DATA_RETENTION_DAYS` | No | Giorni di conservazione in SQLite dei dati di un server dopo la rimozione del bot. `0` (predefinito) disabilita la purge; es. `30` elimina all'avvio e ogni 24 ore i dati dei server rimossi da più di 30 giorni. Un nuovo ingresso prima della scadenza annulla l'eliminazione programmata |

### Contratto operativo Cloudflare AI Gateway

La traduzione usa `@cf/google/gemma-4-26b-a4b-it` tramite il [Cloudflare AI Gateway](https://developers.cloudflare.com/ai-gateway/) configurato. L'ID del modello è fisso nel codice e non può essere modificato tramite variabile d'ambiente.

Il bot instrada sempre le richieste di traduzione tramite `CLOUDFLARE_AI_GATEWAY_ID`. Configura **un** token API Cloudflare (`CLOUDFLARE_API_TOKEN`) per ambiente di deployment tramite variabile d'ambiente; l'ambito è definito dall'operatore.

Parametri fissi: chat completions non in streaming, timeout HTTP **10 s**, **temperature 0.2**, **max_tokens 16384**, schema JSON strict multilingue. Parametri reasoning/thinking omessi (predefinito del provider).

**Gateway (da mantenere nel pannello):**

- **Cache** — abilitata; il bot invia il corpo completo della richiesta, senza intestazione di bypass cache né chiave cache personalizzata
- **Logging** — solo metadati; logging payload disabilitato; il bot invia `cf-aig-collect-log-payload: false` e `cf-aig-metadata` solo con `guild_id` e `message_id`
- **Funzioni disabilitate** — retry, fallback, routing dinamico, DLP e guardrails non usati

Il bot non registra prompt, risposte né token API. Deployment e scelta dell'ambiente (ID Gateway / account) sono responsabilità dell'utente. Errori di traduzione e superamento limiti: **fail-closed** — il messaggio non viene rispecchiato; il canale di origine riceve una notifica localizzata.

Poiché l'offerta Cloudflare Workers AI per questo modello è in beta, questa migrazione non esegue test A/B live né quality gate automatizzati.

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
- `attribute` suggerisce "nome di persona", "nome di luogo", "slang", "abbreviazione" e "termine tecnico", ma può essere inserito qualsiasi valore liberamente. L'attributo viene usato come contesto affinché Gemma 4 comprenda il significato del termine
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
