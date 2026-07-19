# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Un bot per Discord che permette a persone che parlano lingue diverse di chattare insieme nello stesso server.

Collega un canale per lingua formando un **gruppo di traduzione**. Ogni messaggio pubblicato in un canale viene tradotto istantaneamente da `google.gemma-4-26b-a4b` tramite Amazon Bedrock e rispecchiato in tutti gli altri canali del gruppo — mantenendo il nome e l'avatar dell'autore originale — così ogni canale si legge come una conversazione naturale nella propria lingua.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-it (Italiano)
```

## Funzionalità

- **Tutto rimane sincronizzato** — non solo i nuovi messaggi: modifiche, eliminazioni, risposte, messaggi inoltrati, reazioni, messaggi fissati, thread (canali testo / forum / media) e messaggi con soli allegati vengono tutti rispecchiati nel gruppo.
- **I messaggi sembrano inviati dal mittente originale** — i messaggi rispecchiati vengono consegnati tramite webhook con il nome e l'avatar dell'autore originale.
- **Traduzioni naturali** — Gemma 4 26B-A4B utilizza il nome del canale, l'argomento e la cronologia recente della conversazione come contesto; un glossario per server permette di fissare le traduzioni preferite per nomi e termini tecnici.
- **Gestione intelligente dei link** — i link e le menzioni che puntano a canali o messaggi gestiti vengono riscritti verso i loro equivalenti in ogni lingua, e gli URL con alternative `hreflang` vengono sostituiti con la versione nella lingua di destinazione.
- **Efficiente e sicuro** — i messaggi senza testo da tradurre (URL, menzioni, emoji personalizzate, codice) vengono rispecchiati senza chiamare l'API di traduzione; si applicano limiti di frequenza di token per server; URL, menzioni e blocchi di codice sono protetti contro l'iniezione di prompt. In caso di errore di traduzione: fail-closed (nessun rispecchiamento, notifica localizzata nel canale di origine).
- **Interfaccia localizzata** — le risposte ai comandi seguono la lingua del client Discord dell'utente, e le notifiche del canale usano la lingua configurata per quel canale (13 lingue, inglese come fallback).

## Requisiti

- Go 1.24 o versione successiva
- Un account bot Discord con l'intent privilegiato `MESSAGE CONTENT` abilitato
- Un account AWS con accesso ad Amazon Bedrock e una chiave IAM autorizzata a creare inferenze nel Project Mantle predefinito in `us-west-2`.
- Un ID Amazon Bedrock

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

### 2. Configurare Amazon Bedrock

1. Abilita `google.gemma-4-26b-a4b` in Amazon Bedrock nella regione `us-west-2`.
2. Crea un utente IAM dedicato e consenti solo `bedrock-mantle:CreateInference` su `arn:aws:bedrock-mantle:us-west-2:<account-id>:project/default`.
3. Crea una chiave di accesso e imposta `AWS_ACCESS_KEY_ID` e `AWS_SECRET_ACCESS_KEY` in `.env`.

Modello, regione, timeout e limite di output sono fissi nel codice. `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` (predefinito `100000`) regola facoltativamente il limite di token per server.
### 3. Configurare le variabili d'ambiente

```sh
cp .env.example .env
```

Modifica `.env` e imposta i seguenti valori:

```env
DISCORD_TOKEN=your-discord-bot-token
AWS_ACCESS_KEY_ID=your-aws-access-key-id
AWS_SECRET_ACCESS_KEY=your-aws-secret-access-key
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
| `AWS_ACCESS_KEY_ID` | Yes | Access key ID for the dedicated Bedrock IAM user |
| `AWS_SECRET_ACCESS_KEY` | Yes | Secret access key for the dedicated Bedrock IAM user |
| `DB_PATH` | No | Percorso del file SQLite (predefinito: `./translator.db`) |
| `HTTP_ADDR` | No | Indirizzo del server di badge avatar (predefinito: `:8080`) |
| `PUBLIC_BASE_URL` | No | URL base pubblico per i badge ad anello degli avatar. Se non impostato, i messaggi rispecchiati usano l'URL avatar Discord originale e il server di badge non viene utilizzato |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | No | Limite di token Gemma 4 26B-A4B per server al minuto (predefinito: `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | No | Limite di richieste per IP al minuto per l'endpoint badge `/avatar` (predefinito: `120`) |
| `MESSAGE_LINK_RETENTION_DAYS` | No | Giorni di conservazione di `message_links` in SQLite prima della purge automatica. `0` (predefinito) disabilita la purge; es. `60` elimina i link più vecchi di 60 giorni all'avvio e ogni 24 ore |
| `GUILD_DATA_RETENTION_DAYS` | No | Giorni di conservazione in SQLite dei dati di un server dopo la rimozione del bot. `0` (predefinito) disabilita la purge; es. `30` elimina all'avvio e ogni 24 ore i dati dei server rimossi da più di 30 giorni. Un nuovo ingresso prima della scadenza annulla l'eliminazione programmata |

### Contratto operativo Amazon Bedrock

La traduzione usa Mantle Responses API non streaming con `google.gemma-4-26b-a4b` in `us-west-2`: timeout **30 s**, **provider-default temperature 1.0**, **max_output_tokens 4096** e JSON guidato dallo schema e convalidato rigorosamente dal bot. Tutte le lingue sono generate in una richiesta. Limite 4K, arresto anomalo o JSON non valido fanno fallire tutto in modalità fail-closed; non esistono retry, suddivisione o fallback. Il bot non registra prompt, risposte o credenziali. Il deploy GCE convalida credenziali, accesso al modello e contratto di risposta prima della sostituzione tramite `--bedrock-prewarm` con limite di cinque minuti.

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
- `attribute` suggerisce "nome di persona", "nome di luogo", "slang", "abbreviazione" e "termine tecnico", ma può essere inserito qualsiasi valore liberamente. L'attributo viene usato come contesto affinché Gemma 4 26B-A4B comprenda il significato del termine
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
