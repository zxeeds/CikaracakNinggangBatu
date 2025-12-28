# ZiVPN UDP Tunnel

## 🌟 Fitur Utama

- **Minimalist CLI**: Installer dengan tampilan modern, bersih, dan elegan.
- **Headless Management**: Manajemen user sepenuhnya via API atau Bot.
- **Robust User Management**:
  - **Auto-Revoke**: User expired otomatis disconnect (via Cron).
  - **Clean Deletion**: Hapus user bersih total dari config dan database.
- **Dynamic Security**: API Key dan sertifikat SSL digenerate otomatis.
- **High Performance**: Core UDP ZiVPN yang dioptimalkan.

---

## 📥 Instalasi

Jalankan perintah berikut di terminal VPS Anda (sebagai root):

```bash
wget -q https://raw.githubusercontent.com/zxeeds/CikaracakNinggangBatu/main/install.sh && chmod +x install.sh && ./install.sh
```

### Konfigurasi Saat Instalasi

Saat script berjalan, Anda akan diminta memasukkan:

1.  **Domain**: Wajib diisi untuk generate sertifikat SSL (contoh: `vpn.domain.com`).
2.  **API Key**: Tekan **Enter** untuk auto-generate.

---

## 🗑️ Uninstall

Untuk menghapus ZiVPN, API, Bot, dan semua konfigurasi:

```bash
wget -q https://raw.githubusercontent.com/zxeeds/CikaracakNinggangBatu/main/uninstall.sh && chmod +x uninstall.sh && ./uninstall.sh
```
---

## 🔌 API Documentation

API berjalan di port `8080`. Gunakan **API Key** pada header `X-API-Key`.

**Base URL**: `http://<IP-VPS>:8080`
**Header**: `X-API-Key: <YOUR-API-KEY>`

### 1. Create User

- **Endpoint**: `/api/user/create`
- **Method**: `POST`
- **Body**: `{ "password": "user1", "days": 30 }`

### 2. Create Trial

- **Endpoint**: `/api/user/trial`
- **Method**: `POST`
- **Body**: `{ "password": "user1", "minutes": 30 }`

### 3. Delete User

- **Endpoint**: `/api/user/delete`
- **Method**: `POST`
- **Body**: `{ "password": "user1" }`

### 4. Renew User

- **Endpoint**: `/api/user/renew`
- **Method**: `POST`
- **Body**: `{ "password": "user1", "days": 30 }`

### 5. List Users

- **Endpoint**: `/api/users`
- **Method**: `GET`

### 6. System Info

- **Endpoint**: `/api/info`
- **Method**: `GET`

### 7. Cron Trigger (Expire Check)

- **Endpoint**: `/api/cron/expire`
- **Method**: `POST`
- **Desc**: Trigger manual pengecekan expired (biasanya jalan otomatis jam 00:00 WIB).

### 8. Cron Backup (Automatic Backup)

- **Endpoint**: `/api/backup/telegram`
- **Method**: `POST`
- **Desc**: Trigger manual backup telegram (biasanya jalan otomatis jam 00:00 WIB).

---
