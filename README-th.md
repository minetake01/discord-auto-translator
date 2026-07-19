# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

บอท Discord ที่ให้คนพูดภาษาต่างกันสามารถคุยด้วยกันในเซิร์ฟเวอร์เดียวกันได้

เชื่อมต่อแต่ละช่องทางตามภาษาให้เป็น **กลุ่มแปลภาษา** ทุกข้อความที่โพสต์ในช่องทางหนึ่งจะถูกแปลทันทีโดย `google.gemma-4-26b-a4b` ผ่าน Amazon Bedrock และสะท้อนไปยังช่องทางอื่น ๆ ทั้งหมดในกลุ่ม โดยยังคงชื่อและรูปอวาตาร์ของผู้ส่งต้นฉบับไว้ ทำให้แต่ละช่องทางอ่านได้เหมือนการสนทนาธรรมชาติในภาษาของตัวเอง

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-th (ภาษาไทย)
```

## คุณสมบัติ

- **ทุกอย่างซิงค์กัน** — ไม่ใช่แค่ข้อความใหม่ แต่การแก้ไข การลบ การตอบกลับ ข้อความที่ส่งต่อ รีแอ็กชัน การปักหมุด เธรด (ช่องทางข้อความ / ฟอรัม / มีเดีย) และข้อความที่มีแต่ไฟล์แนบ ล้วนถูกสะท้อนไปยังทั้งกลุ่ม
- **ข้อความดูเหมือนส่งมาจากผู้ส่งตัวจริง** — ข้อความที่สะท้อนจะส่งผ่านเว็บฮุกโดยใช้ชื่อและรูปอวาตาร์ของผู้เขียนต้นฉบับ
- **การแปลที่เป็นธรรมชาติ** — Gemma 4 26B-A4B ใช้ชื่อช่องทาง หัวข้อ และประวัติการสนทนาล่าสุดเป็นบริบท นอกจากนี้คำศัพท์เฉพาะของเซิร์ฟเวอร์ยังช่วยกำหนดการแปลที่ต้องการสำหรับชื่อและศัพท์เฉพาะได้
- **จัดการลิงก์อย่างชาญฉลาด** — ลิงก์และการกล่าวถึงที่ชี้ไปยังช่องทางหรือข้อความที่จัดการอยู่จะถูกเขียนใหม่ให้ตรงกับช่องทางในแต่ละภาษา และ URL ที่มีทางเลือก `hreflang` จะถูกแทนที่ด้วยเวอร์ชันภาษาเป้าหมาย
- **มีประสิทธิภาพและปลอดภัย** — ข้อความที่ไม่มีข้อความให้แปล (URL เมนชัน อีโมจิที่กำหนดเอง โค้ด) จะถูกสะท้อนโดยไม่เรียก API แปล มีการจำกัดอัตราโทเค็นต่อเซิร์ฟเวอร์ และ URL เมนชัน และบล็อกโค้ดได้รับการปกป้องจาก prompt injection เมื่อแปลล้มเหลว: fail-closed (ไม่สะท้อน แจ้งเตือนในช่องต้นทาง)
- **อินเทอร์เฟซที่แปลเป็นภาษาท้องถิ่น** — การตอบสนองคำสั่งจะเป็นไปตามภาษาไคลเอนต์ Discord ของผู้ใช้ และการแจ้งเตือนช่องทางจะใช้ภาษาที่กำหนดไว้สำหรับช่องทางนั้น (รองรับ 13 ภาษา ใช้ภาษาอังกฤษเป็นค่าสำรอง)

## ความต้องการ

- Go 1.24 หรือใหม่กว่า
- บัญชีบอท Discord ที่เปิดใช้งาน intent สิทธิพิเศษ `MESSAGE CONTENT`
- บัญชี AWS ที่ใช้ Amazon Bedrock ได้ และคีย์ IAM ที่อนุญาตให้สร้าง inference ใน Mantle Project เริ่มต้นของ `us-west-2`
- Amazon Bedrock ID

## การตั้งค่า

### 1. เตรียมบอท Discord

1. สร้างแอปพลิเคชันใน [Discord Developer Portal](https://discord.com/developers/applications)
2. ที่หน้า **Bot**:
   - เปิดใช้งาน `MESSAGE CONTENT INTENT` (จำเป็น)
   - คัดลอกโทเค็นบอท
3. เชิญบอทเข้าเซิร์ฟเวอร์ผ่าน **OAuth2 → URL Generator**:
   - Scopes: `bot`, `applications.commands`
   - Permissions (ตามที่แสดงใน Developer Portal):
     - **ทั่วไป**: `View Channel`, `Read Message History`
     - **ข้อความ**: `Send Messages`, `Send Messages in Threads`
     - **การกลั่นกรอง**: `Pin Messages`
     - **เว็บฮุก**: `Manage Webhooks`
     - **เธรด**: `Create Public Threads`, `Manage Threads`
     - **รีแอ็กชัน**: `Add Reactions`
   - ค่าจำนวนเต็มของสิทธิ์สำหรับข้างต้นคือ `2252126768139328`
   - หากต้องการซิงค์รีแอ็กชันอีโมจิที่กำหนดเองจากเซิร์ฟเวอร์อื่นด้วย ให้อนุญาต `Use External Emojis` เพิ่มเติม ค่าจำนวนเต็มของสิทธิ์จะเป็น `2252126768401472`

### 2. ตั้งค่า Amazon Bedrock

เปิดใช้ `google.gemma-4-26b-a4b` ใน Amazon Bedrock ภูมิภาค `us-west-2` สร้างผู้ใช้ IAM ที่มีเฉพาะ `bedrock-mantle:CreateInference` สำหรับโมเดล แล้วกำหนด `AWS_ACCESS_KEY_ID` และ `AWS_SECRET_ACCESS_KEY` ใน `.env` โมเดล ภูมิภาค timeout 30 วินาที และขีดจำกัด 4096 token ถูกกำหนดในโค้ด

### 3. กำหนดค่าตัวแปรสภาพแวดล้อม

```sh
cp .env.example .env
```

แก้ไข `.env` และตั้งค่าต่อไปนี้:

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

| ตัวแปร | จำเป็น | คำอธิบาย |
|---|---|---|
| `DISCORD_TOKEN` | ใช่ | โทเค็นบอท Discord |
| `AWS_ACCESS_KEY_ID` | Yes | Access key ID for the dedicated Bedrock IAM user |
| `AWS_SECRET_ACCESS_KEY` | Yes | Secret access key for the dedicated Bedrock IAM user |
| `DB_PATH` | ไม่ | เส้นทางไฟล์ SQLite (ค่าเริ่มต้น: `./translator.db`) |
| `HTTP_ADDR` | ไม่ | ที่อยู่เซิร์ฟเวอร์แบดจ์อวาตาร์ (ค่าเริ่มต้น: `:8080`) |
| `PUBLIC_BASE_URL` | ไม่ | URL พื้นฐานสาธารณะสำหรับแบดจ์วงแหวนอวาตาร์ หากไม่ตั้งค่า ข้อความที่สะท้อนจะใช้ URL อวาตาร์ Discord เดิม และเซิร์ฟเวอร์แบดจ์จะไม่ถูกใช้งาน |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | ไม่ | จำกัดโทเค็น Gemma 4 26B-A4B ต่อเซิร์ฟเวอร์ต่อนาที (ค่าเริ่มต้น: `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | ไม่ | จำกัดคำขอต่อ IP ต่อนาทีสำหรับ endpoint แบดจ์ `/avatar` (ค่าเริ่มต้น: `120`) |
| `MESSAGE_LINK_RETENTION_DAYS` | ไม่ | จำนวนวันเก็บ `message_links` ใน SQLite ก่อนลบอัตโนมัติ `0` (ค่าเริ่มต้น) ปิดการลบ เช่น `60` จะลบลิงก์ที่เก่ากว่า 60 วันตอนเริ่มและทุก 24 ชั่วโมง |
| `GUILD_DATA_RETENTION_DAYS` | ไม่ | จำนวนวันที่เก็บข้อมูล SQLite ของเซิร์ฟเวอร์หลังนำบอทออก `0` (ค่าเริ่มต้น) ปิดการลบ เช่น `30` จะลบข้อมูลของเซิร์ฟเวอร์ที่นำบอทออกเกิน 30 วันตอนเริ่มและทุก 24 ชั่วโมง หากเข้าร่วมใหม่ก่อนครบกำหนดจะยกเลิกการลบที่ตั้งไว้ |

### สัญญาการดำเนินงาน Amazon Bedrock

การแปลใช้ Mantle Responses API แบบไม่สตรีมกับ `google.gemma-4-26b-a4b` ใน `us-west-2` โดยมี timeout **30 วินาที**, **provider-default temperature 1.0**, **max_output_tokens 4096** และ JSON ที่กำกับด้วย schema และตรวจสอบอย่างเข้มงวดโดย Bot ทุกภาษาถูกสร้างในคำขอเดียว ขีดจำกัด 4K การหยุดผิดปกติ หรือ JSON ไม่ถูกต้องจะทำให้ทั้งหมดล้มเหลวแบบ fail-closed ไม่มี retry การแบ่ง หรือ fallback บอทไม่บันทึก prompt คำตอบ หรือข้อมูลรับรอง การ deploy บน GCE ตรวจสอบข้อมูลรับรอง การเข้าถึงโมเดล และสัญญาการตอบกลับ ก่อนแทนที่ด้วย `--bedrock-prewarm` ภายในห้านาที

### 4. เรียกใช้

```sh
go run ./cmd/discord-auto-translator
```

หรือสร้างแล้วเรียกใช้:

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## การใช้งาน

เมื่อบอทเริ่มต้น คำสั่ง slash จะถูกลงทะเบียนในแต่ละเซิร์ฟเวอร์

### ตั้งค่าช่องทาง

#### สร้างกลุ่มแปลภาษา

เรียกใช้ `/new-channel` ในช่องทางภาษาญี่ปุ่นเพื่อสร้างกลุ่มแปลภาษา:

```
/new-channel language:ja
```

#### เพิ่มช่องทางในภาษาอื่น

เรียกใช้ `/join-channel` ในช่องทางภาษาอังกฤษเพื่อเพิ่มเข้ากลุ่ม:

```
/join-channel group:general language:en
```

หากต้องการเพิ่มช่องทางภาษาไทยด้วย:

```
/join-channel group:general language:th
```

ตอนนี้ `#general-ja`, `#general-en` และ `#general-th` เชื่อมต่อกันแล้ว

### รายการคำสั่ง

ตามค่าเริ่มต้น คำสั่ง slash สำหรับผู้ดูแลระบบสามารถเรียกใช้ได้โดย **ผู้ดูแลเซิร์ฟเวอร์** เท่านั้น หากต้องการอนุญาตให้บทบาทอื่นใช้งาน ไปที่ "การตั้งค่าเซิร์ฟเวอร์" Discord → "การรวมระบบ" → "จัดการ" ของบอท → "สิทธิ์คำสั่ง" และกำหนดการเข้าถึงแบบทั่วโลกหรือรายคำสั่ง บอทจะไม่เปลี่ยนบทบาทหรือสิทธิ์คำสั่งเอง

| คำสั่ง | คำอธิบาย |
|---|---|
| `/new-channel language:[ภาษา] channel:<ช่อง> group:<กลุ่ม>` | สร้างกลุ่มแปลภาษาใหม่ หากไม่ระบุ `channel` จะใช้ช่องที่รันคำสั่ง หากไม่ระบุ `group` จะใช้ชื่อช่อง |
| `/join-channel group:[กลุ่ม] language:[ภาษา] channel:<ช่อง>` | เพิ่มช่องทางเข้ากลุ่ม หากไม่ระบุ `channel` จะใช้ช่องที่รันคำสั่ง |
| `/leave-channel group:[กลุ่ม] channel:<ช่อง>` | ลบช่องทางออกจากกลุ่ม หากไม่ระบุ `channel` จะใช้ช่องที่รันคำสั่ง |
| `/delete-group group:[กลุ่ม]` | ลบทั้งกลุ่ม |
| `/list-groups` | แสดงกลุ่มแปลภาษาและช่องทางในเซิร์ฟเวอร์นี้ |
| `/add-glossary term:[คำศัพท์] translation:[การแปล] attribute:<คุณลักษณะ> always_include:<บูลีน>` | ลงทะเบียนการแปลที่ต้องการในอภิธานศัพท์ของเซิร์ฟเวอร์ `attribute` คือข้อความอิสระพร้อมคำแนะนำ ค่าเริ่มต้นของ `always_include` คือ `false` |
| `/list-glossary` | แสดงรายการอภิธานศัพท์ของเซิร์ฟเวอร์ |
| `/remove-glossary term:[คำศัพท์]` | ลบรายการในอภิธานศัพท์ |
| `/set-style group:[กลุ่ม] preset:<พรีเซ็ต> custom:<คำสั่งกำหนดเอง>` | ตั้งค่าสไตล์การแปลของกลุ่ม ระบุ `preset` หรือ `custom` อย่างใดอย่างหนึ่ง ไม่ใช่ทั้งสอง |
| `/bot-whitelist add source_type:[bot\|webhook] source_id:[ID]` | อนุญาตแหล่งที่มาของข้อความอัตโนมัติในเซิร์ฟเวอร์นี้ เมื่อ `source_type:bot` ค่า `source_id` คือ ID ผู้ใช้บอท และเมื่อ `source_type:webhook` คือ ID เว็บฮุก |
| `/bot-whitelist remove source_type:[bot\|webhook] source_id:[ID]` | ลบแหล่งที่มาของข้อความอัตโนมัติที่ตรงกันออกจากรายการอนุญาตของเซิร์ฟเวอร์นี้ |
| `/bot-whitelist list` | แสดงแหล่งที่มาประเภทบอทและเว็บฮุกที่อนุญาตในเซิร์ฟเวอร์นี้ |

- รายการแหล่งที่มาที่อนุญาตจะถูกบันทึกถาวรใน SQLite และแยกตามเซิร์ฟเวอร์ Discord (กิลด์) เว็บฮุกเอาต์พุตที่บอทแปลภาษาจัดการและข้อความจากบอทแปลภาษาเองยังคงถูกยกเว้น แม้จะเพิ่ม ID แล้วก็ตาม

- `language` ใช้รหัส BCP-47 (`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr` เป็นต้น)
- สูงสุด 50 รายการอภิธานศัพท์ต่อเซิร์ฟเวอร์
- `attribute` แนะนำ "ชื่อบุคคล" "ชื่อสถานที่" "คำแสลง" "ตัวย่อ" และ "คำศัพท์เทคนิค" แต่สามารถป้อนค่าใดก็ได้อย่างอิสระ คุณลักษณะนี้ใช้เป็นบริบทให้ Gemma 4 26B-A4B เข้าใจความหมายของคำศัพท์
- คำศัพท์ทั่วไปจะถูกเพิ่มในคำสั่งระบบเฉพาะเมื่อข้อความที่จะแปลมี `term` (ไม่คำนึงตัวพิมพ์เล็ก/ใหญ่) คำศัพท์ที่มี `always_include:true` จะถูกเพิ่มเสมอ
- หากละเว้นตัวเลือก `channel` คำสั่งจะใช้กับช่องทางที่เรียกใช้คำสั่ง
- ประเภทช่องทางที่รองรับ: ข้อความ ข่าวสาร ฟอรัม และมีเดีย

## การทดสอบ

```sh
go test ./...
```

## การปรับใช้บน GCE

สคริปต์การปรับใช้สำหรับ Google Compute Engine รวมอยู่ใน `deploy/deploy-gce.ps1` (Windows PowerShell)

สร้าง `deploy/deploy.json` จากตัวอย่างสำหรับการตั้งค่าการเชื่อมต่อ GCE การตั้งค่าแอปและความลับใช้ `.env` เป็นค่าเริ่มต้น สามารถระบุไฟล์อื่นผ่าน `envFile` ใน `deploy.json` หรือ `-EnvFile`

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# แก้ไข deploy.json และ .env

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # ตั้งค่าครั้งแรก
.\deploy\deploy-gce.ps1                          # อัปเดตโค้ดเท่านั้น
.\deploy\deploy-gce.ps1 -UploadEnv               # อัปเดตความลับ
```

## ใบอนุญาต

ดูไฟล์ [LICENSE](LICENSE) สำหรับใบอนุญาตของโปรเจกต์นี้
