# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Bot Discord yang memungkinkan orang-orang yang berbicara dalam bahasa berbeda untuk mengobrol bersama di server yang sama.

Hubungkan satu saluran per bahasa menjadi sebuah **grup terjemahan**. Setiap pesan yang diposting di satu saluran langsung diterjemahkan oleh Google Gemini dan dicerminkan ke semua saluran lain dalam grup — dengan nama dan avatar pengirim asli — sehingga setiap saluran terbaca seperti percakapan alami dalam bahasanya sendiri.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-id (Indonesia)
```

## Fitur

- **Semuanya tetap tersinkronisasi** — bukan hanya pesan baru: pengeditan, penghapusan, balasan, pesan yang diteruskan, reaksi, pin, utas (saluran teks / forum / media), dan pesan yang hanya berisi lampiran semuanya dicerminkan ke seluruh grup.
- **Pesan terlihat seperti dikirim langsung oleh pengirimnya** — pesan yang dicerminkan dikirim melalui webhook dengan nama dan avatar penulis asli.
- **Terjemahan yang natural** — Gemini menggunakan nama saluran, topik, dan riwayat percakapan terkini sebagai konteks; glosarium per server memungkinkan Anda menetapkan terjemahan pilihan untuk nama dan istilah khusus.
- **Penanganan tautan yang cerdas** — tautan dan mention yang mengarah ke saluran atau pesan yang dikelola ditulis ulang ke padanannya di setiap bahasa, dan URL dengan alternatif `hreflang` diganti dengan versi dalam bahasa target.
- **Efisien dan aman** — pesan tanpa teks yang perlu diterjemahkan (URL, mention, emoji kustom, kode) dicerminkan tanpa memanggil API Gemini; batas laju token per server berlaku; URL, mention, dan blok kode dilindungi dari injeksi prompt.
- **Antarmuka yang dilokalisasi** — respons perintah mengikuti bahasa klien Discord pengguna, dan notifikasi saluran menggunakan bahasa yang dikonfigurasi untuk saluran tersebut (13 bahasa, bahasa Inggris sebagai cadangan).

## Persyaratan

- Go 1.24 atau lebih baru
- Akun bot Discord dengan intent istimewa `MESSAGE CONTENT` diaktifkan
- Kunci API Google Gemini

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

### 2. Dapatkan kunci API Gemini

Dapatkan kunci API dari [Google AI Studio](https://aistudio.google.com/).

### 3. Konfigurasi variabel lingkungan

```sh
cp .env.example .env
```

Edit `.env` dan atur nilai berikut:

```env
DISCORD_TOKEN=your-discord-bot-token
GEMINI_API_KEY=your-gemini-api-key
DB_PATH=./translator.db
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://your-public-domain.example
GEMINI_RATE_LIMIT_TOKENS_PER_MIN=100000
AVATAR_RATE_LIMIT_REQUESTS_PER_MIN=120
```

| Variabel | Wajib | Deskripsi |
|---|---|---|
| `DISCORD_TOKEN` | Ya | Token bot Discord |
| `GEMINI_API_KEY` | Ya | Kunci API Gemini |
| `DB_PATH` | Tidak | Jalur ke file SQLite (default: `./translator.db`) |
| `HTTP_ADDR` | Tidak | Alamat server badge avatar (default: `:8080`) |
| `PUBLIC_BASE_URL` | Tidak | URL dasar publik untuk badge cincin avatar. Jika tidak diatur, pesan yang dicerminkan menggunakan URL avatar Discord asli dan server badge tidak digunakan |
| `GEMINI_RATE_LIMIT_TOKENS_PER_MIN` | Tidak | Batas token Gemini per server per menit (default: `100000`) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | Tidak | Batas permintaan per IP per menit untuk endpoint badge `/avatar` (default: `120`) |

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

- `language` menggunakan kode BCP-47 (`en`, `ja`, `zh-CN`, `pt-BR`, `ko`, `fr`, dll.)
- Maksimal 50 entri glosarium per server
- `attribute` menyarankan "nama orang", "nama tempat", "slang", "singkatan", dan "istilah teknis", tetapi nilai apa pun dapat dimasukkan secara bebas. Atribut digunakan sebagai konteks agar Gemini memahami arti istilah tersebut
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
