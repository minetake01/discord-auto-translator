# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Un bot Discord che consente a persone che parlano lingue diverse di comunicare in tempo reale nello stesso server Discord, ciascuna usando la propria lingua madre.

Collegando un canale per lingua all'interno di un **gruppo di traduzione**, qualsiasi messaggio inviato in un canale viene tradotto automaticamente da un LLM compatibile con OpenAI (API Chat Completions) e inoltrato a tutti gli altri canali del gruppo con il **nome e l'avatar dell'autore originale**. Ogni canale può così essere letto come una normale conversazione nella propria lingua.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. Guida per Utenti e Amministratori

### Funzionalità Principali ed Esperienza Utente

- **Chatta normalmente come sempre**
  Nessun comando o prefisso speciale richiesto. Scrivi e invia semplicemente i tuoi messaggi: verranno tradotti e sincronizzati in tempo reale con tutti gli altri canali collegati.
- **I messaggi mantengono l'identità dell'autore**
  I messaggi tradotti vengono inoltrati tramite Webhook di Discord, preservando il nome e l'avatar dell'autore originale.
- **Sincronizzazione bidirezionale completa in tempo reale**
  - **Nuovi messaggi e allegati**: Supporta testi, immagini (compresi testi alternativi / descrizioni) e vari allegati.
  - **Modifiche e cancellazioni**: Modificare o cancellare un messaggio originale aggiorna o elimina istantaneamente i messaggi tradotti.
  - **Risposte (Replies)**: Cita un estratto del messaggio di riferimento nella lingua di destinazione con link al messaggio corrispondente (pseudo-risposta).
  - **Messaggi inoltrati**: Mantiene il contesto inoltrato con intestazione localizzata.
  - **Reazioni e messaggi fissati (Pins)**: L'aggiunta/rimozione di reazioni emoji e il fissaggio dei messaggi sono sincronizzati in entrambe le direzioni.
  - **Thread e forum**: Supporta thread standard, canali forum e media, inclusa la mappatura automatica dei tag.
  - **Sondaggi (Polls)**: Traduce domande e opzioni in formato Embed e pubblica i risultati finali al termine del sondaggio.
- **Funzione «View Original» (Visualizza originale)**
  Fai clic destro (o tieni premuto su mobile) su qualsiasi messaggio tradotto e seleziona **«Applicazioni» → «View Original»** per ottenere un link diretto e un'anteprima del testo originale (visibile solo a te).
- **Gestione intelligente di link e media**
  - I link e le menzioni a canali, messaggi o thread gestiti vengono riscritti automaticamente per puntare ai corrispondenti ID della lingua di destinazione.
  - Gli URL di siti web esterni con versioni `hreflang` vengono sostituiti automaticamente con la versione nella lingua di destinazione.

---

### Installazione nel Server

#### 1. Invitare il Bot
Genera un link di invito nel Discord Developer Portal con i seguenti permessi:

- **OAuth2 Scopes**: `bot`, `applications.commands`
- **Permessi del Bot (Bot Permissions)**:
  - **Generale**: `View Channels` (Visualizza canali), `Read Message History` (Leggi la cronologia messaggi)
  - **Testo**: `Send Messages` (Invia messaggi), `Send Messages in Threads` (Invia messaggi nei thread)
  - **Moderazione**: `Pin Messages` (Fissa messaggi)
  - **Webhooks**: `Manage Webhooks` (Gestisci webhook)
  - **Thread**: `Create Public Threads` (Crea thread pubblici), `Manage Threads` (Gestisci thread)
  - **Reazioni**: `Add Reactions` (Aggiungi reazioni)
- **Valore intero dei permessi (Permissions Integer)**: `2252126768139328`
  - *Nota: Per sincronizzare anche le reazioni con emoji personalizzate da altri server, abilita `Use External Emojis` (Permissions Integer: `2252126768401472`).*

#### 2. Abilitare l'Intent Privilegiato
Assicurati che l'opzione `MESSAGE CONTENT INTENT` sia abilitata nella scheda **Bot** del Discord Developer Portal.

---

### Configurazione dei Canali (Operazioni Base)

#### Creare un gruppo di traduzione
Nel tuo canale in giapponese (es: `#general-ja`), esegui `/new-channel`:

```
/new-channel language:ja
```
*Nota: Se si omette `group`, verrà utilizzato il nome del canale attuale come identificatore.*

#### Aggiungere canali in altre lingue
Nel tuo canale in inglese (es: `#general-en`), esegui `/join-channel`:

```
/join-channel group:general language:en
```

Per aggiungere un canale in italiano (es: `#general-it`):

```
/join-channel group:general language:it
```

Ora `#general-ja`, `#general-en` e `#general-it` sono collegati e la traduzione automatica è attiva.

#### Uscire da un gruppo ed eliminare gruppi
- Rimuovere un canale dal gruppo: `/leave-channel group:general`
- Eliminare completamente un gruppo: `/delete-group group:general`
- Visualizzare i gruppi e i canali attivi: `/list-groups`

---

### Riferimento dei Comandi

#### Comandi Slash per Amministratori
Per impostazione predefinita, i comandi di amministrazione sono riservati agli utenti con **permessi di Amministratore**. Per autorizzare altri ruoli, vai su: **Impostazioni del server → Integrazioni → (Nome del bot) → Gestisci → Permessi dei comandi**.

| Comando | Descrizione | Opzioni Principali |
|---|---|---|
| `/new-channel` | Crea un nuovo gruppo di traduzione e registra un canale | `language` (obbligatorio): Codice lingua BCP-47<br>`channel` (opzionale): Canale di destinazione (predefinito: canale corrente)<br>`group` (opzionale): Nome del gruppo (predefinito: nome canale) |
| `/join-channel` | Aggiunge un canale a un gruppo esistente | `group` (obbligatorio): Nome del gruppo<br>`language` (obbligatorio): Codice lingua BCP-47<br>`channel` (opzionale): Canale di destinazione (predefinito: canale corrente) |
| `/leave-channel` | Rimuove un canale da un gruppo | `group` (obbligatorio): Nome del gruppo<br>`channel` (opzionale): Canale di destinazione (predefinito: canale corrente) |
| `/delete-group` | Elimina completamente un gruppo di traduzione | `group` (obbligatorio): Nome del gruppo da eliminare |
| `/list-groups` | Elenca tutti i gruppi di traduzione e i canali collegati | Nessuna |
| `/set-style` | Imposta lo stile o il tono di traduzione per il gruppo | `group` (obbligatorio): Nome del gruppo<br>`preset` (opzionale): Preimpostazione di stile (vedi sotto)<br>`custom` (opzionale): Istruzione personalizzata in linguaggio naturale (max 200 caratteri) |
| `/add-glossary` | Registra una traduzione preferita nel glossario del server | `term` (obbligatorio): Termine di origine<br>`translation` (obbligatorio): Traduzione preferita<br>`attribute` (opzionale): Categoria del termine (es: nome proprio, slang)<br>`always_include` (opzionale): Includi nel prompt anche senza corrispondenza di parole chiave (predefinito: `false`) |
| `/list-glossary` | Visualizza le voci del glossario di questo server | Nessuna |
| `/remove-glossary`| Rimuove una voce dal glossario | `term` (obbligatorio): Termine da rimuovere |
| `/edit-forum-tags` | Modifica le mappature dei tag per canali forum o media | `group` (obbligatorio): Nome del gruppo<br>`channel` (opzionale): Canale forum target |
| `/bot-whitelist` | Gestisce la whitelist per bot e webhook automatizzati | Sottocomandi: `add`, `remove`, `list`<br>`source_type`: `bot` o `webhook`<br>`source_id`: ID utente del bot o ID del webhook |

#### Comando Messaggio (Disponibile per tutti gli utenti)
- **`View Original` (Menu Applicazioni)**
  Fai clic destro o tieni premuto su un messaggio → **«Applicazioni» → «View Original»** per ricevere un link diretto e un'anteprima del testo originale.

---

### Personalizzazione Avanzata

#### 1. Stile di Traduzione (`/set-style`)
Adatta il tono della traduzione all'atmosfera della tua community (`preset` e `custom` sono mutuamente esclusivi):

| Preimpostazione | Descrizione e Utilizzo |
|---|---|
| `default` | Tono conversazionale naturale usato dai madrelingua nelle chat |
| `casual` | Tono amichevole e rilassato adatto a gruppi di amici |
| `gaming` | Gergo videoludico e stile community gaming |
| `friendly` | Tono caloroso, educato e cordiale |
| `business` | Tono conciso, professionale e formale |
| `formal` | Linguaggio formale con formule di cortesia |
| `netslang` | Gergo di internet e stile forum |
| `tweet` | Frasi brevi e d'impatto in stile social media (X / Twitter) |
| `literal` | Traduzione letterale quando esistono più interpretazioni |

#### 2. Glossario del Server (`/add-glossary`)
Imposta traduzioni fisse per nomi di personaggi, termini di gioco o gergo interno del server (fino a 50 voci per server):
- **Attributi (`attribute`)**: Categorie come "nome di persona", "luogo", "slang", "abbreviazione" o "termine tecnico" aiutano l'IA a comprendere il contesto.
- **Includi sempre (`always_include`)**: Se impostato su `true`, il termine sarà sempre inviato nel contesto del prompt anche se non appare esplicitamente nel messaggio.

#### 3. Mappatura dei Tag dei Forum (`/edit-forum-tags`)
Quando colleghi canali forum, puoi associare i tag tra le diverse lingue. Quando viene creato un post con tag, il post speculare riceverà automaticamente il tag corrispondente.

#### 4. Whitelist per Messaggi Automatici (`/bot-whitelist`)
Per impostazione predefinita, i messaggi di bot e webhook vengono ignorati per evitare loop infiniti. Con `/bot-whitelist add` puoi autorizzare bot di annunci, feed RSS o notifiche.

---

## 2. Guida per Sviluppatori e Self-Hosting

### Requisiti e Stack Tecnologico

- **Linguaggio**: Go 1.24 o superiore
- **Database**: SQLite (Driver puro Go tramite `modernc.org/sqlite`, senza CGO)
- **Libreria Discord**: `github.com/bwmarrin/discordgo`
- **Motore di Traduzione**: API Chat Completions compatibile con OpenAI (OpenAI, OpenRouter, Azure OpenAI, LLM locali ecc.)
- **Compilazione Incrociata**: Pieno supporto con `CGO_ENABLED=0` per binari standalone per Linux, Windows e macOS.

---

### Installazione e Avvio

#### 1. Creare il Bot Discord
1. Accedi al [Discord Developer Portal](https://discord.com/developers/applications) e crea una nuova Application.
2. Nella scheda **Bot**, attiva `MESSAGE CONTENT INTENT` e copia il Bot Token.
3. In **OAuth2 → URL Generator**, seleziona i permessi e gli ambiti `bot` e `applications.commands`, quindi invita il bot al server.

#### 2. Preparare un'API compatibile con OpenAI
Ottieni endpoint URL, API key e model ID dal tuo fornitore LLM preferito.

#### 3. Configurare le Variabili d'Ambiente
Copia `.env.example` in `.env` e configura i parametri:

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

#### 4. Compilare ed Eseguire

Esecuzione diretta in locale:
```sh
go run ./cmd/discord-auto-translator
```

Compilazione ed esecuzione del binario standalone:
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**Verifica preventiva del modello (`--model-prewarm`)**:
Verifica credenziali, connettività e schema di risposta prima del deployment:
```sh
./discord-auto-translator --model-prewarm
```

---

### Riferimento delle Variabili d'Ambiente

| Variabile | Obbligatorio | Predefinito | Descrizione |
|---|---|---|---|
| `DISCORD_TOKEN` | **Sì** | - | Token di autenticazione del bot Discord |
| `OPENAI_BASE_URL` | **Sì** | - | URL di base dell'API Chat Completions (es: `https://api.openai.com/v1`) |
| `OPENAI_API_KEY` | **Sì** | - | Chiave API Bearer |
| `OPENAI_MODEL` | **Sì** | - | ID del modello presso il fornitore LLM |
| `OPENAI_REASONING_EFFORT` | No | (non impostato) | Parametro `reasoning_effort`. Impostare su `none` per disattivare i token di pensiero sui modelli ibridi |
| `DB_PATH` | No | `./translator.db` | Percorso del file di database SQLite |
| `HTTP_ADDR` | No | `:8080` | Indirizzo del server HTTP per i badge avatar |
| `PUBLIC_BASE_URL` | No | (non impostato) | URL pubblico per i badge avatar. Mostra un anello con il colore del ruolo più alto |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | No | `100000` | Limite di token per server al minuto |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | No | `120` | Limite di richieste al minuto per IP per `/avatar` |
| `MESSAGE_LINK_RETENTION_DAYS` | No | `0` | Giorni di conservazione dei link dei messaggi. `0` = illimitato |
| `GUILD_DATA_RETENTION_DAYS` | No | `0` | Giorni di conservazione dei dati di un server dopo l'uscita del bot |

---

### Architettura e Principi di Progettazione

#### 1. Pipeline di Traduzione
1. **Assemblaggio del Contesto**: Raccoglie argomento del canale, cronologia recente della conversazione, citazioni di risposta, metadati OGP e immagini ridimensionate.
2. **Mascheramento con Segnaposto**: Sostituisce menzioni (`<@id>`), emoji (`<:name:id>`), canali (`<#id>`), URL e blocchi di codice con token (`[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]`) per prevenire iniezioni di prompt.
3. **Composizione del Prompt e Caching**: Suddivide il prompt in sezioni stabili e dinamiche per sfruttare il Prefix Prompt Caching dei provider AI.
4. **Generazione con Structured Outputs**: Utilizza `response_format.type=json_schema` (`strict: true`) per ottenere tutte le traduzioni in una singola chiamata strutturata in JSON.
5. **Post-elaborazione e Distribuzione**: Ripristina i segnaposto, riscrive i link interni di Discord, sostituisce gli URL `hreflang` e invia i messaggi tramite Webhook in parallelo.

#### 2. Sicurezza e Principio Fail-Closed
- **Difesa da Prompt Injection**: Tutti i contenuti utente vengono convertiti in XML ed elaborati all'interno di tag dedicati.
- **Comportamento Fail-Closed**: In caso di superamento dei token (`finish_reason=length`), JSON non valido o errori di rete, il bot non invia messaggi corrotti, ma pubblica una notifica di errore localizzata nel canale di origine.

#### 3. Affidabilità e Coerenza dei Dati
- **Idempotenza**: `message_links` e `processed_events` prevengono la duplicazione dei messaggi in caso di eventi Gateway ripetuti.
- **Transazioni di Compensazione**: Se il salvataggio su database fallisce dopo l'invio del Webhook, il messaggio Discord inviato viene immediatamente rimosso.
- **Sincronizzazione Bidirezionale**: Reazioni e messaggi fissati si sincronizzano nell'intero gruppo, indipendentemente dal canale in cui è avvenuta l'azione.

---

### Sviluppo e Test

#### Esecuzione dei Test
```sh
go test ./...
```

#### Catalogo Multilingue dell'Interfaccia (i18n)
Tutti i testi per gli utenti e le notifiche sono gestiti in `internal/translatorbot/ui_strings.go` per 13 lingue. L'aggiunta di nuove stringhe richiede la definizione in tutte le lingue, convalidata dal test `TestUIStringCatalogIsComplete`.

---

## 3. Licenza

Questo progetto è rilasciato sotto licenza [MIT License](LICENSE).
