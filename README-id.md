# Discord Auto Translator

[English](README.md) | [日本語](README-ja.md) | [简体中文](README-zh-CN.md) | [繁體中文](README-zh-TW.md) | [한국어](README-ko.md) | [Français](README-fr.md) | [Deutsch](README-de.md) | [Español](README-es.md) | [Português (Brasil)](README-pt-BR.md) | [Italiano](README-it.md) | [Bahasa Indonesia](README-id.md) | [ไทย](README-th.md) | [Tiếng Việt](README-vi.md)

Bot Discord yang memungkinkan orang-orang dengan bahasa yang berbeda untuk berkomunikasi secara real-time di server Discord yang sama, masing-masing menggunakan bahasa ibu mereka.

Dengan menautkan satu saluran per bahasa ke dalam sebuah **grup penerjemahan**, setiap pesan yang dikirim di salah satu saluran akan secara otomatis diterjemahkan oleh LLM yang kompatibel dengan OpenAI (API Chat Completions) dan dicerminkan ke semua saluran lain dalam grup menggunakan **nama dan avatar pengirim asli**. Dengan demikian, setiap saluran terasa seperti percakapan alami dalam bahasanya sendiri.

```
#chat-ja (日本語)  ⇄  #chat-en (English)  ⇄  #chat-zh (中文)
```

---

## 1. Panduan untuk Pengguna & Administrator Server

### Fitur Utama & Pengalaman Pengguna

- **Mengobrol Secara Alami Seperti Biasa**
  Tidak memerlukan perintah atau awalan khusus. Cukup kirim pesan seperti biasa: pesan akan diterjemahkan dan disinkronkan secara real-time ke saluran tertaut lainnya.
- **Pesan Mempertahankan Identitas Pengirim Asli**
  Pesan terjemahan dikirim melalui Webhook Discord, menjaga nama tampilan dan avatar pengirim asli secara mulus.
- **Sinkronisasi Dua Arah Real-Time yang Lengkap**
  - **Pesan Baru & Lampiran**: Mendukung teks, gambar (termasuk deskripsi gambar / teks alt), dan berbagai lampiran file.
  - **Edit & Hapus Pesan**: Mengedit atau menghapus pesan asli akan langsung memperbarui atau menghapus versi terjemahannya di saluran lain.
  - **Balasan (Replies)**: Mengutip cuplikan pesan yang dirujuk dalam bahasa target dan menautkan ke pesan terkait (balasan semu).
  - **Pesan yang Diteruskan**: Mempertahankan konteks pesan yang diteruskan dengan header yang dilokalkan.
  - **Reaksi & Pin**: Penambahan/penghapusan reaksi emoji dan penyematan pesan disinkronkan secara dua arah.
  - **Thread & Forum**: Mendukung thread biasa, saluran forum, dan saluran media, termasuk pemetaan tag forum.
  - **Polling (Jajak Pendapat)**: Menerjemahkan pertanyaan dan opsi polling ke dalam format Embed serta memposting hasil akhir saat polling selesai.
- **Fitur «View Original» (Lihat Asli)**
  Klik kanan (atau tekan lama di ponsel) pada pesan terjemahan mana pun, lalu pilih **«Aplikasi» → «View Original»** untuk mendapatkan tautan langsung dan cuplikan teks asli (hanya terlihat oleh Anda).
- **Penanganan Pintar untuk Tautan dan Media**
  - Tautan dan sebutan ke saluran, pesan, atau thread yang dikelola akan otomatis diubah ke ID yang sesuai dalam bahasa target.
  - URL situs web eksternal yang memiliki versi multibahasa `hreflang` akan otomatis diganti dengan versi bahasa target.

---

### Langkah Penyiapan Server

#### 1. Mengundang Bot
Buat tautan undangan di Discord Developer Portal dengan izin berikut:

- **OAuth2 Scopes**: `bot`, `applications.commands`
- **Izin Bot (Bot Permissions)**:
  - **Umum**: `View Channels` (Lihat Saluran), `Read Message History` (Baca Riwayat Pesan)
  - **Teks**: `Send Messages` (Kirim Pesan), `Send Messages in Threads` (Kirim Pesan di Thread)
  - **Moderasi**: `Pin Messages` (Sematkan Pesan)
  - **Webhooks**: `Manage Webhooks` (Kelola Webhook)
  - **Thread**: `Create Public Threads` (Buat Thread Publik), `Manage Threads` (Kelola Thread)
  - **Reaksi**: `Add Reactions` (Tambah Reaksi)
- **Bilangan Bulat Izin (Permissions Integer)**: `2252126768139328`
  - *Catatan: Untuk menyinkronkan reaksi emoji kustom dari server lain, aktifkan `Use External Emojis` (Permissions Integer: `2252126768401472`).*

#### 2. Mengaktifkan Intent Istimewa
Pastikan `MESSAGE CONTENT INTENT` diaktifkan pada tab **Bot** di Discord Developer Portal.

---

### Konfigurasi Saluran (Operasi Dasar)

#### Membuat Grup Penerjemahan
Jalankan `/new-channel` di saluran bahasa Jepang Anda (misalnya `#general-ja`):

```
/new-channel language:ja
```
*Catatan: Jika `group` diabaikan, nama saluran saat ini akan digunakan sebagai nama grup.*

#### Menambahkan Saluran Bahasa Lain
Jalankan `/join-channel` di saluran bahasa Inggris Anda (misalnya `#general-en`):

```
/join-channel group:general language:en
```

Untuk menambahkan saluran bahasa Indonesia (misalnya `#general-id`):

```
/join-channel group:general language:id
```

Sekarang `#general-ja`, `#general-en`, dan `#general-id` saling terhubung dan penerjemahan otomatis aktif.

#### Keluar dari Grup dan Menghapus Grup
- Menghapus saluran dari grup: `/leave-channel group:general`
- Menghapus grup penerjemahan sepenuhnya: `/delete-group group:general`
- Melihat daftar grup dan saluran aktif: `/list-groups`

---

### Referensi Perintah

#### Perintah Slash Administrator
Secara default, perintah administratif hanya dapat dijalankan oleh anggota dengan **izin Administrator**. Untuk memberi akses ke peran lain, atur di: **Pengaturan Server → Integrasi → (Nama Bot) → Kelola → Izin Perintah**.

| Perintah | Deskripsi | Opsi Utama |
|---|---|---|
| `/new-channel` | Membuat grup penerjemahan baru dan mendaftarkan saluran | `language` (wajib): Kode bahasa BCP-47<br>`channel` (opsional): Saluran target (default: saluran saat ini)<br>`group` (opsional): Nama grup (default: nama saluran) |
| `/join-channel` | Menambahkan saluran ke grup penerjemahan yang ada | `group` (wajib): Nama grup target<br>`language` (wajib): Kode bahasa BCP-47<br>`channel` (opsional): Saluran target (default: saluran saat ini) |
| `/leave-channel` | Menghapus saluran dari grup | `group` (wajib): Nama grup<br>`channel` (opsional): Saluran target (default: saluran saat ini) |
| `/delete-group` | Menghapus grup penerjemahan secara menyeluruh | `group` (wajib): Nama grup yang akan dihapus |
| `/list-groups` | Menampilkan semua grup penerjemahan dan saluran tertaut | Tidak ada |
| `/set-style` | Mengatur gaya atau nada terjemahan untuk grup | `group` (wajib): Nama grup<br>`preset` (opsional): Preset gaya (lihat di bawah)<br>`custom` (opsional): Instruksi bahasa alami kustom (maks. 200 karakter) |
| `/add-glossary` | Mendaftarkan terjemahan istilah pilihan dalam glosarium server | `term` (wajib): Istilah sumber<br>`translation` (wajib): Terjemahan pilihan<br>`attribute` (opsional): Kategori istilah (mis. nama orang, slang)<br>`always_include` (opsional): Selalu sertakan dalam prompt tanpa pencocokan kata (default: `false`) |
| `/list-glossary` | Menampilkan daftar glosarium server ini | Tidak ada |
| `/remove-glossary`| Menghapus entri dari glosarium | `term` (wajib): Istilah yang akan dihapus |
| `/edit-forum-tags` | Mengedit pemetaan tag untuk saluran forum/media | `group` (wajib): Nama grup<br>`channel` (opsional): Saluran forum target |
| `/bot-whitelist` | Mengelola daftar izin bot dan webhook otomatis | Subperintah: `add`, `remove`, `list`<br>`source_type`: `bot` atau `webhook`<br>`source_id`: ID pengguna bot atau ID webhook |

#### Perintah Pesan (Dapat Digunakan Semua Anggota)
- **`View Original` (Menu Aplikasi)**
  Klik kanan atau tekan lama pada pesan → **«Aplikasi» → «View Original»** untuk mendapatkan tautan langsung dan cuplikan teks asli.

---

### Kustomisasi Lanjutan

#### 1. Gaya Penerjemahan (`/set-style`)
Sesuaikan nada terjemahan dengan suasana komunitas server Anda (`preset` dan `custom` saling eksklusif):

| Preset | Deskripsi & Penggunaan |
|---|---|
| `default` | Gaya percakapan alami yang biasa digunakan penutur asli dalam obrolan |
| `casual` | Nada santai dan ramah yang cocok untuk teman dan komunitas |
| `gaming` | Slang gamer dan gaya komunitas game |
| `friendly` | Nada hangat, sopan, dan bersahabat |
| `business` | Nada ringkas, profesional, dan formal |
| `formal` | Gaya bahasa formal dengan kata-kata sopan dan santun |
| `netslang` | Slang internet dan gaya forum |
| `tweet` | Kalimat pendek dan padat seperti di media sosial (X / Twitter) |
| `literal` | Terjemahan harfiah jika terdapat beberapa interpretasi |

#### 2. Glosarium Server (`/add-glossary`)
Tetapkan terjemahan untuk nama karakter, istilah game, atau jargon komunitas server Anda (hingga 50 entri per server):
- **Atribut (`attribute`)**: Kategori seperti "nama orang", "tempat", "slang", "singkatan", atau "istilah teknis" membantu model memahami konteks secara tepat.
- **Selalu Sertakan (`always_include`)**: Jika disetel ke `true`, istilah akan selalu dikirim sebagai konteks meskipun kata tersebut tidak muncul secara langsung dalam pesan.

#### 3. Pemetaan Tag Forum (`/edit-forum-tags`)
Saat menautkan saluran forum, Anda dapat memetakan tag antar bahasa. Saat sebuah postingan dibuat dengan tag di satu bahasa, postingan cerminannya otomatis menerima tag yang sesuai.

#### 4. Daftar Izin Pesan Otomatis (`/bot-whitelist`)
Secara default, pesan dari bot dan webhook diabaikan untuk mencegah loop tak terbatas. Gunakan `/bot-whitelist add` untuk mengizinkan bot pengumuman, feed RSS, atau notifikasi otomatis agar tetap diterjemahkan.

---

## 2. Panduan Pengembang & Self-Hosting

### Persyaratan Sistem & Tech Stack

- **Bahasa**: Go 1.24 atau lebih baru
- **Basis Data**: SQLite (Driver Go murni via `modernc.org/sqlite`, tanpa CGO)
- **Library Discord**: `github.com/bwmarrin/discordgo`
- **Mesin Penerjemahan**: API Chat Completions yang kompatibel dengan OpenAI (OpenAI, OpenRouter, Azure OpenAI, LLM lokal, dll.)
- **Cross-Compilation**: Didukung penuh dengan `CGO_ENABLED=0` untuk menghasilkan biner mandiri pada Linux, Windows, dan macOS.

---

### Penyiapan & Menjalankan Bot

#### 1. Membuat Bot Discord
1. Buka [Discord Developer Portal](https://discord.com/developers/applications) dan buat Aplikasi baru.
2. Di tab **Bot**, aktifkan `MESSAGE CONTENT INTENT` dan salin Bot Token.
3. Di **OAuth2 → URL Generator**, pilih cakupan `bot` dan `applications.commands` beserta izin yang diperlukan, lalu undang bot ke server Anda.

#### 2. Menyiapkan API yang Kompatibel dengan OpenAI
Dapatkan URL endpoint API, kunci API, dan ID model dari penyedia LLM pilihan Anda.

#### 3. Mengonfigurasi Variabel Lingkungan
Salin `.env.example` ke `.env` dan sesuaikan nilainya:

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

#### 4. Membangun & Menjalankan

Menjalankan langsung secara lokal:
```sh
go run ./cmd/discord-auto-translator
```

Membangun biner mandiri dan menjalankannya:
```sh
go build -o discord-auto-translator ./cmd/discord-auto-translator
./discord-auto-translator
```

**Validasi Model Pra-Peluncuran (`--model-prewarm`)**:
Memvalidasi kredensial API, koneksi model, dan skema respons sebelum deployment:
```sh
./discord-auto-translator --model-prewarm
```

---

### Referensi Variabel Lingkungan

| Variabel | Wajib | Default | Deskripsi |
|---|---|---|---|
| `DISCORD_TOKEN` | **Ya** | - | Token autentikasi bot Discord |
| `OPENAI_BASE_URL` | **Ya** | - | URL dasar API Chat Completions yang kompatibel (mis. `https://api.openai.com/v1`) |
| `OPENAI_API_KEY` | **Ya** | - | Kunci API Bearer |
| `OPENAI_MODEL` | **Ya** | - | ID model pada penyedia LLM |
| `OPENAI_REASONING_EFFORT` | Tidak | (tidak disetel) | Parameter `reasoning_effort`. Setel ke `none` untuk mematikan token pemikiran pada model hybrid |
| `DB_PATH` | Tidak | `./translator.db` | Jalur file basis data SQLite |
| `HTTP_ADDR` | Tidak | `:8080` | Alamat listen untuk server HTTP lencana avatar |
| `PUBLIC_BASE_URL` | Tidak | (tidak disetel) | URL publik untuk lencana avatar. Menambahkan cincin warna peran tertinggi di sekitar avatar |
| `TRANSLATION_RATE_LIMIT_TOKENS_PER_MIN` | Tidak | `100000` | Batas token per menit per server (guild) |
| `AVATAR_RATE_LIMIT_REQUESTS_PER_MIN` | Tidak | `120` | Batas permintaan per menit per IP untuk endpoint `/avatar` |
| `MESSAGE_LINK_RETENTION_DAYS` | Tidak | `0` | Masa retensi tautan pesan dalam hari. `0` = tanpa batas waktu |
| `GUILD_DATA_RETENTION_DAYS` | Tidak | `0` | Masa retensi data server setelah bot dikeluarkan |

---

### Arsitektur & Prinsip Desain

#### 1. Alur Penerjemahan (Pipeline)
1. **Perakitan Konteks**: Mengumpulkan topik saluran, konteks percakapan terkini, referensi balasan, metadata OGP dari tautan, dan gambar yang telah diubah ukurannya.
2. **Masking Placeholder**: Mengganti sebutan (`<@id>`), emoji (`<:name:id>`), saluran (`<#id>`), URL, dan blok kode dengan token (`[USER:name]`, `[EMOJI:name]`, `[SITE:N]`, `[CODE]`) untuk mencegah injeksi prompt.
3. **Penyusunan Prompt & Caching**: Menyusun prompt dalam struktur stabil dan dinamis untuk memanfaatkan Prefix Prompt Caching dari penyedia AI.
4. **Structured Outputs**: Menggunakan `response_format.type=json_schema` (`strict: true`) untuk menghasilkan semua bahasa target dalam satu respons JSON terstruktur.
5. **Pasca-pemrosesan & Pengiriman**: Memulihkan placeholder, menulis ulang tautan Discord internal, mengganti URL `hreflang`, dan mengirimkan pesan Webhook secara paralel.

#### 2. Keamanan & Prinsip Fail-Closed
- **Pertahanan Prompt Injection**: Semua input pengguna di-escape dalam XML dan diisolasi dalam tag khusus.
- **Prinsip Fail-Closed**: Jika batas token terlampaui (`finish_reason=length`), JSON tidak valid, atau terjadi gangguan jaringan, bot membatalkan pengiriman dan mengirimkan notifikasi kesalahan di saluran asal.

#### 3. Keandalan & Konsistensi Data
- **Idempotensi**: `message_links` dan `processed_events` mencegah duplikasi pesan saat menerima event gateway berulang.
- **Transaksi Kompensasi**: Jika penyimpanan basis data gagal setelah pengiriman Webhook, pesan Discord yang terkirim akan langsung dihapus.
- **Sinkronisasi Dua Arah**: Reaksi dan pesan yang disematkan disinkronkan ke seluruh grup, di mana pun tindakan tersebut dilakukan.

---

### Pengembangan & Pengujian

#### Menjalankan Pengujian
```sh
go test ./...
```

#### Katalog UI Multibahasa (i18n)
Semua teks antarmuka dan pesan kesalahan dikelola secara terpusat di `internal/translatorbot/ui_strings.go` untuk 13 bahasa. Penambahan teks baru harus mencakup semua bahasa yang didukung dan divalidasi oleh `TestUIStringCatalogIsComplete`.

---

## 3. Lisensi

Proyek ini dilisensikan di bawah [MIT License](LICENSE).
