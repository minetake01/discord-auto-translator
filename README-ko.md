# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

서로 다른 언어를 사용하는 사용자들이 같은 Discord 서버에서 함께 대화할 수 있게 해주는 봇입니다.

언어별로 채널을 하나씩 만들어 **번역 그룹**으로 연결하면, 한 채널에 올라온 메시지가 `google.gemma-4-26b-a4b`(Amazon Bedrock 경유)로 즉시 번역되어 그룹 내 다른 모든 채널로 미러링됩니다. 원래 작성자의 이름과 아바타가 그대로 표시되므로, 각 채널은 해당 언어로 진행되는 자연스러운 대화처럼 보입니다.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-ko (한국어)
```

## 주요 기능

- **모든 것이 동기화됩니다** — 새 메시지뿐만 아니라 수정, 삭제, 답장, 전달된 메시지, 반응(리액션), 고정(핀), 스레드(텍스트 / 포럼 / 미디어 채널), 첨부 파일만 있는 메시지까지 그룹 전체에 미러링됩니다.
- **본인이 보낸 것처럼 보입니다** — 미러링된 메시지는 웹훅을 통해 원래 작성자의 이름과 아바타로 전송됩니다.
- **자연스러운 번역** — Gemma 4 26B-A4B는 채널 이름, 주제, 최근 대화 기록을 문맥으로 참고합니다. 서버별 용어집으로 인명이나 전문 용어의 번역을 고정할 수도 있습니다.
- **스마트한 링크 처리** — 관리 대상 채널이나 메시지를 가리키는 링크와 멘션은 각 언어 채널의 대응 대상으로 재작성되며, `hreflang` 대체 버전이 있는 URL은 대상 언어 버전으로 교체됩니다.
- **효율적이고 안전함** — 번역할 텍스트가 없는 메시지(URL, 멘션, 커스텀 이모지, 코드만 있는 경우)는 번역 API를 호출하지 않고 미러링되며, 서버별 토큰 속도 제한이 적용됩니다. URL, 멘션, 코드 블록은 프롬프트 인젝션으로부터 보호됩니다. 번역 실패 시 fail-closed(미러링하지 않고 원본 채널에 현지화된 알림).
- **현지화된 UI** — 명령어 응답은 사용자의 Discord 클라이언트 언어를 따르고, 채널 알림은 채널에 설정된 언어로 표시됩니다(13개 언어 지원, 미지원 언어는 영어).

## 요구 사항

- Go 1.24 이상
- `MESSAGE CONTENT` 특권 인텐트가 활성화된 Discord 봇 계정
- Amazon Bedrock을 사용할 수 있고 `us-west-2`의 기본 Mantle Project에서 추론 생성을 허용한 IAM 액세스 키가 있는 AWS 계정
- Amazon Bedrock ID

## 설정

### 1. Discord 봇 준비

1. [Discord Developer Portal](https://discord.com/developers/applications)에서 애플리케이션 생성
2. **Bot** 페이지에서:
   - `MESSAGE CONTENT INTENT` 활성화(필수)
   - 봇 토큰 복사
3. **OAuth2 → URL Generator**로 봇을 서버에 초대:
   - Scopes: `bot`, `applications.commands`
   - Permissions(Developer Portal에 표시되는 이름 기준):
     - **일반**: `View Channel`, `Read Message History`
     - **메시지**: `Send Messages`, `Send Messages in Threads`
     - **관리**: `Pin Messages`
     - **웹훅**: `Manage Webhooks`
     - **스레드**: `Create Public Threads`, `Manage Threads`
     - **반응**: `Add Reactions`
   - 위 권한의 정수 값은 `2252126768139328`입니다
   - 다른 서버의 커스텀 이모지 반응까지 동기화하려면 `Use External Emojis`를 추가로 허용하세요. 이 경우 권한 정수 값은 `2252126768401472`입니다

### 2. Amazon Bedrock 구성

`us-west-2` Amazon Bedrock에서 `google.gemma-4-26b-a4b`를 활성화합니다. 해당 모델의 `bedrock-mantle:CreateInference`만 허용한 IAM 사용자를 만들고 `.env`에 `AWS_ACCESS_KEY_ID`와 `AWS_SECRET_ACCESS_KEY`를 설정합니다. 모델, 리전, 30초 타임아웃, 4096 token 한도는 코드에 고정됩니다.

### 3. 환경 변수 설정

```sh
cp .env.example .env
```

`.env`를 편집하여 다음을 설정합니다:

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

| 변수 | 필수 | 설명 |
|---|---|---|
| `DISCORD_TOKEN` | 필수 | Discord 봇 토큰 |
| `AWS_ACCESS_KEY_ID` | Yes | Access key ID for the dedicated Bedrock IAM user |
| `AWS_SECRET_ACCESS_KEY` | Yes | Secret access key for the dedicated Bedrock IAM user |
| `DB_PATH` | 선택 | SQLite 파일 경로(기본값: `./translator.db`) |
| `HTTP_ADDR` | 선택 | 아바타 배지 서버 주소(기본값: `:8080`) |
| `PUBLIC_BASE_URL` | 선택 | 아바타 링 배지용 공개 기본 URL. 미설정 시 미러링된 메시지는 Discord 원본 아바타 URL을 사용하며 배지 서버는 사용되지 않습니다 |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | 선택 | 서버(길드)별 분당 Gemma 4 26B-A4B 토큰 상한(기본값: `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | 선택 | `/avatar` 배지 엔드포인트의 IP별 분당 요청 상한(기본값: `120`) |
| `MESSAGE_LINK_RETENTION_DAYS` | 선택 | SQLite의 `message_links` 자동 정리 전 보관 일수. `0`(기본값)이면 정리 비활성화. 예: `60`이면 60일보다 오래된 링크를 시작 시 및 24시간마다 삭제 |
| `GUILD_DATA_RETENTION_DAYS` | 선택 | 봇이 서버에서 제거된 후 해당 서버의 SQLite 데이터를 보관하는 일수. `0`(기본값)이면 정리 비활성화. 예: `30`이면 제거 후 30일이 지난 서버 데이터를 시작 시 및 24시간마다 삭제. 만료 전에 다시 참여하면 예약된 삭제를 취소 |

### Amazon Bedrock 운영 계약

번역은 `us-west-2`의 `google.gemma-4-26b-a4b`를 비스트리밍 Mantle Responses API로 호출하며 **30초**, **provider-default temperature 1.0**, **max_output_tokens 4096**, schema 지시를 따르고 Bot이 엄격히 검증하는 JSON을 사용합니다. 모든 언어를 한 요청에서 생성합니다. 4K 한도, 비정상 종료 또는 잘못된 JSON은 전체를 fail-closed로 실패시키며 retry, 분할, fallback은 없습니다. Bot은 prompt, 응답, 인증 정보를 기록하지 않습니다. GCE 배포는 교체 전에 5분 제한의 `--bedrock-prewarm`으로 인증 정보, 모델 접근 권한 및 응답 계약을 검증합니다.

### 4. 실행

```sh
go run ./cmd/discord-auto-translator
```

또는 빌드 후 실행:

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## 사용 방법

봇을 시작하면 슬래시 명령어가 각 서버에 등록됩니다.

### 채널 설정

#### 번역 그룹 만들기

일본어 채널에서 `/new-channel`을 실행하여 번역 그룹을 만듭니다:

```
/new-channel language:ja
```

#### 다른 언어 채널 추가하기

영어 채널에서 `/join-channel`을 실행하여 그룹에 추가합니다:

```
/join-channel group:general language:en
```

한국어 채널도 추가하려면:

```
/join-channel group:general language:ko
```

이제 `#general-ja`, `#general-en`, `#general-ko`가 연결됩니다.

### 명령어 목록

관리용 슬래시 명령어는 기본적으로 **서버 관리자**만 실행할 수 있습니다. 다른 역할에도 실행을 허용하려면 Discord의 "서버 설정" → "연동" → 해당 봇의 "관리" → "명령어 권한"에서 전체 또는 명령어별로 권한을 설정하세요. 봇은 역할이나 명령어 권한을 스스로 변경하지 않습니다.

| 명령어 | 설명 |
|---|---|
| `/new-channel language:[언어] channel:<채널> group:<그룹>` | 번역 그룹 새로 만들기. `channel`을 생략하면 명령을 실행한 채널, `group`을 생략하면 채널 이름이 사용됩니다 |
| `/join-channel group:[그룹] language:[언어] channel:<채널>` | 그룹에 채널 추가. `channel`을 생략하면 명령을 실행한 채널이 대상이 됩니다 |
| `/leave-channel group:[그룹] channel:<채널>` | 그룹에서 채널 제외. `channel`을 생략하면 명령을 실행한 채널이 대상이 됩니다 |
| `/delete-group group:[그룹]` | 그룹 전체 삭제 |
| `/list-groups` | 이 서버의 번역 그룹과 채널 목록 표시 |
| `/add-glossary term:[용어] translation:[번역] attribute:<속성> always_include:<불리언>` | 서버 용어집에 우선 번역 등록. `attribute`는 후보가 표시되는 자유 입력입니다. `always_include` 기본값은 `false`입니다 |
| `/list-glossary` | 서버의 용어집 목록 표시 |
| `/remove-glossary term:[용어]` | 용어집 항목 삭제 |
| `/set-style group:[그룹] preset:<프리셋> custom:<사용자 지정 지시>` | 그룹의 번역 스타일 설정. `preset` 또는 `custom` 중 하나만 지정하세요 |
| `/bot-whitelist add source_type:[bot\|webhook] source_id:[ID]` | 이 서버에서 자동 메시지 출처를 허용합니다. `source_type:bot`이면 `source_id`는 봇 사용자 ID이고, `source_type:webhook`이면 웹후크 ID입니다 |
| `/bot-whitelist remove source_type:[bot\|webhook] source_id:[ID]` | 이 서버의 허용 목록에서 일치하는 자동 메시지 출처를 제거합니다 |
| `/bot-whitelist list` | 이 서버에서 허용된 봇 및 웹후크 출처를 표시합니다 |

- 출처 허용 목록은 SQLite에 영구 저장되며 Discord 서버(길드)별로 분리됩니다. 번역 봇이 관리하는 출력 웹후크와 번역 봇 자체의 메시지는 ID를 추가해도 계속 제외됩니다

- `language`는 BCP-47 형식(`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr` 등)
- 용어집은 서버당 최대 50개까지 등록할 수 있습니다
- `attribute`에는 "인명", "지명", "속어", "약어", "전문 용어" 등의 후보가 표시되며 임의의 속성도 자유롭게 입력할 수 있습니다. 지정한 속성은 Gemma 4 26B-A4B가 용어의 의미를 판단하는 문맥으로 사용됩니다
- 일반 용어는 번역 대상 본문에 `term`이 포함될 때만(대소문자 무시) 시스템 지시에 추가됩니다. `always_include:true`인 용어는 항상 추가됩니다
- `channel` 옵션을 생략하면 명령어를 실행한 채널이 대상이 됩니다
- 지원 채널 유형: 텍스트, 공지, 포럼, 미디어

## 테스트

```sh
go test ./...
```

## GCE 배포

Google Compute Engine용 배포 스크립트가 `deploy/deploy-gce.ps1`에 포함되어 있습니다(Windows PowerShell용).

예제에서 `deploy/deploy.json`을 만들어 GCE 연결 설정을 합니다. 앱 설정과 시크릿은 기본적으로 `.env`를 사용하며, `deploy.json`의 `envFile` 또는 `-EnvFile`로 다른 파일을 지정할 수 있습니다.

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# deploy.json과 .env 편집

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # 최초 설정
.\deploy\deploy-gce.ps1                          # 코드 업데이트만
.\deploy\deploy-gce.ps1 -UploadEnv               # 시크릿 업데이트
```

## 라이선스

이 프로젝트의 라이선스는 [LICENSE](LICENSE) 파일을 참조하세요.
