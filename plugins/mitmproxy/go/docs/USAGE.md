# Proxying — Panduan Penggunaan Lengkap

**Proxying** = MITM HTTP(S) lokal + Web UI + REST client (Compose) + MCP server.  
Tanpa cloud, tanpa akun, tanpa lisensi online. Data default: `~/.proxying`.

| Port default | Fungsi |
|--------------|--------|
| `127.0.0.1:8080` | Proxy MITM (HTTP + HTTPS CONNECT) |
| `127.0.0.1:9090` | Web UI + Control API + MCP HTTP |

---

## Daftar isi

1. [Install & build](#1-install--build)
2. [Menjalankan](#2-menjalankan)
3. [Web UI](#3-web-ui)
4. [Capture desktop (browser / curl)](#4-capture-desktop)
5. [Sertifikat CA (HTTPS)](#5-sertifikat-ca-https)
6. [Rules (block / rewrite / map-local)](#6-rules)
7. [Scripts JS](#7-scripts-js)
8. [Breakpoints](#8-breakpoints)
9. [Compose (REST ala Postman)](#9-compose-rest)
10. [MCP (OpenCode / Cursor / Claude)](#10-mcp)
11. [Android APK MITM](#11-android-apk-mitm) ← detail
12. [API ringkas](#12-api-ringkas)
13. [Docker](#13-docker)
14. [Keamanan & batasan](#14-keamanan--batasan)
15. [Troubleshooting](#15-troubleshooting)

---

## 1. Install & build

### Prasyarat

- Go **1.22+** (proyek ini dibuild dengan Go 1.25)
- Node.js **20+** (hanya jika rebuild Web UI)
- Linux / macOS / Windows (Linux diutamakan)

### Build

```bash
cd /path/to/proxying

# Binary saja (UI yang sudah di-embed)
go build -o bin/proxying ./cmd/proxying

# Full release: rebuild SPA + binary + version
make release
```

Binary: `./bin/proxying`

---

## 2. Menjalankan

```bash
./bin/proxying
# sama dengan:
./bin/proxying \
  -proxy-addr 127.0.0.1:8080 \
  -api-addr 127.0.0.1:9090 \
  -data-dir ~/.proxying
```

### Flag penting

| Flag | Default | Keterangan |
|------|---------|------------|
| `-proxy-addr` | `127.0.0.1:8080` | Listen proxy |
| `-api-addr` | `127.0.0.1:9090` | UI + API |
| `-data-dir` | `~/.proxying` | CA, rules, scripts, collections, history |
| `-max-flows` | `5000` | Ring buffer in-memory |
| `-max-body` | `1 MiB` | Cap body per message |
| `-bp-max` / `-bp-timeout` | `32` / `60s` | Breakpoint |
| `-script-timeout` | `100ms` | Timeout script goja |
| `-mcp-stdio` | off | Mode MCP stdin/stdout (untuk agent) |
| `-turbo` | off | Capture lebih hemat (body/flow cap lebih kecil) |
| `-report-token` | kosong | Token opsional untuk POST report agent |
| `-version` | | Cetak versi |

Buka browser: **http://127.0.0.1:9090**  
Hard refresh jika UI cache: `Ctrl+Shift+R`.

---

## 3. Web UI

Tema dark tooling (Capture / Rules / Compose / Explorer / Scripts).

| Panel | Fungsi |
|-------|--------|
| **Capture** | List traffic live (SSE), filter, detail headers/body, pause/clear, HAR, unduh CA |
| **Rules** | CRUD rule host block/allow, rewrite, map-local, dll. |
| **Compose** | REST client ala Postman: method, URL, params, headers, body, auth, env, collections |
| **Explorer** | Agregasi host dari capture → filter cepat |
| **Scripts** | Gallery template JS + Apply ke `data-dir/scripts/` |

### Alur tipikal

1. Start `./bin/proxying`
2. Unduh CA dari tombol **CA** di header
3. Set proxy client ke `127.0.0.1:8080`
4. Traffic muncul di **Capture** (badge **LIVE**)
5. Klik baris → inspector (Overview / Headers / Request / Response)
6. **From flow** di Compose untuk replay sebagai REST

---

## 4. Capture desktop

### curl

```bash
export http_proxy=http://127.0.0.1:8080
export https_proxy=http://127.0.0.1:8080
export no_proxy=localhost,127.0.0.1

# HTTP
curl -s http://example.com/

# HTTPS — trust CA lokal; HTTP/1.1 lebih stabil dengan MITM saat ini
curl -s --http1.1 --cacert ~/.proxying/ca.pem https://example.com/
```

### Browser (Firefox contoh)

1. Settings → Network Settings → Manual proxy  
   - HTTP Proxy: `127.0.0.1` port `8080`  
   - Also use for HTTPS  
2. Import `ca.pem` sebagai authority (lihat [§5](#5-sertifikat-ca-https))
3. Buka situs → traffic di UI

### System env (satu terminal)

```bash
export http_proxy=http://127.0.0.1:8080 https_proxy=http://127.0.0.1:8080
# jalankan app CLI dari terminal ini
```

---

## 5. Sertifikat CA (HTTPS)

MITM HTTPS membutuhkan **root CA lokal** yang dipercaya client.

### Unduh

```bash
curl -o proxying-ca.pem http://127.0.0.1:9090/api/ca.pem
# atau tombol CA di UI
# file juga ada di: ~/.proxying/ca.pem
```

Private key: `~/.proxying/ca.key` (mode `0600`) — **jangan dibagikan**.

### Linux (system)

```bash
sudo cp proxying-ca.pem /usr/local/share/ca-certificates/proxying-ca.crt
sudo update-ca-certificates
```

### Linux (Firefox / Chrome user NSS)

```bash
certutil -d sql:$HOME/.pki/nssdb -A -t "C,," -n proxying -i proxying-ca.pem
```

### macOS

```bash
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain proxying-ca.pem
```

### Windows

```powershell
certutil -addstore -f Root proxying-ca.pem
```

### Android

Lihat [§11](#11-android-apk-mitm) — user CA vs system CA (apk yang di-repack).

---

## 6. Rules

Disimpan di `~/.proxying/rules.json`. UI **Rules** atau API `/api/rules`.

### Tipe utama

| `type` | Efek |
|--------|------|
| `host_block` | Client dapat **403** |
| `host_allow` | Jika ada allow-list, host di luar list → 403 |
| `header` | Set/remove header (phase request/response) |
| `body_replace` | Ganti string body |
| `map_local` | Sajikan file dari `data-dir/map-local/` (path jail) |
| `ssl_bypass` | CONNECT **tanpa** MITM (tunnel) |
| `access_control` | Izinkan/tolak client IP (CIDR) |
| `gateway` | Rewrite upstream host |
| `mirror_remote` | Response dari URL lain |
| `secondary_proxy` | Upstream proxy (`http://` / `socks5://`) |
| `reverse_proxy` | Listen loopback → target |
| `highlight` | Label/warna di list |

### Contoh: block host

```bash
curl -s -X POST http://127.0.0.1:9090/api/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "enabled": true,
    "type": "host_block",
    "match": { "host": "ads.example.com" }
  }'
```

### Contoh: map local

```bash
mkdir -p ~/.proxying/map-local
echo '{"ok":true}' > ~/.proxying/map-local/api.json

curl -s -X POST http://127.0.0.1:9090/api/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "enabled": true,
    "type": "map_local",
    "match": { "host": "api.local.test", "path": "^/v1" },
    "map_local": { "status": 200, "file": "api.json" }
  }'
```

---

## 7. Scripts JS

Folder: `~/.proxying/scripts/*.js`  
Atau **Scripts** di UI → Apply template.

```js
function onRequest(flow) {
  flow.request.headers["X-Debug"] = "1";
}
function onResponse(flow) {
  flow.response.headers["X-Out"] = "1";
}
```

- Runtime: **goja** (bukan Node)
- Timeout default **100ms**
- Hanya field allowlist: request/response method, url, headers, body, status
- **Tidak** ada `fs`, `net`, `require`

---

## 8. Breakpoints

Pause request yang cocok, edit, resume.

```bash
# aktifkan match
curl -s -X PUT http://127.0.0.1:9090/api/breakpoints \
  -H 'Content-Type: application/json' \
  -d '{"enabled":true,"match":{"host":"api.example.com"}}'

# daftar hold
curl -s http://127.0.0.1:9090/api/breakpoints

# resume (+ optional edit header)
curl -s -X POST http://127.0.0.1:9090/api/breakpoints/{id}/resume \
  -H 'Content-Type: application/json' \
  -d '{"headers":{"X-Edited":"1"}}'
```

Batas: max concurrent (`-bp-max`), timeout (`-bp-timeout`). H2 multiplex / frame tunnel tidak di-pause per-stream.

---

## 9. Compose (REST)

Panel **Compose** di UI:

- Method + URL + **Send**
- Params / Headers / Body / Auth (Bearer, Basic) / Env (`{{var}}`)
- Collections (buat, simpan request, load)
- **From flow** — isi form dari capture
- Response: status, timing, headers, body

CLI:

```bash
curl -s -X POST http://127.0.0.1:9090/api/rest/send \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","url":"https://example.com/"}'

curl -s -X POST http://127.0.0.1:9090/api/rest/from-flow/{id}
```

Env:

```bash
curl -s -X PUT http://127.0.0.1:9090/api/rest/env \
  -H 'Content-Type: application/json' \
  -d '{"env":{"base":"https://api.example.com"}}'
# URL: {{base}}/v1/users
```

---

## 10. MCP

Agent (OpenCode, Cursor, Claude Desktop) bisa memanggil tools proxying.

### Tools

| Tool | Fungsi |
|------|--------|
| `list_flows` | List capture → `{flows, count}` |
| `get_flow` | Detail by id |
| `clear_flows` | Clear buffer |
| `set_capture_paused` | Pause/resume |
| `rest_send` | HTTP client |
| `rest_from_flow` | REST dari capture |
| `list_rules` | List rules |
| `add_host_block_rule` | Block host |

### OpenCode (`~/.config/opencode/opencode.json`)

```json
{
  "mcp": {
    "proxying": {
      "type": "local",
      "command": [
        "/absolute/path/to/proxying/bin/proxying",
        "-mcp-stdio",
        "-data-dir",
        "/home/YOU/.proxying"
      ],
      "enabled": true,
      "timeout": 45000,
      "cwd": "/absolute/path/to/proxying"
    }
  }
}
```

Restart OpenCode setelah ubah config.  
**Data-dir harus sama** dengan proses UI agar `list_flows` melihat traffic live.

### Cursor / Claude Desktop

```json
{
  "mcpServers": {
    "proxying": {
      "command": "/absolute/path/to/bin/proxying",
      "args": ["-mcp-stdio", "-data-dir", "/home/YOU/.proxying"]
    }
  }
}
```

### HTTP MCP (opsional)

Saat UI jalan: `http://127.0.0.1:9090/mcp` (Streamable HTTP).  
Legacy: `GET /api/mcp/tools`, `POST /api/mcp/call`.

---

## 11. Android APK MITM

Tujuan: melihat HTTP(S) app Android lewat proxying di PC.

### Arsitektur

```
[APK di HP/Emulator] --Wi‑Fi/proxy--> [PC:8080 MITM] --internet--> [API server]
                         ^
                         |
              CA proxying dipercaya di device
```

### 11.1 Persiapan PC

1. PC dan HP **satu Wi‑Fi** (atau emulator di host yang sama).
2. Jalankan proxy **bind ke LAN** (bukan hanya 127.0.0.1):

```bash
# Cari IP LAN PC, mis. 192.168.1.10
ip -4 addr show | grep inet

./bin/proxying \
  -proxy-addr 0.0.0.0:8080 \
  -api-addr 127.0.0.1:9090 \
  -data-dir ~/.proxying
```

- Proxy: HP pakai `192.168.1.10:8080`
- UI tetap di PC: `http://127.0.0.1:9090` (lebih aman tidak expose API ke LAN)

**Peringatan keamanan:** `0.0.0.0:8080` bisa dipakai orang di Wi‑Fi yang sama. Pakai jaringan trusted / firewall. Pertimbangkan rule `access_control` (CIDR HP saja).

3. Unduh CA:

```bash
curl -o proxying-ca.pem http://127.0.0.1:9090/api/ca.pem
# salin ke HP (USB, adb, HTTP sementara, QR, dll.)
adb push proxying-ca.pem /sdcard/Download/
```

### 11.2 Emulator Android (paling mudah)

```bash
# IP host dari emulator biasanya 10.0.2.2
adb shell settings put global http_proxy 10.0.2.2:8080
```

Install CA user (Settings → Security → Encryption & credentials → Install a certificate → CA certificate) dari file `proxying-ca.pem`.

Proxying di host:

```bash
./bin/proxying -proxy-addr 0.0.0.0:8080 -api-addr 127.0.0.1:9090
```

Buka browser/app di emulator → traffic di UI.

Matikan proxy emulator:

```bash
adb shell settings put global http_proxy :0
```

### 11.3 HP fisik — proxy Wi‑Fi

1. Settings → Wi‑Fi → jaringan terhubung → Pencil / Advanced  
2. Proxy: **Manual**  
   - Host: IP PC (`192.168.x.x`)  
   - Port: `8080`  
3. Install CA (lihat 11.4)  
4. Tes browser dulu (Chrome) ke `http://example.com` lalu HTTPS

### 11.4 Install CA di Android

#### A) User certificate (Android 7+)

Cukup untuk:

- Browser sistem / Chrome (banyak kasus)
- App yang **mempercayai user CA**

Langkah umum:

1. Salin `proxying-ca.pem` ke Download  
2. **Settings → Security → Encryption & credentials → Install a certificate → CA certificate**  
   (path menu berbeda per OEM: Samsung/Xiaomi/Pixel)  
3. Nama: `proxying`  
4. Pin / pola mungkin diminta  

Cek: **Trusted credentials → User** harus ada “proxying”.

**Batasan:** App debug modern sering set `android:networkSecurityConfig` / hanya trust **system** CA → HTTPS app **gagal** (pinning / cleartext policy) meski proxy + user CA sudah benar.

#### B) System CA (butuh root **atau** repack APK)

Dua pendekatan umum:

**B1. Root + pasang ke system store** (advanced, risiko brick/security):

- Convert PEM → format sistem (hash subject + `cacerts`)
- Remount `/system` read-write, salin ke `/system/etc/security/cacerts/`
- Reboot  

Hanya untuk device lab/root. Tidak didokumentasikan step-by-step OEM-specific di sini.

**B2. Repack APK agar trust user CA (direkomendasikan untuk debug non-root)**

Konsep: ubah Network Security Config supaya debug build mempercayai user certificates.

1. Decode APK:

```bash
apktool d app.apk -o app_src
```

2. Buat/edit `res/xml/network_security_config.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<network-security-config>
  <base-config cleartextTrafficPermitted="true">
    <trust-anchors>
      <certificates src="system" />
      <certificates src="user" />
    </trust-anchors>
  </base-config>
  <!-- opsional: debug-overrides saja -->
  <debug-overrides>
    <trust-anchors>
      <certificates src="user" />
    </trust-anchors>
  </debug-overrides>
</network-security-config>
```

3. Di `AndroidManifest.xml` pada `<application ...>`:

```xml
android:networkSecurityConfig="@xml/network_security_config"
```

4. Build & sign ulang:

```bash
apktool b app_src -o app-patched.apk
# zipalign + apksigner / jarsigner dengan keystore debug
adb install -r app-patched.apk
```

5. Device: user CA `proxying` terpasang + Wi‑Fi proxy ke PC.

**Certificate pinning** (OkHttp `CertificatePinner`, custom TrustManager, Play Integrity, dll.): network security config **tidak cukup**. Perlu:

- Frida / objection bypass pinning, atau  
- Patch library native/Java yang pin, atau  
- Build debug resmi dari developer app  

Itu di luar cakupan proxying; proxy hanya MITM setelah TLS “terbuka”.

### 11.5 Checklist Android

| Langkah | OK? |
|---------|-----|
| PC & HP satu jaringan | ☐ |
| `proxying -proxy-addr 0.0.0.0:8080` | ☐ |
| Firewall PC izinkan TCP 8080 dari LAN | ☐ |
| Wi‑Fi proxy HP → IP_PC:8080 | ☐ |
| CA terpasang (User) | ☐ |
| Browser HTTP terlihat di Capture | ☐ |
| Browser HTTPS terlihat | ☐ |
| App HTTP terlihat | ☐ |
| App HTTPS: jika gagal → networkSecurityConfig / pinning | ☐ |

### 11.6 App hanya HTTP cleartext

Beberapa app lama hanya HTTP: cukup proxy, tanpa CA.  
Android 9+ mungkin block cleartext kecuali di NSC `cleartextTrafficPermitted="true"`.

### 11.7 Alternatif: report agent (tanpa system proxy)

Jika app bisa diinstrumentasi, POST capture ke PC:

```http
POST http://IP_PC:9090/api/report
Authorization: Bearer <report-token>   # jika -report-token di-set
Content-Type: application/json
```

(Lihat API report / agents di UI legacy.) Untuk APK stock, system/Wi‑Fi proxy + CA tetap cara utama.

### 11.8 Matikan proxy di HP

- Wi‑Fi → Proxy **None**  
- Emulator: `adb shell settings put global http_proxy :0`  
- Cabut CA user jika tidak dipakai lagi (Security → Trusted credentials → User → remove)

---

## 12. API ringkas

Base: `http://127.0.0.1:9090`

| Method | Path | Ket |
|--------|------|-----|
| GET | `/api/health` | Health |
| GET | `/api/flows` | List (`?host&method&status&q`) |
| GET | `/api/flows/{id}` | Detail |
| DELETE | `/api/flows` | Clear |
| GET | `/api/events` | SSE live |
| GET/PUT | `/api/capture` | `{paused}` |
| GET | `/api/export/har` | HAR export |
| GET | `/api/ca.pem` | Root CA |
| * | `/api/rules` | CRUD rules |
| * | `/api/rest/*` | send, env, collections, from-flow |
| * | `/api/breakpoints` | breakpoint control |
| GET | `/api/mcp/tools` | Legacy MCP list |
| * | `/mcp` | MCP Streamable HTTP |

---

## 13. Docker

```bash
cd deploy/compose
docker compose up -d --build
```

Lihat [deploy/compose/README.md](../deploy/compose/README.md).  
Publish default loopback; sesuaikan ports untuk LAN Android (hati-hati security).

---

## 14. Keamanan & batasan

- Default **loopback only** — bagus untuk desktop; Android butuh bind LAN sadar risiko.
- Siapa pun yang reach API bisa baca traffic & ubah rules.
- Bukan pengganti Burp/Reqable enterprise; H3 deferred; pinning app perlu teknik terpisah.
- Hanya untuk **debug milik sendiri / lab** — patuhi hukum & ToS app.

---

## 15. Troubleshooting

| Gejala | Cek |
|--------|-----|
| UI putih / lama | Hard refresh; pastikan binary di-rebuild setelah `make build` / copy `web/dist` → embed |
| HTTPS gagal di desktop | CA terpasang? `curl --cacert ~/.proxying/ca.pem` |
| HTTPS gagal di Android app | User CA vs system; NSC; **pinning** |
| Tidak ada traffic dari HP | IP benar? firewall? proxy Wi‑Fi? `0.0.0.0:8080`? |
| MCP `list_flows` kosong | UI & MCP **data-dir sama**? Capture sedang jalan? |
| MCP schema / timeout | Binary terbaru; restart OpenCode; jangan httpbin jika 503 |
| curl HTTPS aneh | Coba `--http1.1` |
| Port in use | Ganti `-proxy-addr` / `-api-addr` |

### Log

```bash
./bin/proxying 2>&1 | tee proxying.log
```

Mode MCP stdio **tidak** spam log ke stderr (sengaja, agar OpenCode tidak error).

---

## Referensi cepat perintah

```bash
# start
./bin/proxying -data-dir ~/.proxying

# desktop capture
export http_proxy=http://127.0.0.1:8080 https_proxy=http://127.0.0.1:8080
curl --http1.1 --cacert ~/.proxying/ca.pem https://example.com/

# Android lab (PC)
./bin/proxying -proxy-addr 0.0.0.0:8080 -api-addr 127.0.0.1:9090

# MCP OpenCode
# command: .../bin/proxying -mcp-stdio -data-dir ~/.proxying
```

---

**Lisensi:** MIT — lihat [LICENSE](../LICENSE).  
**Proyek:** personal local-first; bukan afiliasi Reqable/Postman.
