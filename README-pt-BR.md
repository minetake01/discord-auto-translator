# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Um bot para Discord que permite que pessoas que falam idiomas diferentes conversem em tempo real no mesmo servidor, cada uma usando sua língua nativa.

Ao vincular um canal para cada idioma em um **grupo de tradução**, qualquer mensagem enviada em um canal é traduzida automaticamente por um LLM compatível com OpenAI (Chat Completions API) e espelhada para todos os outros canais do grupo com o **nome e avatar do autor original**. Assim, cada canal é lido como uma conversa fluida em seu próprio idioma.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. Guia para Usuários e Administradores

### Principais Recursos e Experiência do Usuário

- **Converse naturalmente como sempre**
  Sem comandos especiais ou prefixos. Apenas envie suas mensagens normalmente: elas serão traduzidas e sincronizadas em tempo real com os demais canais vinculados.
- **As mensagens mantêm a identidade do remetente**
  As mensagens traduzidas são enviadas via Webhooks do Discord, preservando o nome de exibição e o avatar do autor original.
- **Sincronização bidirecional completa em tempo real**
  - **Novas mensagens e anexos**: Suporta textos, imagens (incluindo descrições / texto alternativo) e diversos tipos de anexos.
  - **Edições e exclusões**: Editar ou apagar uma mensagem original atualiza ou exclui instantaneamente as versões traduzidas.
  - **Respostas (Replies)**: Cita um trecho da mensagem referenciada no idioma de destino e adiciona o link correspondente (pseudo-resposta).
  - **Mensagens encaminhadas**: Preserva o conteúdo encaminhado com um cabeçalho localizado.
  - **Reações e fixados (Pins)**: Adicionar/remover reações de emojis e fixar mensagens são ações sincronizadas bidirecionalmente.
  - **Tópicos (Threads) e fóruns**: Suporta tópicos comuns, canais de fórum e de mídia, incluindo mapeamento de tags.
  - **Enquetes (Polls)**: Traduz perguntas e opções em formato Embed e publica os resultados finais ao término da votação.
- **Recurso «View Original» (Ver Original)**
  Clique com o botão direito (ou pressione e segure no celular) em qualquer mensagem traduzida e selecione **«Aplicativos» → «View Original»** para receber um link direto e uma prévia do texto original (visível apenas para você).
- **Tratamento inteligente de links e mídias**
  - Links e menções para canais, mensagens ou tópicos gerenciados são reescritos automaticamente para os IDs correspondentes no idioma de destino.
  - URLs externas com versões `hreflang` são substituídas automaticamente pela URL correspondente ao idioma de destino.

---

### Como Adicionar ao Servidor

#### 1. Convidar o Bot
Gere um link de convite no Discord Developer Portal com as seguintes permissões:

- **OAuth2 Scopes**: `bot`, `applications.commands`
- **Permissões do Bot (Bot Permissions)**:
  - **Geral**: `View Channels` (Ver canais), `Read Message History` (Ver histórico de mensagens)
  - **Texto**: `Send Messages` (Enviar mensagens), `Send Messages in Threads` (Enviar mensagens em tópicos)
  - **Moderação**: `Pin Messages` (Fixar mensagens)
  - **Webhooks**: `Manage Webhooks` (Gerenciar webhooks)
  - **Tópicos**: `Create Public Threads` (Criar tópicos públicos), `Manage Threads` (Gerenciar tópicos)
  - **Reações**: `Add Reactions` (Adicionar reações)
- **Número Inteiro de Permissões (Permissions Integer)**: `2252126768139328`
  - *Observação: Para sincronizar também reações com emojis de outros servidores, marque `Use External Emojis` (Permissions Integer: `2252126768401472`).*

#### 2. Ativar Intent Privilegiada
Certifique-se de que a opção `MESSAGE CONTENT INTENT` está ativada na aba **Bot** do Discord Developer Portal.

---

### Configuração de Canais (Operações Básicas)

#### Criar um grupo de tradução
No canal em japonês (ex: `#general-ja`), execute `/new-channel`:

```
/new-channel language:ja
```
*Observação: Se `group` for omitido, o nome do canal atual será usado como identificador.*

#### Adicionar canais em outros idiomas
No canal em inglês (ex: `#general-en`), execute `/join-channel`:

```
/join-channel group:general language:en
```

Para adicionar um canal em português (ex: `#general-pt`):

```
/join-channel group:general language:pt-BR
```

Agora `#general-ja`, `#general-en` e `#general-pt` estão vinculados e a tradução automática está ativa.

#### Sair de um grupo e excluir grupos
- Remover um canal do grupo: `/leave-channel group:general`
- Excluir um grupo por completo: `/delete-group group:general`
- Listar grupos e canais ativos: `/list-groups`

---

### Referência de Comandos

#### Comandos Slash de Administração
Por padrão, os comandos de administração só podem ser executados por membros com **permissões de Administrador**. Para autorizar outros cargos, configure em: **Configurações do Servidor → Integrações → (Nome do bot) → Gerenciar → Permissões de Comandos**.

| Comando | Descrição | Principais Opções |
|---|---|---|
| `/new-channel` | Cria um novo grupo de tradução e registra um canal | `language` (obrigatório): Código BCP-47 do idioma<br>`channel` (opcional): Canal alvo (padrão: canal atual)<br>`group` (opcional): Nome do grupo (padrão: nome do canal) |
| `/join-channel` | Adiciona um canal a um grupo existente | `group` (obrigatório): Nome do grupo<br>`language` (obrigatório): Código BCP-47 do idioma<br>`channel` (opcional): Canal alvo (padrão: canal atual) |
| `/leave-channel` | Remove um canal de um grupo | `group` (obrigatório): Nome do grupo<br>`channel` (opcional): Canal alvo (padrão: canal atual) |
| `/delete-group` | Exclui um grupo de tradução por completo | `group` (obrigatório): Nome do grupo a excluir |
| `/list-groups` | Lista todos os grupos e canais vinculados | Nenhuma |
| `/set-style` | Define o tom ou estilo de tradução do grupo | `group` (obrigatório): Nome do grupo<br>`preset` (opcional): Predefinição de estilo (ver abaixo)<br>`custom` (opcional): Instrução personalizada em linguagem natural (máx. 200 caracteres) |
| `/add-glossary` | Registra uma tradução preferida no glossário do servidor | `term` (obrigatório): Termo de origem<br>`translation` (obrigatório): Tradução preferida<br>`attribute` (opcional): Categoria do termo (ex: nome de pessoa, gíria)<br>`always_include` (opcional): Incluir no prompt mesmo sem correspondência direta (padrão: `false`) |
| `/list-glossary` | Exibe as entradas do glossário do servidor | Nenhuma |
| `/remove-glossary`| Remove uma entrada do glossário | `term` (obrigatório): Termo a remover |
| `/edit-forum-tags` | Edita o mapeamento de tags para canais de fórum ou mídia | `group` (obrigatório): Nome do grupo<br>`channel` (opcional): Canal de fórum alvo |
| `/bot-whitelist` | Gerencia a lista de permissões para bots e webhooks automáticos | Subcomandos: `add`, `remove`, `list`<br>`source_type`: `bot` ou `webhook`<br>`source_id`: ID de usuário do bot ou ID do webhook |

#### Comando de Mensagem (Disponível para todos os usuários)
- **`View Original` (Menu de Aplicativos)**
  Clique com o botão direito ou segure em uma mensagem → **«Aplicativos» → «View Original»** para obter o link direto e uma prévia da mensagem original.

---

### Personalização Avançada

#### 1. Estilo de Tradução (`/set-style`)
Adapte o tom da tradução ao estilo da sua comunidade (`preset` e `custom` são mutuamente exclusivos):

| Predefinição | Descrição e Uso |
|---|---|
| `default` | Tom de conversa natural usado por falantes nativos em chats |
| `casual` | Tom descontraído e amigável para amigos e comunidades |
| `gaming` | Gírias de jogos e estilo de comunidades gamer |
| `friendly` | Tom acolhedor, educado e simpático |
| `business` | Tom conciso, profissional e formal |
| `formal` | Tom formal com tratamento respeitoso e linguagem polida |
| `netslang` | Gírias da internet e estilo de fóruns |
| `tweet` | Frases curtas e diretas no estilo de redes sociais (X / Twitter) |
| `literal` | Tradução literal quando houver múltiplas interpretações |

#### 2. Glossário do Servidor (`/add-glossary`)
Defina traduções para nomes de personagens, termos de jogos ou jargões da comunidade (até 50 termos por servidor):
- **Atributos (`attribute`)**: Categorias como "nome de pessoa", "lugar", "gíria", "abreviação" ou "termo técnico" ajudam a IA a entender o contexto.
- **Incluir Sempre (`always_include`)**: Quando definido como `true`, o termo será enviado no contexto do prompt mesmo que não apareça explicitamente na mensagem.

#### 3. Mapeamento de Tags de Fórum (`/edit-forum-tags`)
Ao conectar canais de fórum, você pode mapear tags entre idiomas. Quando uma postagem com tag for criada em um idioma, a postagem espelhada receberá automaticamente a tag correspondente.

#### 4. Lista de Permissão de Mensagens Automáticas (`/bot-whitelist`)
Por padrão, mensagens de bots e webhooks são ignoradas para evitar loops infinitos. Use `/bot-whitelist add` para permitir que bots de anúncios, feeds RSS ou integrações sejam traduzidos.

---

## 2. Guia para Desenvolvedores e Auto-Hospedagem (Self-Hosting)

### Requisitos e Stack Tecnológica

- **Linguagem**: Go 1.24 ou superior
- **Banco de Dados**: SQLite (Driver puro em Go via `modernc.org/sqlite`, sem CGO)
- **Biblioteca Discord**: `github.com/bwmarrin/discordgo`
- **Motor de Tradução**: API Chat Completions compatível com OpenAI (OpenAI, OpenRouter, Azure OpenAI, LLMs locais etc.)
- **Compilação Cruzada**: Totalmente suportada com `CGO_ENABLED=0` para gerar binários únicos para Linux, Windows e macOS.

---

### Como Hospedar e Executar

#### 1. Criar o Bot no Discord
1. Acesse o [Discord Developer Portal](https://discord.com/developers/applications) e crie uma nova Application.
2. Na aba **Bot**, ative `MESSAGE CONTENT INTENT` e copie o Bot Token.
3. Em **OAuth2 → URL Generator**, selecione os escopos `bot` e `applications.commands` com as permissões necessárias e convide o bot.

#### 2. Preparar uma API compatível com OpenAI
Obtenha a URL do endpoint, a chave de API e o ID do modelo do seu provedor LLM preferido.

#### 3. Configurar Variáveis de Ambiente
Copie `.env.example` para `.env` e preencha as configurações:

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

#### 4. Compilar e Executar

Executar diretamente no ambiente local:
```sh
go run ./cmd/discord-auto-translator
```

Compilar e executar o binário:
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**Validação prévia do modelo (`--model-prewarm`)**:
Verifica credenciais, conexão e conformidade do esquema de resposta antes da implantação:
```sh
./discord-auto-translator --model-prewarm
```

---

### Referência das Variáveis de Ambiente

| Variável | Obrigatório | Padrão | Descrição |
|---|---|---|---|
| `DISCORD_TOKEN` | **Sim** | - | Token de autenticação do bot no Discord |
| `OPENAI_BASE_URL` | **Sim** | - | URL base da API Chat Completions (ex: `https://api.openai.com/v1`) |
| `OPENAI_API_KEY` | **Sim** | - | Chave de API Bearer |
| `OPENAI_MODEL` | **Sim** | - | ID do modelo no provedor LLM |
| `OPENAI_REASONING_EFFORT` | Não | (não definido) | Parâmetro `reasoning_effort`. Defina como `none` para desativar tokens de pensamento em modelos híbridos |
| `DB_PATH` | Não | `./translator.db` | Caminho do arquivo de banco de dados SQLite |
| `HTTP_ADDR` | Não | `:8080` | Endereço de escuta do servidor HTTP de emblemas de avatar |
| `PUBLIC_BASE_URL` | Não | (não definido) | URL pública para os emblemas de avatar. Renderiza um anel com a cor do cargo mais alto |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | Não | `100000` | Limite de tokens por servidor por minuto |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | Não | `120` | Limite de requisições por minuto por IP para o endpoint `/avatar` |
| `MESSAGE_LINK_RETENTION_DAYS` | Não | `0` | Dias de retenção de links de mensagens. `0` = ilimitado |
| `GUILD_DATA_RETENTION_DAYS` | Não | `0` | Dias de retenção de dados de um servidor após a saída do bot |

---

### Arquitetura e Design

#### 1. Pipeline de Tradução
1. **Montagem do Contexto**: Coleta o tópico do canal, histórico de conversas recente, referências de respostas, metadados OGP e imagens redimensionadas.
2. **Mascaramento por Tokens**: Substitui menções (`<@id>`), emojis (`<:name:id>`), canais (`<#id>`), URLs e blocos de código por tokens (`[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]`) para evitar injeção de prompt.
3. **Composição e Cache do Prompt**: Estrutura o prompt em camadas estáveis e dinâmicas para aproveitar o Prefix Prompt Caching dos provedores.
4. **Geração com Structured Outputs**: Usa `response_format.type=json_schema` (`strict: true`) para gerar todas as traduções em uma única chamada de API.
5. **Pós-processamento e Envio**: Restaura os tokens, reescreve links internos do Discord, substitui URLs `hreflang` e envia as mensagens via Webhooks simultaneamente.

#### 2. Segurança e Comportamento Fail-Closed
- **Defesa contra Prompt Injection**: Todo o conteúdo do usuário passa por escape XML e é isolado em tags dedicadas.
- **Princípio Fail-Closed**: Em caso de limite de tokens excedido (`finish_reason=length`), JSON inválido ou falhas de rede, o bot não envia mensagens corrompidas e publica um aviso de erro localizado no canal de origem.

#### 3. Confiabilidade e Consistência de Dados
- **Idempotência**: `message_links` e `processed_events` evitam a duplicação de mensagens durante eventos repetidos do Gateway.
- **Transações de Compensação**: Se a gravação no banco falhar após o envio via Webhook, a mensagem no Discord é excluída imediatamente.
- **Sincronização Bidirecional**: Reações e mensagens fixadas são sincronizadas em todo o grupo, independentemente do canal de origem.

---

### Desenvolvimento e Testes

#### Executar Testes
```sh
go test ./...
```

#### Catálogo Multilíngue de UI (i18n)
Todas as mensagens da interface e notificações são mantidas em `internal/translatorbot/ui_strings.go` para 13 idiomas. Novas mensagens devem ser adicionadas em todos os idiomas, com validação garantida por `TestUIStringCatalogIsComplete`.

---

## 3. Licença

Este projeto está sob a licença [MIT License](LICENSE).
