# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

서로 다른 언어를 사용하는 사용자들이 같은 Discord 서버 내에서 각자의 모국어로 실시간 대화를 나눌 수 있도록 지원하는 Discord 봇입니다.

언어별 채널을 생성하여 **번역 그룹**으로 연결하면, 특정 채널에 게시된 메시지가 OpenAI 호환 LLM(Chat Completions API)을 통해 자동 번역되어 그룹 내 모든 채널로 **원래 발신자의 이름과 아바타**를 유지한 채 미러링됩니다. 참가자들은 자신의 언어 채널에서 자연스럽게 대화에 참여할 수 있습니다.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. 사용자 및 서버 관리자 가이드

### 주요 기능 및 사용자 경험

- **평소처럼 자연스럽게 대화**
  특수 명령어 입력 없이 평소처럼 메시지를 작성하여 전송하기만 하면 다른 언어 채널로 자동 번역 및 전달됩니다.
- **원래 발신자 프로필 유지**
  미러링된 메시지는 Webhook을 통해 전송되며 원래 작성자의 닉네임과 아바타 이미지가 그대로 유지됩니다.
- **모든 작업의 실시간 완벽 동기화**
  - **새 메시지 및 첨부 파일**: 텍스트뿐만 아니라 이미지(대체 텍스트 / Alt text 포함) 및 다양한 파일 첨부도 동기화됩니다.
  - **메시지 수정 및 삭제**: 원본 메시지를 수정하거나 삭제하면 다른 언어 채널의 번역 메시지도 즉시 수정되거나 삭제됩니다.
  - **답장(Reply)**: 참조된 메시지의 요약과 점프 링크를 대상 언어에 맞게 변환하여 전달합니다(의사 답장 형식).
  - **전달된 메시지(Forward)**: 전달된 컨텍스트를 유지하면서 헤더를 현지화하여 동기화합니다.
  - **반응(리액션) 및 고정(Pin)**: 메시지에 추가/제거된 이모지 반응 및 핀 고정 작업이 양방향으로 동기화됩니다.
  - **스레드 및 포럼**: 일반 스레드뿐만 아니라 포럼 및 미디어 채널의 스레드와 태그(Tag) 매핑을 완벽히 지원합니다.
  - **투표(Polls)**: 투표 메시지의 질문과 선택지를 번역하여 Embed 형식으로 미러링하며, 투표 종료 시 결과도 함께 게시합니다.
- **"View Original"(원문 보기) 기능**
  번역된 메시지를 마우스 우클릭(모바일은 길게 누름)한 후 **"앱" → "View Original"**을 선택하면 원본 메시지 링크와 원문 요약을 비공개(에페머럴)로 확인할 수 있습니다.
- **스마트 링크 및 미디어 처리**
  - 그룹 내 다른 언어 채널, 메시지 또는 스레드를 가리키는 링크와 멘션은 대상 언어 채널의 해당 ID로 자동 변환됩니다.
  - 외부 웹사이트 링크에 `hreflang` 다국어 버전이 있는 경우 자동으로 대상 언어 버전의 URL로 대체됩니다.

---

### 서버 도입 방법

#### 1. 봇 초대하기
Discord 개발자 포털(Developer Portal)에서 필요한 권한을 부여한 후 봇을 서버에 초대합니다:

- **OAuth2 Scopes**: `bot`, `applications.commands`
- **봇 권한 (Bot Permissions)**:
  - **일반**: `View Channels`(채널 보기), `Read Message History`(메시지 기록 읽기)
  - **텍스트**: `Send Messages`(메시지 보내기), `Send Messages in Threads`(스레드에서 메시지 보내기)
  - **관리**: `Pin Messages`(메시지 고정)
  - **Webhook**: `Manage Webhooks`(웹후크 관리)
  - **스레드**: `Create Public Threads`(공개 스레드 생성), `Manage Threads`(스레드 관리)
  - **반응**: `Add Reactions`(반응 추가하기)
- **권한 정수 (Permissions Integer)**: `2252126768139328`
  - *참고: 다른 서버의 커스텀 이모지 반응도 동기화하려면 `Use External Emojis`(외부 이모티콘 사용)를 추가 허용하세요 (권한 정수: `2252126768401472`).*

#### 2. 특권 인텐트 활성화
Discord 개발자 포털의 **Bot** 탭에서 `MESSAGE CONTENT INTENT`가 활성화되어 있어야 합니다.

---

### 채널 설정 (기본 작업)

#### 번역 그룹 생성
일본어 채널(예: `#general-ja`)에서 `/new-channel`을 실행하여 번역 그룹을 생성합니다:

```
/new-channel language:ja
```
*참고: `group`을 생략하면 현재 채널명이 그룹 식별자로 사용됩니다.*

#### 다른 언어 채널 추가
영어 채널(예: `#general-en`)에서 `/join-channel`을 실행하여 그룹에 참여시킵니다:

```
/join-channel group:general language:en
```

한국어 채널(예: `#general-ko`)을 추가하는 경우:

```
/join-channel group:general language:ko
```

이제 `#general-ja`, `#general-en`, `#general-ko`가 연결되어 상호 자동 번역이 시작됩니다.

#### 채널 퇴장 및 그룹 삭제
- 채널을 그룹에서 제거: `/leave-channel group:general`
- 그룹 전체를 완전히 삭제: `/delete-group group:general`
- 서버 내 번역 그룹 및 채널 목록 확인: `/list-groups`

---

### 명령어 레퍼런스

#### 관리자 슬래시 명령어
관리 명령어는 기본적으로 **서버 관리자(Administrator 권한)**만 실행할 수 있습니다. 특정 역할에 권한을 부여하려면 Discord "서버 설정" → "연동" → 봇 "관리" → "명령어 권한"에서 설정하세요.

| 명령어 | 설명 | 주요 옵션 |
|---|---|---|
| `/new-channel` | 새 번역 그룹을 생성하고 채널을 등록 | `language` (필수): BCP-47 언어 코드<br>`channel` (선택): 대상 채널 (기본값: 현재 채널)<br>`group` (선택): 그룹 식별자 (기본값: 채널명) |
| `/join-channel` | 기존 번역 그룹에 채널 추가 | `group` (필수): 참여할 그룹명<br>`language` (필수): BCP-47 언어 코드<br>`channel` (선택): 대상 채널 (기본값: 현재 채널) |
| `/leave-channel` | 번역 그룹에서 채널 제거 | `group` (필수): 그룹명<br>`channel` (선택): 대상 채널 (기본값: 현재 채널) |
| `/delete-group` | 번역 그룹 완전 삭제 | `group` (필수): 삭제할 그룹명 |
| `/list-groups` | 서버 내 모든 번역 그룹 및 채널 목록 표시 | 없음 |
| `/set-style` | 번역 그룹의 어조 및 스타일 설정 | `group` (필수): 그룹명<br>`preset` (선택): 스타일 프리셋 (하단 표 참고)<br>`custom` (선택): 자연어 사용자 정의 지침 (최대 200자) |
| `/add-glossary` | 서버 전용 용어집(우선 번역) 등록 | `term` (필수): 원문 용어<br>`translation` (필수): 우선 번역어<br>`attribute` (선택): 용어 속성 (인명, 지명, 은어 등)<br>`always_include` (선택): 키워드가 없어도 항상 프롬프트에 포함할지 여부 (기본값: `false`) |
| `/list-glossary` | 등록된 용어집 목록 표시 | 없음 |
| `/remove-glossary`| 등록된 용어집 항목 삭제 | `term` (필수): 삭제할 용어 |
| `/edit-forum-tags` | 포럼/미디어 채널의 태그 매핑 수정 | `group` (필수): 그룹명<br>`channel` (선택): 대상 포럼 채널 |
| `/bot-whitelist` | 자동 번역을 허용할 봇 및 Webhook 화이트리스트 관리 | 하위 명령어: `add`, `remove`, `list`<br>`source_type`: `bot` 또는 `webhook`<br>`source_id`: 봇 사용자 ID 또는 Webhook ID |

#### 메시지 앱 명령어 (일반 사용자 사용 가능)
- **`View Original` (앱 메뉴)**
  메시지 우클릭 또는 길게 누름 → **"앱" → "View Original"**을 실행하여 원본 메시지 점프 링크와 원문 미리보기를 확인합니다.

---

### 고급 맞춤 설정

#### 1. 번역 스타일 설정 (`/set-style`)
서버 커뮤니티의 분위기에 맞춰 번역 어조를 설정할 수 있습니다(프리셋과 사용자 정의 지침은 상호 배타적).

| 프리셋 | 특징 및 용도 |
|---|---|
| `default` | 원어민이 채팅에서 사용하는 자연스러운 일상 대화 스타일 |
| `casual` | 친구끼리 나누는 편안하고 친근한 말투 |
| `gaming` | 게임 커뮤니티에서 사용하는 은어 및 표현 |
| `friendly` | 다정하고 친절한 어조 |
| `business` | 비즈니스에 적합한 간결하고 정중한 표현 |
| `formal` | 존댓말과 격식을 갖춘 정중한 문체 |
| `netslang` | 인터넷 유행어 및 커뮤니티 게시판 스타일 |
| `tweet` | SNS(X / Twitter) 스타일의 짧고 직관적인 문체 |
| `literal` | 여러 번역이 가능할 때 가장 직역에 가까운 표현 선택 |

#### 2. 서버 용어집 등록 (`/add-glossary`)
인명, 게임 용어, 캐릭터 이름, 서버 고유 은어 등을 등록하여 오역을 방지할 수 있습니다(서버당 최대 50개).
- **속성(`attribute`)**: "인명", "지명", "은어", "약어", "전문용어" 등의 속성을 부여하여 AI가 문맥을 정확히 파악하도록 돕습니다.
- **항상 포함(`always_include`)**: `true`로 설정하면 메시지에 해당 단어가 포함되어 있지 않아도 항상 컨텍스트로 LLM에 전달됩니다.

#### 3. 포럼 및 미디어 채널 태그 매핑 (`/edit-forum-tags`)
포럼 채널을 그룹화할 때 언어별 태그 ID를 서로 연결할 수 있습니다. 특정 언어 포럼에서 태그가 달린 게시물이 작성되면 미러링 채널에도 대응하는 태그가 자동 적용됩니다.

#### 4. 자동 발신 화이트리스트 (`/bot-whitelist`)
기본적으로 무한 루프 방지를 위해 봇이나 Webhook 메시지는 번역되지 않습니다. `/bot-whitelist add`를 사용하여 공지 봇, RSS 피드 등을 허용하면 해당 자동 메시지도 번역 및 미러링할 수 있습니다.

---

## 2. 엔지니어 및 셀프 호스팅 가이드

### 시스템 요구사항 및 기술 스택

- **개발 언어**: Go 1.24 이상
- **데이터베이스**: SQLite (`modernc.org/sqlite`를 통한 순수 Go 구현, CGO 불필요)
- **Discord 라이브러리**: `github.com/bwmarrin/discordgo`
- **번역 엔진**: OpenAI 호환 Chat Completions API (OpenAI, OpenRouter, Azure OpenAI, 로컬 LLM 등)
- **크로스 컴파일**: `CGO_ENABLED=0`으로 Linux, Windows, macOS용 단일 바이너리를 손쉽게 빌드 가능.

---

### 셀프 호스팅 및 실행 절차

#### 1. Discord 봇 생성
1. [Discord Developer Portal](https://discord.com/developers/applications)에서 새 애플리케이션을 생성합니다.
2. **Bot** 탭에서 `MESSAGE CONTENT INTENT`를 활성화하고 Bot Token을 복사합니다.
3. **OAuth2 → URL Generator**에서 `bot`, `applications.commands` 스코프와 필수 권한을 선택하고 초대 링크를 생성하여 서버에 추가합니다.

#### 2. OpenAI 호환 API 준비
원하는 LLM 공급자(OpenAI, OpenRouter 등)의 엔드포인트 URL, API 키 및 모델 ID를 준비합니다.

#### 3. 환경 변수 설정
`.env.example`을 복사하여 `.env`를 생성하고 필요한 파라미터를 입력합니다:

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

#### 4. 빌드 및 실행

로컬 직접 실행:
```sh
go run ./cmd/discord-auto-translator
```

바이너리 빌드 및 실행:
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**모델 연결 및 계약 사전 검증 (`--model-prewarm`)**:
배포 시 LLM 연결, 인증 및 구조화 출력 스키마 계약을 사전 검증하고 종료하는 모드입니다(Discord 및 HTTP 서버는 실행되지 않음):
```sh
./discord-auto-translator --model-prewarm
```

---

### 환경 변수 레퍼런스

| 변수명 | 필수 여부 | 기본값 | 설명 |
|---|---|---|---|
| `DISCORD_TOKEN` | **필수** | - | Discord 봇 토큰 |
| `OPENAI_BASE_URL` | **필수** | - | OpenAI 호환 Chat Completions 기본 URL (예: `https://api.openai.com/v1`) |
| `OPENAI_API_KEY` | **필수** | - | API Bearer 인증 토큰 |
| `OPENAI_MODEL` | **필수** | - | 대상 LLM 모델 ID |
| `OPENAI_REASONING_EFFORT` | 선택 | (미설정) | 선택적 `reasoning_effort` 매개변수. 하이브리드 추론 모델에서 지연 시간을 줄이기 위해 생각 토큰을 비활성화하려면 `none` 설정 |
| `DB_PATH` | 선택 | `./translator.db` | SQLite 데이터베이스 파일 경로 |
| `HTTP_ADDR` | 선택 | `:8080` | 아바타 뱃지 HTTP 서버 바인딩 주소 |
| `PUBLIC_BASE_URL` | 선택 | (미설정) | 아바타 링 뱃지용 공개 기본 URL. 설정 시 최상위 역할 색상 링이 아바타 외곽에 렌더링됨 |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | 선택 | `100000` | 서버(길드)당 분당 번역 토큰 제한 |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | 선택 | `120` | `/avatar` 엔드포인트 IP당 분당 요청 속도 제한 |
| `MESSAGE_LINK_RETENTION_DAYS` | 선택 | `0` | 메시지 링크 데이터 보관 기간(일). `0`은 무기한; 설정 시 24시간마다 만료 데이터 자동 삭제 |
| `GUILD_DATA_RETENTION_DAYS` | 선택 | `0` | 봇이 퇴장한 서버의 데이터 보관 기간(일). 기한 내 재참여 시 삭제 예약 취소 |

---

### 아키텍처 및 설계 원칙

#### 1. 번역 파이프라인
1. **문맥 수집 (Context Assembly)**: 채널 주제, 최근 대화 버스트, 답장 참조, 공유 URL OGP 메타데이터, 리사이징된 첨부 이미지를 수집합니다.
2. **플레이스홀더 보호 (Placeholder Masking)**: 멘션(`<@id>`), 이모지(`<:name:id>`), 채널 링크(`<#id>`), URL, 코드 블록을 `[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]` 형태로 임시 치환하여 오역 및 프롬프트 인젝션을 방지합니다.
3. **프롬프트 구성 및 캐싱 최적화**: 시스템 프롬프트, 고정 컨텍스트, 히스토리 컨텍스트, 가변 콘텐츠를 계층화하여 LLM의 접두사 프롬프트 캐싱(Prefix Prompt Caching) 효율을 극대화합니다.
4. **Structured Outputs 일괄 생성**: `response_format.type=json_schema`(`strict: true`)를 사용하여 한 번의 API 호출로 모든 대상 언어의 번역 결과를 구조화된 JSON으로 수신합니다.
5. **후처리 및 미러링**: 플레이스홀더를 복원하고 Discord 채널/메시지 링크 변환 및 `hreflang` URL 대체를 적용한 후 Webhook을 통해 대상 채널로 동시 전송합니다.

#### 2. 보안 및 Fail-Closed 설계
- **프롬프트 인젝션 방어**: 모든 사용자 입력은 XML 이스케이프 처리되며 격리 태그 내에 배치됩니다. 플레이스홀더 메커니즘으로 시스템 지침 덮어쓰기를 원천 차단합니다.
- **Fail-Closed 원칙**: 토큰 수 초과(`finish_reason=length`), 유효하지 않은 JSON 응답, 일시적 네트워크 오류 발생 시 불완전한 번역을 전송하지 않고 원본 채널에 에러 알림을 게시합니다.

#### 3. 신뢰성 및 데이터 일관성
- **멱등성 보장 (Idempotency)**: `message_links` 및 `processed_events`를 통해 중복 게이트웨이 이벤트 수신 시 메시지 중복 미러링을 방지합니다.
- **보상 트랜잭션**: Webhook 전송 후 DB 저장이 실패하면 전송된 Discord 메시지를 즉시 삭제하여 고아 메시지를 방지합니다.
- **양방향 동기화**: 반응 및 핀 고정은 메시지 링크 매핑을 기반으로 어느 채널에서 변경되더라도 그룹 전체에 양방향으로 동기화됩니다.

---

### 개발 및 테스트

#### 테스트 실행
```sh
go test ./...
```

#### 다국어 UI 카탈로그 (i18n)
사용자 대면 메시지 및 시스템 알림은 `internal/translatorbot/ui_strings.go`에서 13개 언어로 중앙 집중 관리됩니다. 새 문자열 추가 시 모든 지원 언어 항목을 정의해야 하며, `TestUIStringCatalogIsComplete` 테스트로 검증합니다.

---

## 3. 라이선스

본 프로젝트는 [MIT License](LICENSE)에 따라 배포됩니다.
