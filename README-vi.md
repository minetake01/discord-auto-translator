# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Bot Discord giúp những người nói các ngôn ngữ khác nhau có thể trò chuyện cùng nhau trong cùng một máy chủ.

Liên kết một kênh mỗi ngôn ngữ thành một **nhóm dịch thuật**. Mỗi tin nhắn được đăng trên một kênh sẽ được `google.gemma-4-26b-a4b` qua Amazon Bedrock dịch ngay lập tức và phản chiếu đến tất cả các kênh khác trong nhóm — giữ nguyên tên và ảnh đại diện của người gửi gốc — để mỗi kênh đọc như một cuộc trò chuyện tự nhiên bằng ngôn ngữ của mình.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-vi (Tiếng Việt)
```

## Tính năng

- **Mọi thứ được đồng bộ hóa** — không chỉ tin nhắn mới: chỉnh sửa, xóa, trả lời, tin nhắn được chuyển tiếp, phản ứng, ghim, chủ đề (kênh văn bản / diễn đàn / phương tiện) và tin nhắn chỉ có tệp đính kèm đều được phản chiếu trên toàn nhóm.
- **Tin nhắn trông như được gửi bởi chính người gửi** — các tin nhắn phản chiếu được gửi qua webhook với tên và ảnh đại diện của tác giả gốc.
- **Dịch thuật tự nhiên** — Gemma 4 26B-A4B sử dụng tên kênh, chủ đề và lịch sử cuộc trò chuyện gần đây làm ngữ cảnh; bảng thuật ngữ theo từng máy chủ cho phép cố định các bản dịch ưa thích cho tên riêng và thuật ngữ chuyên ngành.
- **Xử lý liên kết thông minh** — các liên kết và lượt đề cập trỏ đến kênh hoặc tin nhắn được quản lý sẽ được viết lại thành các tương đương trong mỗi ngôn ngữ, và các URL có lựa chọn thay thế `hreflang` sẽ được thay bằng phiên bản ngôn ngữ đích.
- **Hiệu quả và an toàn** — các tin nhắn không có văn bản cần dịch (URL, đề cập, emoji tùy chỉnh, mã) được phản chiếu mà không gọi API dịch; giới hạn tỷ lệ token theo từng máy chủ được áp dụng; URL, đề cập và khối mã được bảo vệ khỏi việc tiêm prompt. Khi dịch thất bại: fail-closed (không phản chiếu, thông báo bản địa hóa ở kênh nguồn).
- **Giao diện được bản địa hóa** — phản hồi lệnh tuân theo ngôn ngữ ứng dụng Discord của người dùng, và thông báo kênh sử dụng ngôn ngữ được cấu hình cho kênh đó (13 ngôn ngữ, tiếng Anh là dự phòng).

## Yêu cầu

- Go 1.24 trở lên
- Tài khoản bot Discord với intent đặc quyền `MESSAGE CONTENT` được bật
- Tài khoản AWS có quyền dùng Amazon Bedrock và khóa IAM được phép tạo suy luận trong Mantle Project `your-aws-bedrock-project-id` tại `your-aws-bedrock-region`.
- ID Amazon Bedrock

## Cài đặt

### 1. Chuẩn bị bot Discord

1. Tạo ứng dụng trong [Discord Developer Portal](https://discord.com/developers/applications)
2. Trên trang **Bot**:
   - Bật `MESSAGE CONTENT INTENT` (bắt buộc)
   - Sao chép token bot
3. Mời bot vào máy chủ của bạn qua **OAuth2 → URL Generator**:
   - Scopes: `bot`, `applications.commands`
   - Permissions (như hiển thị trong Developer Portal):
     - **Chung**: `View Channel`, `Read Message History`
     - **Tin nhắn**: `Send Messages`, `Send Messages in Threads`
     - **Kiểm duyệt**: `Pin Messages`
     - **Webhook**: `Manage Webhooks`
     - **Chủ đề**: `Create Public Threads`, `Manage Threads`
     - **Phản ứng**: `Add Reactions`
   - Số nguyên permissions cho những điều trên là `2252126768139328`
   - Để cũng đồng bộ hóa phản ứng emoji tùy chỉnh từ các máy chủ khác, hãy cấp thêm `Use External Emojis`; số nguyên permissions sẽ là `2252126768401472`

### 2. Cấu hình Amazon Bedrock

1. Bật `google.gemma-4-26b-a4b` trong Amazon Bedrock tại `your-aws-bedrock-region`.
2. Tạo người dùng IAM riêng và chỉ cho phép `bedrock-mantle:CreateInference` trên `arn:aws:bedrock-mantle:your-aws-bedrock-region:<account-id>:project/your-aws-bedrock-project-id`.
3. Tạo access key rồi đặt `AWS_ACCESS_KEY_ID` và `AWS_SECRET_ACCESS_KEY` trong `.env`.

Model, timeout và giới hạn đầu ra được cố định trong mã. Region và Project ID là cấu hình deployment cục bộ bắt buộc qua `AWS_BEDROCK_REGION` và `AWS_BEDROCK_PROJECT_ID`. `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` (mặc định `100000`) có thể điều chỉnh giới hạn token theo máy chủ.
### 3. Cấu hình biến môi trường

```sh
cp .env.example .env
```

Chỉnh sửa `.env` và đặt các giá trị sau:

```env
DISCORD_TOKEN=your-discord-bot-token
AWS_ACCESS_KEY_ID=your-aws-access-key-id
AWS_SECRET_ACCESS_KEY=your-aws-secret-access-key
AWS_BEDROCK_REGION=your-aws-bedrock-region
AWS_BEDROCK_PROJECT_ID=your-aws-bedrock-project-id
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
# MESSAGE_LINK_RETENTION_DAYS=60
# GUILD_DATA_RETENTION_DAYS=30
```

| Biến | Bắt buộc | Mô tả |
|---|---|---|
| `DISCORD_TOKEN` | Có | Token bot Discord |
| `AWS_ACCESS_KEY_ID` | Yes | Access key ID for the dedicated Bedrock IAM user |
| `AWS_SECRET_ACCESS_KEY` | Yes | Secret access key for the dedicated Bedrock IAM user |
| `AWS_BEDROCK_REGION` | Yes | Bedrock Mantle region, such as `your-aws-bedrock-region` |
| `AWS_BEDROCK_PROJECT_ID` | Yes | Bedrock Mantle Project ID, such as `your-aws-bedrock-project-id` |
| `DB_PATH` | Không | Đường dẫn đến tệp SQLite (mặc định: `./translator.db`) |
| `HTTP_ADDR` | Không | Địa chỉ máy chủ badge ảnh đại diện (mặc định: `:8080`) |
| `PUBLIC_BASE_URL` | Không | URL cơ sở công khai cho badge vòng ảnh đại diện. Nếu không đặt, tin nhắn phản chiếu dùng URL ảnh đại diện Discord gốc và máy chủ badge không được sử dụng |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | Không | Giới hạn token Gemma 4 26B-A4B mỗi máy chủ mỗi phút (mặc định: `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | Không | Giới hạn yêu cầu mỗi IP mỗi phút cho endpoint badge `/avatar` (mặc định: `120`) |
| `MESSAGE_LINK_RETENTION_DAYS` | Không | Số ngày giữ `message_links` trong SQLite trước khi dọn tự động. `0` (mặc định) tắt dọn; ví dụ `60` xóa liên kết cũ hơn 60 ngày khi khởi động và mỗi 24 giờ |
| `GUILD_DATA_RETENTION_DAYS` | Không | Số ngày giữ dữ liệu SQLite của máy chủ sau khi bot bị gỡ. `0` (mặc định) tắt dọn; ví dụ `30` xóa dữ liệu của máy chủ đã gỡ bot quá 30 ngày khi khởi động và mỗi 24 giờ. Tham gia lại trước hạn sẽ hủy lịch xóa |

### Hợp đồng vận hành Amazon Bedrock

Bản dịch dùng Mantle Responses API không streaming với `google.gemma-4-26b-a4b` tại `your-aws-bedrock-region`; mọi yêu cầu được gán cho Project `your-aws-bedrock-project-id` qua header `OpenAI-Project`: timeout **30 giây**, **provider-default temperature 1.0**, **max_output_tokens 4096** và JSON theo schema được bot kiểm tra nghiêm ngặt. Mọi ngôn ngữ được tạo trong một yêu cầu. Giới hạn 4K, dừng bất thường hoặc JSON không hợp lệ làm toàn bộ thất bại theo fail-closed; không retry, chia nhỏ hay fallback. Bot không ghi prompt, phản hồi hoặc thông tin xác thực. Deploy GCE xác minh thông tin xác thực, quyền truy cập mô hình và hợp đồng phản hồi trước khi thay thế bằng `--bedrock-prewarm` với giới hạn năm phút.

### 4. Chạy

```sh
go run ./cmd/discord-auto-translator
```

Hoặc build rồi chạy:

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## Sử dụng

Khi bot khởi động, các lệnh slash được đăng ký trong từng máy chủ.

### Thiết lập kênh

#### Tạo nhóm dịch thuật

Chạy `/new-channel` trong kênh tiếng Nhật của bạn để tạo nhóm dịch thuật:

```
/new-channel language:ja
```

#### Thêm kênh bằng ngôn ngữ khác

Chạy `/join-channel` trong kênh tiếng Anh của bạn để thêm vào nhóm:

```
/join-channel group:general language:en
```

Để thêm cả kênh tiếng Việt:

```
/join-channel group:general language:vi
```

Bây giờ `#general-ja`, `#general-en` và `#general-vi` được liên kết.

### Danh sách lệnh

Theo mặc định, các lệnh slash quản trị chỉ có thể được chạy bởi **quản trị viên máy chủ**. Để cho phép các vai trò khác sử dụng, vào "Cài đặt Máy chủ" Discord → "Tích hợp" → "Quản lý" của bot → "Quyền lệnh" và cấu hình quyền truy cập toàn cầu hoặc theo từng lệnh. Bot không bao giờ tự thay đổi vai trò hay quyền lệnh.

| Lệnh | Mô tả |
|---|---|
| `/new-channel language:[ngôn ngữ] channel:<kênh> group:<nhóm>` | Tạo nhóm dịch thuật mới. Bỏ qua `channel` sẽ dùng kênh hiện tại; bỏ qua `group` sẽ dùng tên kênh |
| `/join-channel group:[nhóm] language:[ngôn ngữ] channel:<kênh>` | Thêm kênh vào nhóm. Bỏ qua `channel` sẽ dùng kênh hiện tại |
| `/leave-channel group:[nhóm] channel:<kênh>` | Xóa kênh khỏi nhóm. Bỏ qua `channel` sẽ dùng kênh hiện tại |
| `/delete-group group:[nhóm]` | Xóa toàn bộ nhóm |
| `/list-groups` | Liệt kê các nhóm dịch và kênh trên máy chủ này |
| `/add-glossary term:[thuật ngữ] translation:[bản dịch] attribute:<thuộc tính> always_include:<bool>` | Đăng ký bản dịch ưa thích trong bảng thuật ngữ của máy chủ. `attribute` là văn bản tự do có gợi ý; `always_include` mặc định là `false` |
| `/list-glossary` | Liệt kê bảng thuật ngữ của máy chủ |
| `/remove-glossary term:[thuật ngữ]` | Xóa mục trong bảng thuật ngữ |
| `/set-style group:[nhóm] preset:<preset> custom:<chỉ dẫn tùy chỉnh>` | Đặt phong cách dịch cho nhóm. Chỉ định `preset` hoặc `custom`, không phải cả hai |
| `/bot-whitelist add source_type:[bot\|webhook] source_id:[ID]` | Cho phép nguồn tin nhắn tự động trong máy chủ này. Với `source_type:bot`, `source_id` là ID người dùng bot; với `source_type:webhook`, đó là ID webhook |
| `/bot-whitelist remove source_type:[bot\|webhook] source_id:[ID]` | Xóa nguồn tin nhắn tự động tương ứng khỏi danh sách cho phép của máy chủ này |
| `/bot-whitelist list` | Liệt kê các nguồn bot và webhook được cho phép trong máy chủ này |

- Danh sách nguồn được phép được lưu bền vững trong SQLite và giới hạn theo từng máy chủ Discord (guild). Webhook đầu ra do bot dịch quản lý và tin nhắn của chính bot dịch vẫn bị loại trừ ngay cả khi ID của chúng được thêm vào

- `language` sử dụng mã BCP-47 (`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr`, v.v.)
- Tối đa 50 mục thuật ngữ mỗi máy chủ
- `attribute` gợi ý "tên người", "tên địa điểm", "tiếng lóng", "từ viết tắt" và "thuật ngữ kỹ thuật", nhưng có thể nhập bất kỳ giá trị nào tùy ý. Thuộc tính được sử dụng làm ngữ cảnh để Gemma 4 26B-A4B hiểu ý nghĩa của thuật ngữ
- Các thuật ngữ thông thường chỉ được thêm vào hướng dẫn hệ thống khi nội dung tin nhắn cần dịch có chứa `term` (không phân biệt chữ hoa/thường). Các thuật ngữ có `always_include:true` luôn được thêm vào
- Nếu tùy chọn `channel` bị bỏ qua, lệnh sẽ áp dụng cho kênh nơi lệnh được chạy
- Các loại kênh được hỗ trợ: văn bản, tin tức, diễn đàn và phương tiện

## Kiểm thử

```sh
go test ./...
```

## Triển khai lên GCE

Script triển khai cho Google Compute Engine được bao gồm tại `deploy/deploy-gce.ps1` (Windows PowerShell).

Tạo `deploy/deploy.json` từ file mẫu cho cài đặt kết nối GCE. Cấu hình app và secret mặc định dùng `.env`; file khác có thể chỉ định qua `envFile` trong `deploy.json` hoặc `-EnvFile`.

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# Chỉnh sửa deploy.json và .env

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # Thiết lập lần đầu
.\deploy\deploy-gce.ps1                          # Chỉ cập nhật code
.\deploy\deploy-gce.ps1 -UploadEnv               # Cập nhật secret
```

## Giấy phép

Xem tệp [LICENSE](LICENSE) để biết giấy phép của dự án này.
