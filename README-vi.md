# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Bot Discord cho phép những người nói các ngôn ngữ khác nhau có thể giao tiếp theo thời gian thực trong cùng một máy chủ Discord, mỗi người sử dụng ngôn ngữ mẹ đẻ của mình.

Bằng cách liên kết mỗi kênh đại diện cho một ngôn ngữ vào một **nhóm dịch thuật (Translation Group)**, mọi tin nhắn được gửi trong một kênh sẽ được tự động dịch bởi mô hình ngôn ngữ lớn tương thích OpenAI (Chat Completions API) và chuyển tiếp đến tất cả các kênh khác trong nhóm bằng **tên và ảnh đại diện của người gửi ban đầu**. Mỗi kênh sẽ mang lại trải nghiệm như một cuộc trò chuyện tự nhiên bằng chính ngôn ngữ đó.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. Hướng dẫn dành cho Người dùng & Quản trị viên Máy chủ

### Các tính năng chính & Trải nghiệm Người dùng

- **Trò chuyện tự nhiên như bình thường**
  Không cần lệnh đặc biệt hay tiền tố bot. Chỉ cần nhập và gửi tin nhắn như bình thường, bot sẽ tự động dịch và đồng bộ hóa sang các kênh khác trong thời gian thực.
- **Tin nhắn giữ nguyên danh tính người gửi gốc**
  Các tin nhắn dịch được gửi thông qua Webhook của Discord, bảo toàn trọn vẹn tên hiển thị và ảnh đại diện của tác giả gốc.
- **Đồng bộ hóa hai chiều toàn diện theo thời gian thực**
  - **Tin nhắn mới & Tệp đính kèm**: Hỗ trợ văn bản, hình ảnh (bao gồm cả văn bản thay thế / mô tả hình ảnh) và các loại tệp đính kèm.
  - **Chỉnh sửa & Xóa tin nhắn**: Chỉnh sửa hoặc xóa tin nhắn gốc sẽ cập nhật hoặc xóa ngay lập tức các phiên bản dịch ở kênh khác.
  - **Trả lời (Replies)**: Trích dẫn đoạn văn bản của tin nhắn được phản hồi bằng ngôn ngữ đích kèm liên kết đến tin nhắn tương ứng (phản hồi giả lập).
  - **Tin nhắn chuyển tiếp**: Bảo toàn ngữ cảnh chuyển tiếp với tiêu đề được bản địa hóa.
  - **Thả cảm xúc (Reactions) & Ghim tin nhắn (Pins)**: Thao tác thêm/xóa reaction emoji và ghim tin nhắn được đồng bộ hóa hai chiều.
  - **Chủ đề (Threads) & Diễn đàn (Forums)**: Hỗ trợ các thread thông thường, kênh diễn đàn và kênh media, bao gồm cả ánh xạ thẻ tag diễn đàn.
  - **Bình chọn (Polls)**: Dịch câu hỏi và các lựa chọn sang định dạng Embed, đồng thời thông báo kết quả cuối cùng khi cuộc bình chọn kết thúc.
- **Tính năng «View Original» (Xem bản gốc)**
  Nhấp chuột phải (hoặc nhấn giữ trên điện thoại) vào bất kỳ tin nhắn dịch nào, chọn **«Ứng dụng» → «View Original»** để nhận liên kết trực tiếp và xem trích đoạn nội dung gốc (chỉ hiển thị cho bạn).
- **Xử lý liên kết và đa phương tiện thông minh**
  - Các liên kết và lượt nhắc (mention) đến kênh, tin nhắn hoặc thread được quản lý sẽ tự động chuyển đổi sang ID tương ứng của ngôn ngữ đích.
  - Các URL trang web bên ngoài có phiên bản đa ngôn ngữ `hreflang` sẽ tự động được thay thế bằng URL của ngôn ngữ đích.

---

### Các bước thêm Bot vào Máy chủ

#### 1. Mời Bot vào máy chủ
Tạo liên kết mời trên Discord Developer Portal với các quyền sau:

- **OAuth2 Scopes**: `bot`, `applications.commands`
- **Quyền của Bot (Bot Permissions)**:
  - **Chung**: `View Channels` (Xem kênh), `Read Message History` (Đọc lịch sử tin nhắn)
  - **Văn bản**: `Send Messages` (Gửi tin nhắn), `Send Messages in Threads` (Gửi tin nhắn trong luồng)
  - **Quản lý**: `Pin Messages` (Ghim tin nhắn)
  - **Webhook**: `Manage Webhooks` (Quản lý webhook)
  - **Luồng**: `Create Public Threads` (Tạo luồng công khai), `Manage Threads` (Quản lý luồng)
  - **Cảm xúc**: `Add Reactions` (Thêm phản ứng)
- **Giá trị quyền nguyên vẹn (Permissions Integer)**: `2252126768139328`
  - *Lưu ý: Để đồng bộ cả cảm xúc emoji tùy chỉnh từ máy chủ khác, hãy bật thêm `Use External Emojis` (Permissions Integer: `2252126768401472`).*

#### 2. Kích hoạt Privileged Intent
Đảm bảo rằng `MESSAGE CONTENT INTENT` đã được bật trong tab **Bot** trên Discord Developer Portal.

---

### Thiết lập Kênh (Thao tác Cơ bản)

#### Tạo nhóm dịch thuật
Chạy lệnh `/new-channel` tại kênh tiếng Nhật (ví dụ `#general-ja`):

```
/new-channel language:ja
```
*Lưu ý: Nếu bỏ qua `group`, tên kênh hiện tại sẽ được dùng làm tên nhóm.*

#### Thêm các kênh ngôn ngữ khác
Chạy lệnh `/join-channel` tại kênh tiếng Anh (ví dụ `#general-en`):

```
/join-channel group:general language:en
```

Để thêm kênh tiếng Việt (ví dụ `#general-vi`):

```
/join-channel group:general language:vi
```

Bây giờ `#general-ja`, `#general-en` và `#general-vi` đã được liên kết và quá trình tự động dịch sẽ bắt đầu.

#### Rời khỏi nhóm và xóa nhóm
- Rút một kênh ra khỏi nhóm: `/leave-channel group:general`
- Xóa hoàn toàn một nhóm dịch thuật: `/delete-group group:general`
- Xem danh sách các nhóm và kênh hiện có: `/list-groups`

---

### Danh mục Lệnh

#### Lệnh Slash dành cho Quản trị viên
Theo mặc định, các lệnh quản trị chỉ có thể được thực hiện bởi thành viên có quyền **Administrator**. Để cấp quyền cho các vai trò khác, hãy cấu hình tại: **Cài đặt máy chủ → Tích hợp → (Tên bot) → Quản lý → Quyền của lệnh**.

| Lệnh | Mô tả | Tùy chọn chính |
|---|---|---|
| `/new-channel` | Tạo nhóm dịch mới và đăng ký kênh | `language` (bắt buộc): Mã ngôn ngữ BCP-47<br>`channel` (tùy chọn): Kênh mục tiêu (mặc định: kênh hiện tại)<br>`group` (tùy chọn): Tên nhóm (mặc định: tên kênh) |
| `/join-channel` | Thêm kênh vào nhóm dịch hiện có | `group` (bắt buộc): Tên nhóm mục tiêu<br>`language` (bắt buộc): Mã ngôn ngữ BCP-47<br>`channel` (tùy chọn): Kênh mục tiêu (mặc định: kênh hiện tại) |
| `/leave-channel` | Rút một kênh ra khỏi nhóm dịch | `group` (bắt buộc): Tên nhóm<br>`channel` (tùy chọn): Kênh mục tiêu (mặc định: kênh hiện tại) |
| `/delete-group` | Xóa hoàn toàn một nhóm dịch thuật | `group` (bắt buộc): Tên nhóm cần xóa |
| `/list-groups` | Liệt kê tất cả các nhóm dịch và kênh liên kết | Không có |
| `/set-style` | Đặt phong cách hoặc giọng văn dịch thuật cho nhóm | `group` (bắt buộc): Tên nhóm<br>`preset` (tùy chọn): Phong cách dựng sẵn (xem bảng dưới)<br>`custom` (tùy chọn): Hướng dẫn tùy chỉnh bằng ngôn ngữ tự nhiên (tối đa 200 ký tự) |
| `/add-glossary` | Đăng ký từ dịch ưu tiên trong bảng thuật ngữ máy chủ | `term` (bắt buộc): Thuật ngữ gốc<br>`translation` (bắt buộc): Bản dịch mong muốn<br>`attribute` (tùy chọn): Phân loại từ (vd: tên người, tiếng lóng)<br>`always_include` (tùy chọn): Luôn đưa vào prompt kể cả khi không khớp từ khóa (mặc định: `false`) |
| `/list-glossary` | Xem danh sách thuật ngữ của máy chủ | Không có |
| `/remove-glossary`| Xóa một mục khỏi bảng thuật ngữ | `term` (bắt buộc): Thuật ngữ cần xóa |
| `/edit-forum-tags` | Chỉnh sửa ánh xạ thẻ tag cho kênh diễn đàn/media | `group` (bắt buộc): Tên nhóm<br>`channel` (tùy chọn): Kênh diễn đàn mục tiêu |
| `/bot-whitelist` | Quản lý danh sách cho phép bot và webhook tự động | Lệnh con: `add`, `remove`, `list`<br>`source_type`: `bot` hoặc `webhook`<br>`source_id`: ID người dùng bot hoặc ID webhook |

#### Lệnh Tin nhắn (Tất cả thành viên đều dùng được)
- **`View Original` (Menu Ứng dụng)**
  Nhấp chuột phải hoặc nhấn giữ vào tin nhắn → **«Ứng dụng» → «View Original»** để nhận liên kết trực tiếp và xem trích đoạn văn bản gốc.

---

### Tùy chỉnh Nâng cao

#### 1. Phong cách Dịch thuật (`/set-style`)
Điều chỉnh giọng điệu dịch cho phù hợp với cộng đồng máy chủ của bạn (`preset` và `custom` loại trừ lẫn nhau):

| Phong cách | Mô tả & Ứng dụng |
|---|---|
| `default` | Giọng đàm thoại tự nhiên như người bản xứ trò chuyện |
| `casual` | Thoải mái, thân mật phù hợp cho bạn bè và cộng đồng |
| `gaming` | Tiếng lóng game và phong cách cộng đồng game thủ |
| `friendly` | Ấm áp, lịch sự và thân thiện |
| `business` | Ngắn gọn, chuyên nghiệp và trang trọng |
| `formal` | Trang trọng với đại từ xưng hô tôn kính |
| `netslang` | Tiếng lóng Internet và phong cách diễn đàn |
| `tweet` | Ngắn gọn, súc tích theo phong cách mạng xã hội (X / Twitter) |
| `literal` | Dịch sát nghĩa đen khi có nhiều cách hiểu |

#### 2. Bảng Thuật ngữ Máy chủ (`/add-glossary`)
Cố định bản dịch cho tên nhân vật, thuật ngữ trò chơi hoặc tiếng lóng riêng của máy chủ (tối đa 50 mục mỗi máy chủ):
- **Phân loại (`attribute`)**: Các nhãn như "tên người", "địa danh", "tiếng lóng", "từ viết tắt", "thuật ngữ chuyên ngành" giúp AI hiểu đúng ngữ cảnh.
- **Luôn bao gồm (`always_include`)**: Khi đặt là `true`, thuật ngữ sẽ luôn được cung cấp làm ngữ cảnh cho AI ngay cả khi từ đó không xuất hiện trực tiếp trong tin nhắn.

#### 3. Ánh xạ Thẻ Diễn đàn (`/edit-forum-tags`)
Khi liên kết các kênh diễn đàn, bạn có thể ánh xạ các thẻ tag tương ứng giữa các ngôn ngữ. Khi một bài đăng có gắn thẻ được tạo, bài đăng nhân bản sẽ tự động nhận thẻ tương ứng.

#### 4. Danh sách cho phép tin nhắn tự động (`/bot-whitelist`)
Theo mặc định, tin nhắn từ bot và webhook bị bỏ qua để tránh vòng lặp vô hạn. Bạn có thể sử dụng `/bot-whitelist add` để cho phép các bot thông báo, nguồn cấp RSS được dịch và nhân bản.

---

## 2. Hướng dẫn dành cho Nhà phát triển & Tự Lưu trữ (Self-Hosting)

### Yêu cầu Hệ thống & Công nghệ

- **Ngôn ngữ**: Go 1.24 trở lên
- **Cơ sở dữ liệu**: SQLite (Driver thuần Go qua `modernc.org/sqlite`, không cần CGO)
- **Thư viện Discord**: `github.com/bwmarrin/discordgo`
- **Công cụ dịch**: API Chat Completions tương thích OpenAI (OpenAI, OpenRouter, Azure OpenAI, LLM cục bộ, v.v.)
- **Biên dịch chéo**: Hỗ trợ đầy đủ với `CGO_ENABLED=0` để tạo binary độc lập chạy trên Linux, Windows và macOS.

---

### Quy trình Tự Lưu trữ & Khởi chạy

#### 1. Tạo Bot Discord
1. Truy cập [Discord Developer Portal](https://discord.com/developers/applications) và tạo một Application mới.
2. Trong tab **Bot**, bật `MESSAGE CONTENT INTENT` và sao chép Bot Token.
3. Trong **OAuth2 → URL Generator**, chọn phạm vi `bot` và `applications.commands` cùng các quyền cần thiết rồi mời bot vào máy chủ.

#### 2. Chuẩn bị API tương thích OpenAI
Lấy URL endpoint API, API key và model ID từ nhà cung cấp LLM của bạn.

#### 3. Cấu hình Biến Môi trường
Sao chép `.env.example` thành `.env` và điền các tham số:

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

#### 4. Biên dịch & Chạy

Chạy trực tiếp trên máy cục bộ:
```sh
go run ./cmd/discord-auto-translator
```

Biên dịch binary độc lập và chạy:
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**Kiểm tra trước mô hình (`--model-prewarm`)**:
Xác thực thông tin đăng nhập API, kết nối mô hình và cấu trúc phản hồi trước khi triển khai:
```sh
./discord-auto-translator --model-prewarm
```

---

### Tham khảo Biến Môi trường

| Biến | Bắt buộc | Mặc định | Mô tả |
|---|---|---|---|
| `DISCORD_TOKEN` | **Có** | - | Token xác thực bot Discord |
| `OPENAI_BASE_URL` | **Có** | - | URL cơ sở của Chat Completions API (vd: `https://api.openai.com/v1`) |
| `OPENAI_API_KEY` | **Có** | - | Mã khóa Bearer API |
| `OPENAI_MODEL` | **Có** | - | ID mô hình LLM mục tiêu |
| `OPENAI_REASONING_EFFORT` | Không | (chưa đặt) | Tham số `reasoning_effort`. Đặt thành `none` để tắt token suy nghĩ trên các mô hình lai |
| `DB_PATH` | Không | `./translator.db` | Đường dẫn tệp cơ sở dữ liệu SQLite |
| `HTTP_ADDR` | Không | `:8080` | Địa chỉ máy chủ HTTP huy hiệu avatar |
| `PUBLIC_BASE_URL` | Không | (chưa đặt) | URL công khai cho huy hiệu avatar. Hiển thị vòng tròn màu theo vai trò cao nhất |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | Không | `100000` | Giới hạn token dịch mỗi phút cho mỗi máy chủ |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | Không | `120` | Giới hạn yêu cầu mỗi phút trên mỗi IP cho `/avatar` |
| `MESSAGE_LINK_RETENTION_DAYS` | Không | `0` | Thời gian lưu trữ liên kết tin nhắn (ngày). `0` = vô thời hạn |
| `GUILD_DATA_RETENTION_DAYS` | Không | `0` | Thời gian lưu dữ liệu máy chủ sau khi bot rời khỏi máy chủ (ngày) |

---

### Kiến trúc & Nguyên lý Thiết kế

#### 1. Quy trình Dịch thuật (Pipeline)
1. **Thu thập Ngữ cảnh**: Thu thập chủ đề kênh, ngữ cảnh cuộc trò chuyện gần nhất, trích dẫn phản hồi, siêu dữ liệu OGP và hình ảnh đã thu nhỏ.
2. **Che giấu bằng Ký hiệu (Masking)**: Thay thế lượt nhắc (`<@id>`), emoji (`<:name:id>`), kênh (`<#id>`), URL và khối mã bằng token (`[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]`) để tránh lỗi và chống tấn công Prompt Injection.
3. **Sắp xếp Prompt & Tối ưu Cache**: Tổ chức prompt thành các lớp tĩnh và động để tận dụng Prefix Prompt Caching của nhà cung cấp AI.
4. **Tạo Structured Outputs**: Sử dụng `response_format.type=json_schema` (`strict: true`) để nhận kết quả dịch cho tất cả ngôn ngữ đích trong một lần gọi API duy nhất.
5. **Hậu xử lý & Chuyển tiếp**: Khôi phục các ký hiệu, ghi lại liên kết Discord nội bộ, thay thế URL `hreflang` và phân phối tin nhắn Webhook song song.

#### 2. An toàn & Nguyên tắc Fail-Closed
- **Chống Prompt Injection**: Toàn bộ nội dung người dùng được thoát ký tự XML và cô lập trong các thẻ riêng biệt.
- **Nguyên tắc Fail-Closed**: Khi vượt quá giới hạn token (`finish_reason=length`), phản hồi JSON không hợp lệ hoặc lỗi mạng, bot hủy việc chuyển tiếp và gửi thông báo lỗi tại kênh gốc thay vì đăng nội dung hỏng.

#### 3. Độ tin cậy & Tính nhất quán Dữ liệu
- **Tính Idempotent**: `message_links` và `processed_events` ngăn chặn việc gửi trùng lặp tin nhắn khi nhận các sự kiện Gateway lặp lại.
- **Giao dịch Bù trừ**: Nếu lưu cơ sở dữ liệu thất bại sau khi gửi Webhook, tin nhắn Discord vừa gửi sẽ bị xóa ngay lập tức.
- **Đồng bộ Hai chiều**: Cảm xúc và tin nhắn ghim được đồng bộ trên toàn nhóm bất kể thao tác bắt nguồn từ kênh nào.

---

### Phát triển & Kiểm thử

#### Chạy kiểm thử
```sh
go test ./...
```

#### Danh mục Chuỗi Giao diện Đa ngôn ngữ (i18n)
Tất cả thông báo hiển thị cho người dùng được quản lý tập trung tại `internal/translatorbot/ui_strings.go` cho 13 ngôn ngữ. Việc thêm chuỗi mới yêu cầu định nghĩa cho tất cả các ngôn ngữ và được kiểm tra bởi `TestUIStringCatalogIsComplete`.

---

## 3. Giấy phép

Dự án này được phát hành theo giấy phép [MIT License](LICENSE).
