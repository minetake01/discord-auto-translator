# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Um bot para Discord que permite que pessoas que falam idiomas diferentes conversem juntas no mesmo servidor.

Vincule um canal por idioma formando um **grupo de tradução**. Cada mensagem postada em um canal é traduzida instantaneamente pelo Google Gemini e espelhada em todos os outros canais do grupo — mantendo o nome e o avatar do autor original — para que cada canal pareça uma conversa natural no seu próprio idioma.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-pt (Português)
```

## Funcionalidades

- **Tudo fica sincronizado** — não só novas mensagens: edições, exclusões, respostas, mensagens encaminhadas, reações, fixações, tópicos (canais de texto / fórum / mídia) e mensagens com apenas anexos são todos espelhados pelo grupo.
- **As mensagens parecem enviadas pelo remetente** — as mensagens espelhadas são entregues via webhooks com o nome e avatar do autor original.
- **Traduções naturais** — o Gemini usa o nome do canal, o tópico e o histórico recente de conversa como contexto; um glossário por servidor permite fixar traduções preferidas para nomes e jargões.
- **Tratamento inteligente de links** — links e menções apontando para canais ou mensagens gerenciados são reescritos para seus equivalentes em cada idioma, e URLs com alternativas `hreflang` são substituídas pela versão no idioma de destino.
- **Eficiente e seguro** — mensagens sem texto para traduzir (URLs, menções, emojis personalizados, código) são espelhadas sem chamar a API do Gemini; limites de taxa de tokens por servidor se aplicam; URLs, menções e blocos de código são protegidos contra injeção de prompt.
- **Interface localizada** — as respostas dos comandos seguem o idioma do cliente Discord do usuário, e as notificações de canal usam o idioma configurado para o canal (13 idiomas, inglês como fallback).

## Requisitos

- Go 1.24 ou superior
- Uma conta de bot do Discord com o intent privilegiado `MESSAGE CONTENT` habilitado
- Uma chave de API do Google Gemini

## Configuração

### 1. Preparar o bot do Discord

1. Crie um aplicativo no [Discord Developer Portal](https://discord.com/developers/applications)
2. Na página **Bot**:
   - Habilite o `MESSAGE CONTENT INTENT` (obrigatório)
   - Copie o token do bot
3. Convide o bot para o seu servidor via **OAuth2 → URL Generator**:
   - Scopes: `bot`, `applications.commands`
   - Permissões (conforme exibidas no Developer Portal):
     - **Geral**: `View Channel`, `Read Message History`
     - **Mensagens**: `Send Messages`, `Send Messages in Threads`
     - **Moderação**: `Pin Messages`
     - **Webhooks**: `Manage Webhooks`
     - **Tópicos**: `Create Public Threads`, `Manage Threads`
     - **Reações**: `Add Reactions`
   - O inteiro de permissões para o acima é `2252126768139328`
   - Para sincronizar também reações de emojis personalizados de outros servidores, conceda adicionalmente `Use External Emojis`; o inteiro de permissões passa a ser `2252126768401472`

### 2. Obter uma chave de API do Gemini

Obtenha uma chave de API no [Google AI Studio](https://aistudio.google.com/).

### 3. Configurar variáveis de ambiente

```sh
cp .env.example .env
```

Edite o `.env` e defina o seguinte:

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

| Variável | Obrigatório | Descrição |
|---|---|---|
| `DISCORD_TOKEN` | Sim | Token do bot do Discord |
| `GEMINI_API_KEY` | Sim | Chave de API do Gemini |
| `DB_PATH` | Não | Caminho para o arquivo SQLite (padrão: `./translator.db`) |
| `HTTP_ADDR` | Não | Endereço do servidor de badge de avatar (padrão: `:8080`) |
| `PUBLIC_BASE_URL` | Não | URL base pública para badges de anel em avatares. Se não definida, mensagens espelhadas usam a URL de avatar original do Discord e o servidor de badge não é utilizado |
| `GEMINI_RATE_LIMIT_TOKENS_PER_MIN` | Não | Limite de tokens do Gemini por servidor por minuto (padrão: `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | Não | Limite de requisições por IP por minuto para o endpoint de badge `/avatar` (padrão: `120`) |
| `MESSAGE_LINK_RETENTION_DAYS` | Não | Dias de retenção de `message_links` no SQLite antes da limpeza automática. `0` (padrão) desativa a limpeza; ex.: `60` remove links com mais de 60 dias na inicialização e a cada 24 horas |

### 4. Executar

```sh
go run ./cmd/discord-auto-translator
```

Ou compilar e executar:

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## Uso

Assim que o bot iniciar, os comandos de barra serão registrados em cada servidor.

### Configurar canais

#### Criar um grupo de tradução

Execute `/new-channel` no seu canal em japonês para criar um grupo de tradução:

```
/new-channel language:ja
```

#### Adicionar canais em outros idiomas

Execute `/join-channel` no seu canal em inglês para adicioná-lo ao grupo:

```
/join-channel group:general language:en
```

Para adicionar também um canal em português:

```
/join-channel group:general language:pt-BR
```

Agora `#general-ja`, `#general-en` e `#general-pt` estão vinculados.

### Comandos

Por padrão, os comandos de barra de administração só podem ser executados por **administradores do servidor**. Para permitir que outros cargos os usem, vá em "Configurações do servidor" no Discord → "Integrações" → "Gerenciar" do bot → "Permissões de comandos" e configure o acesso globalmente ou por comando. O bot nunca altera cargos ou permissões de comandos por conta própria.

| Comando | Descrição |
|---|---|
| `/new-channel language:[idioma] channel:<canal> group:<grupo>` | Criar um novo grupo de tradução. `channel` usa o canal atual por padrão; `group` usa o nome do canal por padrão |
| `/join-channel group:[grupo] language:[idioma] channel:<canal>` | Adicionar um canal a um grupo. `channel` usa o canal atual por padrão |
| `/leave-channel group:[grupo] channel:<canal>` | Remover um canal de um grupo. `channel` usa o canal atual por padrão |
| `/delete-group group:[grupo]` | Excluir um grupo inteiro |
| `/list-groups` | Listar os grupos de tradução e seus canais neste servidor |
| `/add-glossary term:[termo] translation:[tradução] attribute:<atributo> always_include:<bool>` | Registrar uma tradução preferida no glossário do servidor. `attribute` é texto livre com sugestões; `always_include` tem padrão `false` |
| `/list-glossary` | Listar o glossário do servidor |
| `/remove-glossary term:[termo]` | Remover uma entrada do glossário |
| `/set-style group:[grupo] preset:<predefinição> custom:<instrução personalizada>` | Definir o estilo de tradução de um grupo. Especifique `preset` ou `custom`, não ambos |
| `/bot-whitelist add source_type:[bot\|webhook] source_id:[ID]` | Permitir uma fonte de mensagens automatizada neste servidor. Com `source_type:bot`, `source_id` é o ID de usuário do bot; com `source_type:webhook`, é o ID do webhook |
| `/bot-whitelist remove source_type:[bot\|webhook] source_id:[ID]` | Remover a fonte de mensagens automatizada correspondente da lista de permissões deste servidor |
| `/bot-whitelist list` | Listar as fontes de bot e webhook permitidas neste servidor |

- As listas de fontes permitidas são persistidas no SQLite e limitadas a cada servidor Discord (guild). Webhooks de saída gerenciados pelo tradutor e mensagens do próprio bot tradutor permanecem excluídos mesmo que seus IDs sejam adicionados

- `language` usa códigos BCP-47 (`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr`, etc.)
- Máximo de 50 entradas de glossário por servidor
- `attribute` sugere "nome de pessoa", "nome de lugar", "gíria", "abreviação" e "termo técnico", mas qualquer valor pode ser inserido livremente. O atributo é usado como contexto para o Gemini entender o significado do termo
- Termos normais são adicionados às instruções do sistema apenas quando o corpo da mensagem a traduzir contém `term` (sem distinção de maiúsculas). Termos com `always_include:true` são sempre adicionados
- Se a opção `channel` for omitida, o comando se aplica ao canal em que foi executado
- Tipos de canal suportados: texto, notícias, fórum e mídia

## Testes

```sh
go test ./...
```

## Implantação no GCE

Um script de implantação para o Google Compute Engine está incluído em `deploy/deploy-gce.ps1` (Windows PowerShell).

Crie `deploy/deploy.json` a partir do exemplo para as configurações de conexão GCE. Configurações do app e segredos usam `.env` por padrão; outro arquivo pode ser indicado via `envFile` em `deploy.json` ou `-EnvFile`.

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# Editar deploy.json e .env

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # Configuração inicial
.\deploy\deploy-gce.ps1                          # Apenas atualizações de código
.\deploy\deploy-gce.ps1 -UploadEnv               # Atualizar segredos
```

## Licença

Consulte o arquivo [LICENSE](LICENSE) para a licença deste projeto.
