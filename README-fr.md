# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Un bot Discord qui permet à des personnes parlant des langues différentes de discuter ensemble dans le même serveur.

Associez un salon par langue en formant un **groupe de traduction**. Chaque message posté dans un salon est immédiatement traduit par Google Gemini et dupliqué dans tous les autres salons du groupe — avec le nom et l'avatar de l'auteur d'origine — de sorte que chaque salon ressemble à une conversation naturelle dans sa propre langue.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-fr (Français)
```

## Fonctionnalités

- **Tout reste synchronisé** — pas seulement les nouveaux messages : les modifications, suppressions, réponses, messages transférés, réactions, épingles, fils (salons texte / forum / média) et messages ne contenant que des pièces jointes sont tous dupliqués dans le groupe.
- **Les messages semblent envoyés par leur auteur** — les messages dupliqués sont envoyés via webhooks avec le nom et l'avatar de l'auteur original.
- **Traductions naturelles** — Gemini utilise le nom du salon, le sujet et l'historique récent de la conversation comme contexte ; un glossaire par serveur permet de fixer les traductions préférées pour les noms et le jargon.
- **Gestion intelligente des liens** — les liens et mentions pointant vers des salons ou messages gérés sont réécrits vers leurs équivalents dans chaque langue, et les URL disposant d'alternatives `hreflang` sont remplacées par la version dans la langue cible.
- **Efficace et sécurisé** — les messages sans texte à traduire (URL, mentions, emojis personnalisés, code) sont dupliqués sans appeler l'API Gemini ; des limites de taux de jetons par serveur s'appliquent ; les URL, mentions et blocs de code sont protégés contre les injections de prompt.
- **Interface localisée** — les réponses aux commandes suivent la langue du client Discord de l'utilisateur, et les notifications de salon utilisent la langue configurée pour ce salon (13 langues, anglais par défaut).

## Prérequis

- Go 1.24 ou version ultérieure
- Un compte bot Discord avec l'intent privilégié `MESSAGE CONTENT` activé
- Une clé API Google Gemini

## Installation

### 1. Préparer le bot Discord

1. Créez une application sur le [Discord Developer Portal](https://discord.com/developers/applications)
2. Sur la page **Bot** :
   - Activez le `MESSAGE CONTENT INTENT` (obligatoire)
   - Copiez le token du bot
3. Invitez le bot sur votre serveur via **OAuth2 → URL Generator** :
   - Scopes : `bot`, `applications.commands`
   - Permissions (telles qu'affichées dans le Developer Portal) :
     - **Général** : `View Channel`, `Read Message History`
     - **Messages** : `Send Messages`, `Send Messages in Threads`
     - **Modération** : `Pin Messages`
     - **Webhooks** : `Manage Webhooks`
     - **Fils** : `Create Public Threads`, `Manage Threads`
     - **Réactions** : `Add Reactions`
   - L'entier de permissions correspondant est `2252126768139328`
   - Pour synchroniser également les réactions d'emojis personnalisés provenant d'autres serveurs, autorisez en plus `Use External Emojis` ; l'entier de permissions devient alors `2252126768401472`

### 2. Obtenir une clé API Gemini

Obtenez une clé API sur [Google AI Studio](https://aistudio.google.com/).

### 3. Configurer les variables d'environnement

```sh
cp .env.example .env
```

Modifiez `.env` et définissez les valeurs suivantes :

```env
DISCORD_TOKEN=your-discord-bot-token
GEMINI_API_KEY=your-gemini-api-key
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
GEMINI_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
# MESSAGE_LINK_RETENTION_DAYS=60
# GUILD_DATA_RETENTION_DAYS=30
```

| Variable | Obligatoire | Description |
|---|---|---|
| `DISCORD_TOKEN` | Oui | Token du bot Discord |
| `GEMINI_API_KEY` | Oui | Clé API Gemini |
| `DB_PATH` | Non | Chemin vers le fichier SQLite (défaut : `./translator.db`) |
| `HTTP_ADDR` | Non | Adresse du serveur de badge d'avatar (défaut : `:8080`) |
| `PUBLIC_BASE_URL` | Non | URL de base publique pour les badges d'anneau d'avatar. Si non définie, les messages reflétés utilisent l'URL d'avatar Discord d'origine et le serveur de badge n'est pas utilisé |
| `GEMINI_RATE_LIMIT_TOKENS_PER_MIN` | Non | Limite de jetons Gemini par serveur et par minute (défaut : `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | Non | Limite de requêtes par IP et par minute pour le point de terminaison de badge `/avatar` (défaut : `120`) |
| `MESSAGE_LINK_RETENTION_DAYS` | Non | Nombre de jours de conservation des `message_links` dans SQLite avant purge automatique. `0` (défaut) désactive la purge ; p. ex. `60` supprime les liens de plus de 60 jours au démarrage et toutes les 24 heures |
| `GUILD_DATA_RETENTION_DAYS` | Non | Nombre de jours de conservation dans SQLite des données d'un serveur après le retrait du bot. `0` (défaut) désactive la purge ; p. ex. `30` purge au démarrage et toutes les 24 heures les données des serveurs retirés depuis plus de 30 jours. Un retour avant l'échéance annule la purge prévue |

### 4. Démarrer

```sh
go run ./cmd/discord-auto-translator
```

Ou compiler puis exécuter :

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## Utilisation

Une fois le bot démarré, les commandes slash sont enregistrées dans chaque serveur.

### Configurer les salons

#### Créer un groupe de traduction

Exécutez `/new-channel` dans votre salon japonais pour créer un groupe de traduction :

```
/new-channel language:ja
```

#### Ajouter des salons dans d'autres langues

Exécutez `/join-channel` dans votre salon anglais pour l'ajouter au groupe :

```
/join-channel group:general language:en
```

Pour ajouter également un salon français :

```
/join-channel group:general language:fr
```

Les salons `#general-ja`, `#general-en` et `#general-fr` sont maintenant liés.

### Commandes

Par défaut, les commandes slash d'administration ne peuvent être exécutées que par les **administrateurs du serveur**. Pour autoriser d'autres rôles, rendez-vous dans "Paramètres du serveur" Discord → "Intégrations" → "Gérer" du bot → "Autorisations des commandes", et configurez les accès globalement ou par commande. Le bot ne modifie jamais les rôles ni les permissions de commandes de lui-même.

| Commande | Description |
|---|---|
| `/new-channel language:[langue] channel:<salon> group:<groupe>` | Créer un nouveau groupe de traduction. `channel` vaut le salon actuel par défaut ; `group` vaut le nom du salon par défaut |
| `/join-channel group:[groupe] language:[langue] channel:<salon>` | Ajouter un salon à un groupe. `channel` vaut le salon actuel par défaut |
| `/leave-channel group:[groupe] channel:<salon>` | Retirer un salon d'un groupe. `channel` vaut le salon actuel par défaut |
| `/delete-group group:[groupe]` | Supprimer un groupe entier |
| `/list-groups` | Afficher les groupes de traduction et leurs salons sur ce serveur |
| `/add-glossary term:[terme] translation:[traduction] attribute:<attribut> always_include:<bool>` | Enregistrer une traduction préférée dans le glossaire du serveur. `attribute` est libre avec suggestions ; `always_include` vaut `false` par défaut |
| `/list-glossary` | Afficher le glossaire du serveur |
| `/remove-glossary term:[terme]` | Supprimer une entrée du glossaire |
| `/set-style group:[groupe] preset:<préréglage> custom:<instruction personnalisée>` | Définir le style de traduction d'un groupe. Spécifier `preset` ou `custom`, pas les deux |
| `/bot-whitelist add source_type:[bot\|webhook] source_id:[ID]` | Autoriser une source de messages automatisée sur ce serveur. Avec `source_type:bot`, `source_id` est l'ID utilisateur du bot ; avec `source_type:webhook`, c'est l'ID du webhook |
| `/bot-whitelist remove source_type:[bot\|webhook] source_id:[ID]` | Retirer la source de messages automatisée correspondante de la liste d'autorisation de ce serveur |
| `/bot-whitelist list` | Afficher les sources bot et webhook autorisées sur ce serveur |

- Les listes de sources autorisées sont conservées dans SQLite et limitées à chaque serveur Discord (guild). Les webhooks de sortie gérés par le traducteur et les messages du bot traducteur lui-même restent exclus, même si leurs ID sont ajoutés

- `language` utilise des codes BCP-47 (`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr`, etc.)
- Maximum 50 entrées de glossaire par serveur
- `attribute` propose "nom de personne", "nom de lieu", "argot", "abréviation" et "terme technique", mais toute valeur peut être saisie librement. L'attribut est utilisé comme contexte pour que Gemini comprenne la signification du terme
- Les termes ordinaires ne sont ajoutés aux instructions système que si le message à traduire contient `term` (insensible à la casse). Les termes avec `always_include:true` sont toujours ajoutés
- Si l'option `channel` est omise, la commande s'applique au salon dans lequel elle est exécutée
- Types de salons pris en charge : texte, annonces, forum et média

## Tests

```sh
go test ./...
```

## Déploiement sur GCE

Un script de déploiement pour Google Compute Engine est inclus dans `deploy/deploy-gce.ps1` (Windows PowerShell).

Créez `deploy/deploy.json` à partir de l'exemple pour les paramètres de connexion GCE. La configuration de l'app et les secrets utilisent `.env` par défaut ; un autre fichier peut être indiqué via `envFile` dans `deploy.json` ou `-EnvFile`.

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# Modifier deploy.json et .env

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # Configuration initiale
.\deploy\deploy-gce.ps1                          # Mises à jour de code uniquement
.\deploy\deploy-gce.ps1 -UploadEnv               # Mise à jour des secrets
```

## Licence

Consultez le fichier [LICENSE](LICENSE) pour la licence de ce projet.
