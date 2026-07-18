# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Bot Discord yang memungkinkan orang-orang yang berbicara dalam bahasa berbeda untuk mengobrol bersama di server yang sama.

Hubungkan satu saluran per bahasa menjadi sebuah **grup terjemahan**. Setiap pesan yang diposting di satu saluran langsung diterjemahkan oleh `@cf/google/gemma-4-26b-a4b-it` melalui Cloudflare AI Gateway dan dicerminkan ke semua saluran lain dalam grup — dengan nama dan avatar pengirim asli — sehingga setiap saluran terbaca seperti percakapan alami dalam bahasanya sendiri.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-id (Indonesia)
```

## Fitur

- **Semuanya tetap tersinkronisasi** — bukan hanya pesan baru: pengeditan, penghapusan, balasan, pesan yang diteruskan, reaksi, pin, utas (saluran teks / forum / media), dan pesan yang hanya berisi lampiran semuanya dicerminkan ke seluruh grup.
- **Pesan terlihat seperti dikirim langsung oleh pengirimnya** — pesan yang dicerminkan dikirim melalui webhook dengan nama dan avatar penulis asli.
- **Terjemahan yang natural** — Gemma 4 menggunakan nama saluran, topik, dan riwayat percakapan terkini sebagai konteks; glosarium per server memungkinkan Anda menetapkan terjemahan pilihan untuk nama dan istilah khusus.
- **Penanganan tautan yang cerdas** — tautan dan mention yang mengarah ke saluran atau pesan yang dikelola ditulis ulang ke padanannya di setiap bahasa, dan URL dengan alternatif `hreflang` diganti dengan versi dalam bahasa target.
- **Efisien dan aman** — pesan tanpa teks yang perlu diterjemahkan (URL, mention, emoji kustom, kode) dicerminkan tanpa memanggil API terjemahan; batas laju token per server berlaku; URL, mention, dan blok kode dilindungi dari injeksi prompt. Saat terjemahan gagal: fail-closed (tidak dicerminkan, notifikasi terlokalisasi di saluran sumber).
- **Antarmuka yang dilokalisasi** — respons perintah mengikuti bahasa klien Discord pengguna, dan notifikasi saluran menggunakan bahasa yang dikonfigurasi untuk saluran tersebut (13 bahasa, bahasa Inggris sebagai cadangan).

## Persyaratan

- Go 1.24 atau lebih baru
- Akun bot Discord dengan intent istimewa `MESSAGE CONTENT` diaktifkan
- ID akun Cloudflare
- Satu token API Cloudflare per lingkungan deployment
- ID Cloudflare AI Gateway

## Pengaturan

### 1. Siapkan bot Discord

1. Buat aplikasi di [Discord Developer Portal](https://discord.com/developers/applications)
2. Di halaman **Bot**:
   - Aktifkan `MESSAGE CONTENT INTENT` (wajib)
   - Salin token bot
3. Undang bot ke server Anda melalui **OAuth2 → URL Generator**:
   - Scopes: `bot`, `applications.commands`
   - Permissions (seperti ditampilkan di Developer Portal):
     - **Umum**: `View Channel`, `Read Message History`
     - **Pesan**: `Send Messages`, `Send Messages in Threads`
     - **Moderasi**: `Pin Messages`
     - **Webhook**: `Manage Webhooks`
     - **Utas**: `Create Public Threads`, `Manage Threads`
     - **Reaksi**: `Add Reactions`
   - Bilangan bulat permissions untuk di atas adalah `2252126768139328`
   - Untuk juga menyinkronkan reaksi emoji kustom dari server lain, berikan tambahan `Use External Emojis`; bilangan bulat permissions menjadi `2252126768401472`

### 2. Konfigurasi Cloudflare Workers AI dan AI Gateway

1. Buat **satu** token API Cloudflare per lingkungan deployment; cakupan ditentukan oleh operator.
2. Buat [AI Gateway](https://developers.cloudflare.com/ai-gateway/get-started/) untuk akun tersebut dan catat ID-nya (`CLOUDFLARE_AI_GATEWAY_ID`).
3. Di dasbor Gateway, **aktifkan cache** dan **nonaktifkan logging payload** (hanya metadata).
4. Biarkan retry, fallback, routing dinamis, DLP, dan guardrails **nonaktif** — bot tidak menggunakannya.

Atur `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_API_TOKEN`, dan `CLOUDFLARE_AI_GATEWAY_ID` di `.env`. Opsional: sesuaikan throughput dengan `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` (default `100000`).

### 3. Konfigurasi variabel lingkungan

```sh
cp .env.example .env
```

Edit `.env` dan atur nilai berikut:

```env
DISCORD_TOKEN=your-discord-bot-token
CLOUDFLARE_ACCOUNT_ID=your-cloudflare-account-id
CLOUDFLARE_API_TOKEN=your-cloudflare-api-token
CLOUDFLARE_AI_GATEWAY_ID=your-cloudflare-ai-gateway-id
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
# MESSAGE_LINK_RETENTION_DAYS=60
# GUILD_DATA_RETENTION_DAYS=30
```

| Variabel | Wajib | Deskripsi |
|---|---|---|
| `DISCORD_TOKEN` | Ya | Token bot Discord |
| `CLOUDFLARE_ACCOUNT_ID` | Ya | ID akun Cloudflare untuk Workers AI / AI Gateway |
| `CLOUDFLARE_API_TOKEN` | Ya | Satu token API per lingkungan deployment (cakupan ditentukan operator) |
| `CLOUDFLARE_AI_GATEWAY_ID` | Ya | ID AI Gateway; nilai berbeda per lingkungan jika perlu |
| `DB_PATH` | Tidak | Jalur ke file SQLite (default: `./translator.db`) |
| `HTTP_ADDR` | Tidak | Alamat server badge avatar (default: `:8080`) |
| `PUBLIC_BASE_URL` | Tidak | URL dasar publik untuk badge cincin avatar. Jika tidak diatur, pesan yang dicerminkan menggunakan URL avatar Discord asli dan server badge tidak digunakan |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | Tidak | Batas token Gemma 4 per server per menit (default: `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | Tidak | Batas permintaan per IP per menit untuk endpoint badge `/avatar` (default: `120`) |
| `MESSAGE_LINK_RETENTION_DAYS` | Tidak | Hari retensi `message_links` di SQLite sebelum pembersihan otomatis. `0` (default) menonaktifkan pembersihan; mis. `60` menghapus tautan lebih dari 60 hari saat startup dan setiap 24 jam |
| `GUILD_DATA_RETENTION_DAYS` | Tidak | Hari penyimpanan data SQLite server setelah bot dikeluarkan. `0` (default) menonaktifkan pembersihan; mis. `30` menghapus data server yang sudah lebih dari 30 hari ditinggalkan saat startup dan setiap 24 jam. Bergabung kembali sebelum tenggat membatalkan penghapusan terjadwal |

### Kontrak operasional Cloudflare AI Gateway

Terjemahan menggunakan `@cf/google/gemma-4-26b-a4b-it` melalui [Cloudflare AI Gateway](https://developers.cloudflare.com/ai-gateway/) yang dikonfigurasi. ID model tetap di kode dan tidak dapat diubah lewat variabel lingkungan.

Bot selalu mengarahkan permintaan terjemahan melalui `CLOUDFLARE_AI_GATEWAY_ID`. Konfigurasikan **satu** token API Cloudflare (`CLOUDFLARE_API_TOKEN`) per lingkungan deployment melalui variabel lingkungan; cakupan ditentukan oleh operator.

Parameter permintaan tetap: chat completions non-streaming, batas waktu HTTP **10 dtk**, **temperature 0.2**, **max_tokens 16384**, skema JSON strict multibahasa. Parameter reasoning/thinking dihilangkan (default penyedia).

**Gateway (pertahankan di dasbor Anda):**

- **Cache** — aktif; bot mengirim seluruh isi permintaan, tanpa header bypass cache atau kunci cache kustom
- **Logging** — hanya metadata; logging payload dinonaktifkan; bot mengirim `cf-aig-collect-log-payload: false` dan `cf-aig-metadata` hanya dengan `guild_id` dan `message_id`
- **Fitur nonaktif** — retry, fallback, routing dinamis, DLP, dan guardrails tidak digunakan

Bot tidak mencatat prompt, respons, atau token API. Deployment dan pemilihan lingkungan (ID Gateway / akun) menjadi tanggung jawab pengguna. Kegagalan terjemahan dan pelanggaran batas: **fail-closed** — pesan tidak dicerminkan; saluran sumber menerima notifikasi terlokalisasi.

Karena penawaran Cloudflare Workers AI untuk model ini masih beta, migrasi ini tidak menjalankan uji A/B langsung atau quality gate otomatis.

### 4. Jalankan

```sh
go run ./cmd/discord-auto-translator
```

Atau build lalu jalankan:

```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

## Penggunaan

Setelah bot dimulai, perintah slash terdaftar di setiap server.

### Menyiapkan saluran

#### Buat grup terjemahan

Jalankan `/new-channel` di saluran bahasa Jepang Anda untuk membuat grup terjemahan:

```
/new-channel language:ja
```

#### Tambahkan saluran dalam bahasa lain

Jalankan `/join-channel` di saluran bahasa Inggris Anda untuk menambahkannya ke grup:

```
/join-channel group:general language:en
```

Untuk menambahkan saluran bahasa Indonesia juga:

```
/join-channel group:general language:id
```

Sekarang `#general-ja`, `#general-en`, dan `#general-id` terhubung.

### Daftar perintah

Secara default, perintah slash admin hanya dapat dijalankan oleh **administrator server**. Untuk mengizinkan peran lain menggunakannya, buka "Pengaturan Server" Discord → "Integrasi" → "Kelola" bot → "Izin Perintah" dan konfigurasikan akses secara global atau per perintah. Bot tidak pernah mengubah peran atau izin perintah sendiri.

| Perintah | Deskripsi |
|---|---|
| `/new-channel language:[bahasa] channel:<saluran> group:<grup>` | Buat grup terjemahan baru. `channel` default ke saluran saat ini; `group` default ke nama saluran |
| `/join-channel group:[grup] language:[bahasa] channel:<saluran>` | Tambahkan saluran ke grup. `channel` default ke saluran saat ini |
| `/leave-channel group:[grup] channel:<saluran>` | Hapus saluran dari grup. `channel` default ke saluran saat ini |
| `/delete-group group:[grup]` | Hapus seluruh grup |
| `/list-groups` | Tampilkan grup terjemahan dan salurannya di server ini |
| `/add-glossary term:[istilah] translation:[terjemahan] attribute:<atribut> always_include:<bool>` | Daftarkan terjemahan pilihan di glosarium server. `attribute` adalah teks bebas dengan saran; `always_include` defaultnya `false` |
| `/list-glossary` | Tampilkan glosarium server |
| `/remove-glossary term:[istilah]` | Hapus entri glosarium |
| `/set-style group:[grup] preset:<preset> custom:<instruksi kustom>` | Atur gaya terjemahan untuk grup. Tentukan `preset` atau `custom`, bukan keduanya |
| `/bot-whitelist add source_type:[bot\|webhook] source_id:[ID]` | Izinkan sumber pesan otomatis di server ini. Untuk `source_type:bot`, `source_id` adalah ID pengguna bot; untuk `source_type:webhook`, nilainya adalah ID webhook |
| `/bot-whitelist remove source_type:[bot\|webhook] source_id:[ID]` | Hapus sumber pesan otomatis yang cocok dari daftar izin server ini |
| `/bot-whitelist list` | Tampilkan sumber bot dan webhook yang diizinkan di server ini |

- Daftar izin sumber disimpan di SQLite dan dibatasi untuk setiap server Discord (guild). Webhook keluaran yang dikelola penerjemah dan pesan dari bot penerjemah ini sendiri tetap dikecualikan meskipun ID-nya ditambahkan

- `language` menggunakan kode BCP-47 (`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr`, dll.)
- Maksimal 50 entri glosarium per server
- `attribute` menyarankan "nama orang", "nama tempat", "slang", "singkatan", dan "istilah teknis", tetapi nilai apa pun dapat dimasukkan secara bebas. Atribut digunakan sebagai konteks agar Gemma 4 memahami arti istilah tersebut
- Istilah biasa hanya ditambahkan ke instruksi sistem jika teks pesan yang akan diterjemahkan mengandung `term` (tidak peka huruf besar/kecil). Istilah dengan `always_include:true` selalu ditambahkan
- Jika opsi `channel` dihilangkan, perintah berlaku untuk saluran tempat perintah dijalankan
- Jenis saluran yang didukung: teks, berita, forum, dan media

## Pengujian

```sh
go test ./...
```

## Deploy ke GCE

Skrip deployment untuk Google Compute Engine disertakan di `deploy/deploy-gce.ps1` (Windows PowerShell).

Buat `deploy/deploy.json` dari contoh untuk pengaturan koneksi GCE. Pengaturan aplikasi dan rahasia menggunakan `.env` secara default; file lain dapat ditentukan melalui `envFile` di `deploy.json` atau `-EnvFile`.

```powershell
cp deploy/deploy.json.example deploy/deploy.json
cp .env.example .env
# Edit deploy.json dan .env

.\deploy\deploy-gce.ps1 -Bootstrap -UploadEnv   # Penyiapan awal
.\deploy\deploy-gce.ps1                          # Hanya pembaruan kode
.\deploy\deploy-gce.ps1 -UploadEnv               # Perbarui rahasia
```

## Lisensi

Lihat file [LICENSE](LICENSE) untuk lisensi proyek ini.
