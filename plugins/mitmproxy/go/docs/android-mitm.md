# Android MITM — ringkas

Dokumen penuh: **[USAGE.md §11](USAGE.md#11-android-apk-mitm)**.

## Minimal path

```text
HP/Emulator  --proxy-->  PC:8080 (proxying)  -->  Internet
                 ^
            trust ca.pem
```

```bash
# PC (ganti IP di HP)
./bin/proxying -proxy-addr 0.0.0.0:8080 -api-addr 127.0.0.1:9090
curl -o ca.pem http://127.0.0.1:9090/api/ca.pem
adb push ca.pem /sdcard/Download/
```

| Device | Proxy host |
|--------|------------|
| Emulator | `10.0.2.2:8080` |
| HP fisik | `IP_LAN_PC:8080` |

1. Install **CA certificate** (user) dari `ca.pem`  
2. Wi‑Fi → Manual proxy  
3. Buka UI PC → Capture  

## App HTTPS gagal?

| Penyebab | Tindakan |
|----------|----------|
| Hanya trust system CA (API 24+) | Repack APK + `networkSecurityConfig` `user` certificates |
| Certificate pinning | Frida/objection/patch — di luar proxying |
| Cleartext diblokir | NSC `cleartextTrafficPermitted` |

Jangan expose `-api-addr 0.0.0.0` ke Wi‑Fi publik.
