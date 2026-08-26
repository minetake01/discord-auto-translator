# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Un bot de Discord que permite a personas que hablan diferentes idiomas comunicarse en tiempo real dentro del mismo servidor, cada una usando su idioma nativo.

Al vincular un canal por idioma en un **grupo de traducción**, cualquier mensaje publicado en un canal se traduce automáticamente mediante un LLM compatible con OpenAI (Chat Completions API) y se reenvía a todos los demás canales del grupo con el **nombre y avatar del autor original**. Así, cada canal se lee como una conversación fluida en su propio idioma.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. Guía para Usuarios y Administradores

### Características Principales y Experiencia de Usuario

- **Chatea con total naturalidad**
  Sin necesidad de comandos especiales ni prefijos. Escribe y envía mensajes como de costumbre: se traducirán y sincronizarán automáticamente con los demás canales en tiempo real.
- **Los mensajes conservan la identidad del remitente**
  Los mensajes traducidos se envían a través de Webhooks de Discord, preservando el nombre y avatar del autor original.
- **Sincronización bidireccional completa en tiempo real**
  - **Nuevos mensajes y archivos adjuntos**: Soporta texto, imágenes (con descripciones/texto alternativo) y diversos archivos adjuntos.
  - **Ediciones y eliminaciones**: Al editar o borrar un mensaje original, las versiones traducidas se actualizan o eliminan al instante.
  - **Respuestas (Replies)**: Cita un fragmento del mensaje referenciado en el idioma de destino y enlaza al mensaje correspondiente (pseudo-respuesta).
  - **Mensajes reenviados**: Mantiene el contexto reenviado con un encabezado localizado.
  - **Reacciones y fijados (Pins)**: Agregar/quitar reacciones de emoji y fijar mensajes se sincroniza bidireccionalmente.
  - **Hilos y foros**: Soporta hilos regulares, canales de foro y canales de medios, incluyendo el mapeo automático de etiquetas.
  - **Encuestas (Polls)**: Traduce las preguntas y opciones en formato Embed y publica los resultados finales al concluir la encuesta.
- **Función «View Original» (Ver original)**
  Haz clic derecho (o mantén presionado en móvil) sobre cualquier mensaje traducido y selecciona **«Aplicaciones» → «View Original»** para obtener un enlace efímero al mensaje fuente y una vista previa del texto original.
- **Tratamiento inteligente de enlaces y multimedia**
  - Los enlaces y menciones a canales, mensajes o hilos gestionados se reescriben automáticamente para apuntar a sus contrapartes en el idioma de destino.
  - Las URL externas con versiones `hreflang` se reemplazan automáticamente por la versión en el idioma de destino.

---

### Pasos para Añadir el Bot al Servidor

#### 1. Invitar al Bot
Genera un enlace de invitación en el Discord Developer Portal con los siguientes permisos:

- **OAuth2 Scopes**: `bot`, `applications.commands`
- **Permisos del Bot (Bot Permissions)**:
  - **General**: `View Channels` (Ver canales), `Read Message History` (Leer el historial de mensajes)
  - **Texto**: `Send Messages` (Enviar mensajes), `Send Messages in Threads` (Enviar mensajes en hilos)
  - **Moderación**: `Pin Messages` (Fijar mensajes)
  - **Webhooks**: `Manage Webhooks` (Gestionar webhooks)
  - **Hilos**: `Create Public Threads` (Crear hilos públicos), `Manage Threads` (Gestionar hilos)
  - **Reacciones**: `Add Reactions` (Añadir reacciones)
- **Permisos en número entero (Permissions Integer)**: `2252126768139328`
  - *Nota: Para sincronizar reacciones de emojis personalizados de otros servidores, activa `Use External Emojis` (Permissions Integer: `2252126768401472`).*

#### 2. Activar Intent Privilegiado
Asegúrate de que `MESSAGE CONTENT INTENT` esté habilitado en la pestaña **Bot** del Discord Developer Portal.

---

### Configuración de Canales (Operaciones Básicas)

#### Crear un grupo de traducción
En tu canal en japonés (p. ej. `#general-ja`), ejecuta `/new-channel`:

```
/new-channel language:ja
```
*Nota: Si se omite `group`, se usará el nombre del canal actual como identificador.*

#### Añadir canales en otros idiomas
En tu canal en inglés (p. ej. `#general-en`), ejecuta `/join-channel`:

```
/join-channel group:general language:en
```

Para añadir un canal en español (p. ej. `#general-es`):

```
/join-channel group:general language:es
```

Ahora `#general-ja`, `#general-en` y `#general-es` están enlazados y la traducción automática está activa.

#### Salir de un grupo y eliminar grupos
- Quitar un canal del grupo: `/leave-channel group:general`
- Eliminar un grupo por completo: `/delete-group group:general`
- Ver los grupos y canales activos: `/list-groups`

---

### Referencia de Comandos

#### Comandos Slash de Administración
Por defecto, los comandos de administración solo pueden ser ejecutados por miembros con **permisos de Administrador**. Para autorizar roles adicionales, configúralo en Discord: **Ajustes del servidor → Integraciones → (Nombre del bot) → Gestionar → Permisos de comandos**.

| Comando | Descripción | Opciones principales |
|---|---|---|
| `/new-channel` | Crea un nuevo grupo de traducción y registra un canal | `language` (obligatorio): Código de idioma BCP-47<br>`channel` (opcional): Canal objetivo (por defecto: canal actual)<br>`group` (opcional): Nombre del grupo (por defecto: nombre del canal) |
| `/join-channel` | Añade un canal a un grupo existente | `group` (obligatorio): Nombre del grupo<br>`language` (obligatorio): Código de idioma BCP-47<br>`channel` (opcional): Canal objetivo (por defecto: canal actual) |
| `/leave-channel` | Retira un canal de un grupo | `group` (obligatorio): Nombre del grupo<br>`channel` (opcional): Canal objetivo (por defecto: canal actual) |
| `/delete-group` | Elimina un grupo de traducción por completo | `group` (obligatorio): Nombre del grupo a eliminar |
| `/list-groups` | Lista todos los grupos y canales vinculados | Ninguna |
| `/set-style` | Establece el tono o estilo de traducción del grupo | `group` (obligatorio): Nombre del grupo<br>`preset` (opcional): Preajuste de estilo (ver abajo)<br>`custom` (opcional): Instrucción personalizada en lenguaje natural (máx. 200 caracteres) |
| `/add-glossary` | Registra una traducción preferida en el glosario del servidor | `term` (obligatorio): Término de origen<br>`translation` (obligatorio): Traducción preferida<br>`attribute` (opcional): Categoría (p. ej. nombre propio, jerga)<br>`always_include` (opcional): Incluir en el prompt sin coincidencia de palabra clave (por defecto: `false`) |
| `/list-glossary` | Muestra las entradas del glosario de este servidor | Ninguna |
| `/remove-glossary`| Elimina una entrada del glosario | `term` (obligatorio): Término a eliminar |
| `/edit-forum-tags` | Modifica el mapeo de etiquetas en canales de foro o medios | `group` (obligatorio): Nombre del grupo<br>`channel` (opcional): Canal de foro objetivo |
| `/bot-whitelist` | Administra la lista blanca para bots y webhooks automáticos | Subcomandos: `add`, `remove`, `list`<br>`source_type`: `bot` o `webhook`<br>`source_id`: ID de usuario del bot o ID del webhook |

#### Comando de Mensaje (Disponible para todos los usuarios)
- **`View Original` (Menú de aplicaciones)**
  Haz clic derecho o mantén presionado un mensaje → **«Aplicaciones» → «View Original»** para ver el enlace directo y el fragmento del mensaje original.

---

### Personalización Avanzada

#### 1. Estilo de Traducción (`/set-style`)
Adapta el tono de la traducción al estilo de tu comunidad (`preset` y `custom` son mutuamente excluyentes):

| Preajuste | Descripción y uso |
|---|---|
| `default` | Tono conversacional natural usado por hablantes nativos en chats |
| `casual` | Tono informal y cercano entre amigos |
| `gaming` | Jerga de videojuegos y estilo de comunidades gamer |
| `friendly` | Tono cálido, educado y accesible |
| `business` | Tono conciso, profesional y formal para negocios |
| `formal` | Tono formal con fórmulas de cortesía y tratamiento de respeto |
| `netslang` | Jerga de internet y estilo de foros |
| `tweet` | Frases cortas e impactantes al estilo de redes sociales (X / Twitter) |
| `literal` | Traducción literal cuando existen múltiples interpretaciones |

#### 2. Glosario del Servidor (`/add-glossary`)
Define traducciones para nombres de personajes, términos de juegos o jerga propia de tu servidor (hasta 50 términos por servidor):
- **Atributos (`attribute`)**: Categorías como «nombre de persona», «lugar», «jerga», «abreviatura» o «término técnico» ayudan a la IA a comprender mejor el contexto.
- **Incluir siempre (`always_include`)**: Si se activa en `true`, el término se incluirá siempre como contexto, incluso si no aparece textualmente en el mensaje.

#### 3. Mapeo de Etiquetas de Foro (`/edit-forum-tags`)
Al vincular canales de foro, puedes mapear etiquetas entre idiomas. Al publicar una entrada con etiqueta, el post espejo recibirá la etiqueta correspondiente de forma automática.

#### 4. Lista Blanca de Mensajes Automáticos (`/bot-whitelist`)
Por defecto, los mensajes de bots y webhooks se ignoran para evitar bucles infinitos. Con `/bot-whitelist add` puedes autorizar bots de anuncios, feeds RSS o integraciones.

---

## 2. Guía de Desarrollo y Auto-Alojamiento (Self-Hosting)

### Requisitos y Stack Tecnológico

- **Lenguaje**: Go 1.24 o superior
- **Base de datos**: SQLite (Driver puro en Go mediante `modernc.org/sqlite`, sin CGO)
- **Biblioteca Discord**: `github.com/bwmarrin/discordgo`
- **Motor de traducción**: API Chat Completions compatible con OpenAI (OpenAI, OpenRouter, Azure OpenAI, LLMs locales, etc.)
- **Compilación cruzada**: Totalmente compatible con `CGO_ENABLED=0` para binarios únicos en Linux, Windows y macOS.

---

### Configuración y Puesta en Marcha

#### 1. Crear el Bot de Discord
1. Entra en el [Discord Developer Portal](https://discord.com/developers/applications) y crea una nueva aplicación.
2. En la pestaña **Bot**, activa `MESSAGE CONTENT INTENT` y copia el Bot Token.
3. En **OAuth2 → URL Generator**, selecciona `bot` y `applications.commands` con los permisos necesarios e invita al bot a tu servidor.

#### 2. Preparar una API compatible con OpenAI
Obtén la URL del endpoint, la clave de API y el ID del modelo de tu proveedor LLM preferido.

#### 3. Configurar Variables de Entorno
Copia `.env.example` a `.env` y define los parámetros requeridos:

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

#### 4. Compilar y Ejecutar

Ejecución directa en desarrollo:
```sh
go run ./cmd/discord-auto-translator
```

Compilar un binario independiente y ejecutar:
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**Verificación previa del modelo (`--model-prewarm`)**:
Valida credenciales, conexión con el modelo y el esquema de respuesta antes de iniciar en producción:
```sh
./discord-auto-translator --model-prewarm
```

---

### Referencia de Variables de Entorno

| Variable | Obligatorio | Por defecto | Descripción |
|---|---|---|---|
| `DISCORD_TOKEN` | **Sí** | - | Token de autenticación del bot de Discord |
| `OPENAI_BASE_URL` | **Sí** | - | URL base de la API Chat Completions (p. ej. `https://api.openai.com/v1`) |
| `OPENAI_API_KEY` | **Sí** | - | Clave de API Bearer |
| `OPENAI_MODEL` | **Sí** | - | ID del modelo en el proveedor LLM |
| `OPENAI_REASONING_EFFORT` | No | (no definido) | Parámetro `reasoning_effort`. Configurar en `none` para omitir tokens de pensamiento en modelos híbridos |
| `DB_PATH` | No | `./translator.db` | Ruta del archivo de base de datos SQLite |
| `HTTP_ADDR` | No | `:8080` | Dirección del servidor HTTP de insignias de avatar |
| `PUBLIC_BASE_URL` | No | (no definido) | URL pública para las insignias de avatar. Renderiza un anillo con el color del rol más alto |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | No | `100000` | Límite de tokens por servidor (guild) por minuto |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | No | `120` | Límite de peticiones por minuto por IP para `/avatar` |
| `MESSAGE_LINK_RETENTION_DAYS` | No | `0` | Días de retención de enlaces de mensajes. `0` = indefinido |
| `GUILD_DATA_RETENTION_DAYS` | No | `0` | Días de retención de datos de un servidor tras la salida del bot |

---

### Arquitectura y Principios de Diseño

#### 1. Pipeline de Traducción
1. **Ensamblaje del Contexto**: Recopila el tema del canal, historial reciente de conversación, referencias de respuestas, metadatos OGP y fotos redimensionadas.
2. **Enmascaramiento con Marcadores**: Reemplaza menciones (`<@id>`), emojis (`<:name:id>`), canales (`<#id>`), URLs y bloques de código por tokens (`[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]`) para evitar inyecciones de prompt.
3. **Composición de Prompts y Caché**: Organiza el prompt en capas estables y dinámicas para aprovechar la caché de prefijos (Prefix Prompt Caching) de los proveedores.
4. **Generación con Structured Outputs**: Utiliza `response_format.type=json_schema` (`strict: true`) para obtener todas las traducciones en una sola llamada estructurada en JSON.
5. **Post-procesamiento y Entrega**: Restaura marcadores, reescribe enlaces internos de Discord, reemplaza URLs `hreflang` y envía los mensajes mediante Webhooks de forma concurrente.

#### 2. Seguridad y Comportamiento Fail-Closed
- **Defensa contra Prompt Injection**: Todo el contenido del usuario se escapa en XML y se aísla dentro de etiquetas de contexto.
- **Principio Fail-Closed**: Si se excede el límite de tokens (`finish_reason=length`), el JSON es inválido o hay errores de red, el bot cancela el reenvío y envía un aviso de error localizado en el canal de origen en lugar de publicar contenido corrupto.

#### 3. Fiabilidad y Consistencia de Datos
- **Idempotencia**: `message_links` y `processed_events` previenen la duplicación de mensajes ante eventos repetidos del Gateway.
- **Transacciones de Compensación**: Si falla el guardado en base de datos tras enviar un Webhook, el mensaje de Discord se elimina inmediatamente.
- **Sincronización Bidireccional**: Las reacciones y mensajes fijados se sincronizan en todo el grupo, sin importar en qué canal se originó la acción.

---

### Desarrollo y Pruebas

#### Ejecutar pruebas
```sh
go test ./...
```

#### Catálogo Multilingüe de UI (i18n)
Todos los textos visibles y notificaciones se gestionan en `internal/translatorbot/ui_strings.go` para 13 idiomas. Añadir nuevos textos requiere definir las traducciones en todos los idiomas, validado por `TestUIStringCatalogIsComplete`.

---

## 3. Licencia

Este proyecto está bajo la licencia [MIT License](LICENSE).
