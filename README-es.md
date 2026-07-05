# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Un bot de Discord que permite a personas que hablan diferentes idiomas chatear juntas en el mismo servidor.

Vincula un canal por idioma formando un **grupo de traducción**. Cada mensaje publicado en un canal se traduce al instante con Google Gemini y se replica en todos los demás canales del grupo, conservando el nombre y el avatar del autor original, de modo que cada canal se lee como una conversación natural en su propio idioma.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-es (Español)
```

## Funcionalidades

- **Todo permanece sincronizado** — no solo los mensajes nuevos: ediciones, eliminaciones, respuestas, mensajes reenviados, reacciones, anclajes, hilos (canales de texto / foro / multimedia) y mensajes con solo archivos adjuntos se replican en todo el grupo.
- **Los mensajes parecen enviados por su autor** — los mensajes replicados se envían mediante webhooks con el nombre y el avatar del autor original.
- **Traducciones naturales** — Gemini usa el nombre del canal, el tema y el historial reciente de la conversación como contexto; un glosario por servidor permite fijar las traducciones preferidas para nombres y jerga.
- **Gestión inteligente de enlaces** — los enlaces y menciones que apuntan a canales o mensajes gestionados se reescriben hacia sus equivalentes en cada idioma, y las URL con alternativas `hreflang` se sustituyen por la versión en el idioma de destino.
- **Eficiente y seguro** — los mensajes sin texto traducible (URL, menciones, emojis personalizados, código) se replican sin llamar a la API de Gemini; se aplican límites de tasa de tokens por servidor; las URL, menciones y bloques de código están protegidos contra inyección de prompts.
- **Interfaz localizada** — las respuestas a los comandos siguen el idioma del cliente de Discord del usuario, y las notificaciones de canal usan el idioma configurado para ese canal (13 idiomas, inglés como respaldo).

## Requisitos

- Go 1.24 o superior
- Una cuenta de bot de Discord con el intent privilegiado `MESSAGE CONTENT` habilitado
- Una clave de API de Google Gemini

## Configuración

### 1. Preparar el bot de Discord

1. Crea una aplicación en el [Discord Developer Portal](https://discord.com/developers/applications)
2. En la página **Bot**:
   - Habilita el `MESSAGE CONTENT INTENT` (obligatorio)
   - Copia el token del bot
3. Invita el bot a tu servidor mediante **OAuth2 → URL Generator**:
   - Scopes: `bot`, `applications.commands`
   - Permisos (tal como aparecen en el Developer Portal):
     - **General**: `View Channel`, `Read Message History`
     - **Mensajes**: `Send Messages`, `Send Messages in Threads`
     - **Moderación**: `Pin Messages`
     - **Webhooks**: `Manage Webhooks`
     - **Hilos**: `Create Public Threads`, `Manage Threads`
     - **Reacciones**: `Add Reactions`
   - El entero de permisos para lo anterior es `2252126768139328`
   - Para sincronizar también reacciones de emojis personalizados de otros servidores, concede además `Use External Emojis`; el entero de permisos pasará a ser `2252126768401472`

### 2. Obtener una clave de API de Gemini

Obtén una clave de API en [Google AI Studio](https://aistudio.google.com/).

### 3. Configurar las variables de entorno

```sh
cp .env.example .env
```

Edita `.env` y establece lo siguiente:

```env
DISCORD_TOKEN=your-discord-bot-token
GEMINI_API_KEY=your-gemini-api-key
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
GEMINI_RATE_LIMIT_TOKENS_PER_MIN=100000
```

| Variable | Obligatorio | Descripción |
|---|---|---|
| `DISCORD_TOKEN` | Sí | Token del bot de Discord |
| `GEMINI_API_KEY` | Sí | Clave de API de Gemini |
| `DB_PATH` | No | Ruta al archivo SQLite (predeterminado: `./translator.db`) |
| `HTTP_ADDR` | No | Dirección del servidor de insignia de avatar (predeterminado: `:8080`) |
| `PUBLIC_BASE_URL` | No | URL base para añadir una insignia de anillo naranja a los avatares |
| `GEMINI_RATE_LIMIT_TOKENS_PER_MIN` | No | Límite de tokens de Gemini por servidor y por minuto (predeterminado: `100000`) |

### 4. Ejecutar

```sh
go run ./cmd/discord-auto-translator
```

O compilar y ejecutar:

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## Uso

Una vez que el bot se inicia, los comandos de barra diagonal quedan registrados en cada servidor.

### Configurar los canales

#### Crear un grupo de traducción

Ejecuta `/new-channel` en tu canal japonés para crear un grupo de traducción:

```
/new-channel language:ja
```

#### Añadir canales en otros idiomas

Ejecuta `/join-channel` en tu canal en inglés para añadirlo al grupo:

```
/join-channel group:general language:en
```

Para añadir también un canal en español:

```
/join-channel group:general language:es
```

Ahora `#general-ja`, `#general-en` y `#general-es` están vinculados.

### Comandos

Por defecto, los comandos de barra diagonal de administración solo pueden ejecutarlos los **administradores del servidor**. Para permitir que otros roles los usen, ve a "Configuración del servidor" en Discord → "Integraciones" → "Gestionar" del bot → "Permisos de comandos" y configura el acceso de forma global o por comando. El bot nunca modifica roles ni permisos de comandos por su cuenta.

| Comando | Descripción |
|---|---|
| `/new-channel language:[idioma] channel:<canal> group:<grupo>` | Crear un nuevo grupo de traducción. `channel` usa el canal actual por defecto; `group` usa el nombre del canal por defecto |
| `/join-channel group:[grupo] language:[idioma] channel:<canal>` | Añadir un canal a un grupo. `channel` usa el canal actual por defecto |
| `/leave-channel group:[grupo] channel:<canal>` | Eliminar un canal de un grupo. `channel` usa el canal actual por defecto |
| `/delete-group group:[grupo]` | Eliminar un grupo completo |
| `/list-groups` | Mostrar los grupos de traducción y sus canales en este servidor |
| `/add-glossary term:[término] translation:[traducción] attribute:<atributo> always_include:<bool>` | Registrar una traducción preferida en el glosario del servidor. `attribute` es de texto libre con sugerencias; `always_include` es `false` por defecto |
| `/list-glossary` | Mostrar el glosario del servidor |
| `/remove-glossary term:[término]` | Eliminar una entrada del glosario |
| `/set-style group:[grupo] preset:<preajuste> custom:<instrucción personalizada>` | Establecer el estilo de traducción de un grupo. Especificar `preset` o `custom`, no ambos |

- `language` usa códigos BCP-47 (`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr`, etc.)
- Máximo 50 entradas de glosario por servidor
- `attribute` sugiere "nombre de persona", "nombre de lugar", "argot", "abreviatura" y "término técnico", pero se puede introducir cualquier valor libremente. El atributo se usa como contexto para que Gemini entienda el significado del término
- Los términos normales se añaden a las instrucciones del sistema solo si el mensaje a traducir contiene `term` (sin distinción de mayúsculas). Los términos con `always_include:true` siempre se añaden
- Si se omite la opción `channel`, el comando se aplica al canal en el que se ejecutó
- Tipos de canal admitidos: texto, anuncios, foro y multimedia

## Pruebas

```sh
go test ./...
```

## Despliegue en GCE

Se incluye un script de despliegue para Google Compute Engine en `deploy/deploy-gce.ps1` (Windows PowerShell).

```powershell
# Configuración inicial (instala Caddy + systemd)
.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv

# Al actualizar el código
.\deploy\deploy-gce.ps1
```

## Licencia

Consulta el archivo [LICENSE](LICENSE) para conocer la licencia de este proyecto.
