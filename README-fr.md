# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Un bot Discord qui permet à des utilisateurs parlant différentes langues de communiquer facilement en temps réel au sein d'un même serveur Discord, chacun dans sa langue maternelle.

En liant un salon par langue au sein d'un **groupe de traduction**, chaque message posté dans l'un des salons est automatiquement traduit par un modèle de langage compatible OpenAI (API Chat Completions) et relayé dans tous les autres salons du groupe avec **le nom et l'avatar de l'auteur original**. Les participants discutent ainsi naturellement dans leur propre langue.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. Guide Utilisateur & Administrateur

### Fonctionnalités & Expérience Utilisateur

- **Discutez naturellement comme d'habitude**
  Aucune commande spéciale ni préfixe requis. Tapez et envoyez vos messages normalement : ils seront automatiquement traduits et transmis aux autres salons en temps réel.
- **Les messages conservent l'identité de l'expéditeur**
  Les messages traduits sont envoyés via des Webhooks Discord avec le nom et l'avatar de l'auteur d'origine.
- **Synchronisation complète en temps réel**
  - **Nouveaux messages & pièces jointes** : Prend en charge le texte, les images (y compris le texte alternatif / descriptions) et divers fichiers joints.
  - **Modifications & suppressions** : Modifier ou supprimer un message met à jour ou supprime instantanément ses versions traduites.
  - **Réponses** : Affiche un extrait du message cité dans la langue cible avec un lien vers le message correspondant (pseudo-réponse).
  - **Messages transférés** : Conserve le contexte transféré avec un en-tête localisé.
  - **Réactions & épingles** : L'ajout/suppression de réactions et l'épinglage de messages sont synchronisés de manière bidirectionnelle.
  - **Fils de discussion & forums** : Prise en charge des fils standards, des salons forum et média, incluant le mappage des tags.
  - **Sondages (Polls)** : Traduit les questions et choix dans un format Embed et publie les résultats finaux une fois le sondage clos.
- **Fonction « View Original » (Voir l'original)**
  Faites un clic droit (ou un appui long sur mobile) sur un message traduit, puis choisissez **« Applications » → « View Original »** pour obtenir un lien éphémère vers le message source et un aperçu du texte d'origine.
- **Traitement intelligent des liens et médias**
  - Les liens et mentions pointant vers des salons, messages ou fils gérés sont automatiquement réécrits vers leurs équivalents dans la langue cible.
  - Les URL externes disposant d'alternatives `hreflang` sont automatiquement remplacées par la version dans la langue cible.

---

### Installation sur un serveur

#### 1. Inviter le bot
Générez un lien d'invitation dans le Discord Developer Portal avec les autorisations suivantes :

- **OAuth2 Scopes** : `bot`, `applications.commands`
- **Permissions du bot** :
  - **Général** : `View Channels` (Voir les salons), `Read Message History` (Voir les anciens messages)
  - **Texte** : `Send Messages` (Envoyer des messages), `Send Messages in Threads` (Envoyer des messages dans les fils)
  - **Modération** : `Pin Messages` (Épingler des messages)
  - **Webhooks** : `Manage Webhooks` (Gérer les webhooks)
  - **Fils** : `Create Public Threads` (Créer des fils publics), `Manage Threads` (Gérer les fils)
  - **Réactions** : `Add Reactions` (Ajouter des réactions)
- **Permissions Integer** : `2252126768139328`
  - *Remarque : Pour synchroniser également les emojis personnalisés d'autres serveurs, activez `Use External Emojis` (Permissions Integer : `2252126768401472`).*

#### 2. Activer l'Intention Privilégiée
Assurez-vous que l'option `MESSAGE CONTENT INTENT` est activée dans l'onglet **Bot** du Discord Developer Portal.

---

### Configuration des Salons (Opérations de base)

#### Créer un groupe de traduction
Dans votre salon en japonais (ex: `#general-ja`), exécutez `/new-channel` :

```
/new-channel language:ja
```
*Remarque : Si `group` est omis, le nom du salon actuel servira d'identifiant.*

#### Ajouter des salons dans d'autres langues
Dans votre salon en anglais (ex: `#general-en`), exécutez `/join-channel` :

```
/join-channel group:general language:en
```

Pour ajouter un salon en français (ex: `#general-fr`) :

```
/join-channel group:general language:fr
```

Désormais, `#general-ja`, `#general-en` et `#general-fr` sont liés et la traduction automatique est active.

#### Quitter un groupe et supprimer des groupes
- Retirer un salon d'un groupe : `/leave-channel group:general`
- Supprimer complètement un groupe : `/delete-group group:general`
- Afficher les groupes et salons actifs : `/list-groups`

---

### Référence des Commandes

#### Commandes Slash d'Administration
Par défaut, les commandes d'administration ne sont accessibles qu'aux utilisateurs disposant des **permissions d'Administrateur**. Pour autoriser d'autres rôles, rendez-vous dans Discord : **Paramètres du serveur → Intégrations → (Nom du bot) → Gérer → Permissions des commandes**.

| Commande | Description | Options principales |
|---|---|---|
| `/new-channel` | Créer un nouveau groupe de traduction et enregistrer un salon | `language` (requis) : Code langue BCP-47<br>`channel` (optionnel) : Salon cible (par défaut : salon actuel)<br>`group` (optionnel) : Nom du groupe (par défaut : nom du salon) |
| `/join-channel` | Ajouter un salon à un groupe existant | `group` (requis) : Nom du groupe<br>`language` (requis) : Code langue BCP-47<br>`channel` (optionnel) : Salon cible (par défaut : salon actuel) |
| `/leave-channel` | Retirer un salon d'un groupe | `group` (requis) : Nom du groupe<br>`channel` (optionnel) : Salon cible (par défaut : salon actuel) |
| `/delete-group` | Supprimer complètement un groupe | `group` (requis) : Nom du groupe à supprimer |
| `/list-groups` | Lister les groupes de traduction et salons associés | Aucun |
| `/set-style` | Définir le style ou le ton de traduction d'un groupe | `group` (requis) : Nom du groupe<br>`preset` (optionnel) : Préréglage de style (voir ci-dessous)<br>`custom` (optionnel) : Instruction personnalisée en langage naturel (max 200 car.) |
| `/add-glossary` | Enregistrer une traduction préférée dans le glossaire du serveur | `term` (requis) : Terme source<br>`translation` (requis) : Traduction préférée<br>`attribute` (optionnel) : Catégorie (ex: nom de personne, argot)<br>`always_include` (optionnel) : Inclure dans le prompt sans correspondance de mot-clé (défaut : `false`) |
| `/list-glossary` | Lister les entrées du glossaire pour ce serveur | Aucun |
| `/remove-glossary`| Supprimer une entrée du glossaire | `term` (requis) : Terme à supprimer |
| `/edit-forum-tags` | Modifier les correspondances de tags pour les salons forum/média | `group` (requis) : Nom du groupe<br>`channel` (optionnel) : Salon forum cible |
| `/bot-whitelist` | Gérer la liste blanche pour les bots et webhooks automatisés | Sous-commandes : `add`, `remove`, `list`<br>`source_type` : `bot` ou `webhook`<br>`source_id` : ID utilisateur du bot ou ID du webhook |

#### Commande de Message (Accessible à tous)
- **`View Original` (Menu contextuel d'application)**
  Clic droit ou appui long sur un message → **« Applications » → « View Original »** pour recevoir un lien direct et un extrait de l'original.

---

### Personnalisation Avancée

#### 1. Style de traduction (`/set-style`)
Adaptez le ton de la traduction à la communauté de votre serveur (`preset` et `custom` sont mutuellement exclusifs) :

| Préréglage | Description & Utilisation |
|---|---|
| `default` | Ton conversationnel naturel tel qu'écrit par des locuteurs natifs |
| `casual` | Ton décontracté et amical adapté aux communautés d'amis |
| `gaming` | Argot de jeu vidéo et style communauté gaming |
| `friendly` | Ton chaleureux, poli et accueillant |
| `business` | Style concis, professionnel et poli |
| `formal` | Ton formel utilisant le vouvoiement et les formules de politesse |
| `netslang` | Argot d'Internet et style forum |
| `tweet` | Style court et percutant façon réseaux sociaux (X / Twitter) |
| `literal` | Traduction littérale lorsque plusieurs interprétations existent |

#### 2. Glossaire du serveur (`/add-glossary`)
Fixez la traduction des noms de personnages, termes de jeu ou jargon spécifique au serveur (jusqu'à 50 entrées par serveur) :
- **Attributs (`attribute`)** : Préciser une catégorie (« nom de personne », « nom de lieu », « argot », « abréviation », « terme technique ») aide le modèle à mieux cerner le contexte.
- **Toujours inclure (`always_include`)** : Défini sur `true`, le terme est systématiquement inclus dans le prompt même si le mot n'est pas explicitement présent dans le message.

#### 3. Mappage des tags de forum (`/edit-forum-tags`)
Lors de la liaison de salons forums ou médias, vous pouvez associer les tags entre les différentes langues. Lorsqu'un post est créé avec un tag, le post miroir reçoit automatiquement le tag correspondant.

#### 4. Liste blanche des messages automatisés (`/bot-whitelist`)
Par défaut, les messages de bots et webhooks sont ignorés pour éviter les boucles infinies. Vous pouvez utiliser `/bot-whitelist add` pour autoriser explicitement les bots d'annonces ou flux RSS.

---

## 2. Guide Développeur & Auto-Hébergement

### Prérequis & Stack Technique

- **Langage** : Go 1.24 ou supérieur
- **Base de données** : SQLite (Driver Go pur via `modernc.org/sqlite`, sans CGO)
- **Bibliothèque Discord** : `github.com/bwmarrin/discordgo`
- **Moteur de traduction** : API Chat Completions compatible OpenAI (OpenAI, OpenRouter, Azure OpenAI, LLM locaux, etc.)
- **Compilation croisée** : Prise en charge totale avec `CGO_ENABLED=0` pour des binaires uniques sous Linux, Windows et macOS.

---

### Déploiement & Démarrage

#### 1. Créer un Bot Discord
1. Rendez-vous sur le [Discord Developer Portal](https://discord.com/developers/applications) et créez une nouvelle Application.
2. Dans l'onglet **Bot**, activez `MESSAGE CONTENT INTENT` et copiez le Bot Token.
3. Dans **OAuth2 → URL Generator**, cochez `bot` et `applications.commands` avec les permissions requises et invitez le bot.

#### 2. Préparer une API compatible OpenAI
Obtenez une URL d'API, une clé API et un nom de modèle auprès de votre fournisseur LLM.

#### 3. Configurer les variables d'environnement
Copiez `.env.example` vers `.env` et renseignez les paramètres nécessaires :

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

#### 4. Compiler et exécuter

Exécution locale directe :
```sh
go run ./cmd/discord-auto-translator
```

Compilation et exécution d'un binaire autonome :
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**Validation préalable du modèle (`--model-prewarm`)** :
Permet de vérifier les identifiants API, la connectivité et le schéma de réponse avant le déploiement :
```sh
./discord-auto-translator --model-prewarm
```

---

### Référence des Variables d'Environnement

| Variable | Requis | Valeur par défaut | Description |
|---|---|---|---|
| `DISCORD_TOKEN` | **Oui** | - | Token d'authentification du bot Discord |
| `OPENAI_BASE_URL` | **Oui** | - | URL de base de l'API Chat Completions (ex: `https://api.openai.com/v1`) |
| `OPENAI_API_KEY` | **Oui** | - | Clé d'API Bearer |
| `OPENAI_MODEL` | **Oui** | - | Identifiant du modèle LLM cible |
| `OPENAI_REASONING_EFFORT` | Non | (non défini) | Paramètre `reasoning_effort` facultatif. Mettre à `none` pour désactiver les tokens de réflexion sur les modèles hybrides |
| `DB_PATH` | Non | `./translator.db` | Chemin du fichier de base de données SQLite |
| `HTTP_ADDR` | Non | `:8080` | Adresse d'écoute pour le serveur HTTP des badges d'avatar |
| `PUBLIC_BASE_URL` | Non | (non défini) | URL publique pour les badges d'avatar. Ajoute un anneau avec la couleur du rôle le plus élevé |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | Non | `100000` | Quota de tokens par serveur (guilde) par minute |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | Non | `120` | Limite de requêtes par minute par IP pour `/avatar` |
| `MESSAGE_LINK_RETENTION_DAYS` | Non | `0` | Durée de rétention en jours des liens de messages. `0` = illimité |
| `GUILD_DATA_RETENTION_DAYS` | Non | `0` | Durée de rétention des données d'un serveur après le départ du bot |

---

### Architecture & Conception

#### 1. Pipeline de Traduction
1. **Assemblage du contexte** : Collecte le sujet du salon, l'historique récent, les références de réponses, les métadonnées OGP des URL et les images redimensionnées.
2. **Masquage par balises réservées** : Remplace les mentions (`<@id>`), emojis (`<:name:id>`), salons (`<#id>`), liens et blocs de code par des jetons (`[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]`) pour éviter les injections de prompt.
3. **Optimisation du cache** : Structure le prompt en sections stables et dynamiques pour tirer parti du Prefix Prompt Caching des fournisseurs d'IA.
4. **Génération Structured Outputs** : Utilise `response_format.type=json_schema` (`strict: true`) pour générer toutes les traductions en un seul appel API.
5. **Post-traitement & Diffusion** : Restaure les balises, réécrit les liens Discord internes, remplace les URL `hreflang` et distribue les messages via Webhook.

#### 2. Sécurité & Fail-Closed
- **Défense contre l'injection de prompt** : Tout le contenu utilisateur est échappé en XML et isolé dans des balises dédiées.
- **Principe Fail-Closed** : En cas de dépassement de tokens (`finish_reason=length`), de JSON invalide ou d'erreur réseau, le bot n'envoie pas de message partiel ou corrompu, mais poste une notification d'erreur dans le salon source.

#### 3. Fiabilité & Cohérence des Données
- **Idempotence** : `message_links` et `processed_events` préviennent la duplication des messages en cas d'événements réseau répétés.
- **Transactions de compensation** : Si l'enregistrement en base échoue après l'envoi Webhook, le message Discord envoyé est immédiatement supprimé.
- **Synchronisation bidirectionnelle** : Les réactions et épingles sont répercutées dans l'ensemble du groupe, quel que soit le salon où l'action a eu lieu.

---

### Développement & Tests

#### Lancer les tests
```sh
go test ./...
```

#### Catalogue UI Multilingue (i18n)
Toutes les chaînes visibles par les utilisateurs sont gérées dans `internal/translatorbot/ui_strings.go` pour 13 langues. L'ajout d'une nouvelle chaîne requiert sa définition dans toutes les langues, validée par `TestUIStringCatalogIsComplete`.

---

## 3. Licence

Ce projet est distribué sous licence [MIT License](LICENSE).
