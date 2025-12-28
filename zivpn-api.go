package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "os"
    "os/exec"
    "strings"
    "sync"
    "time"
)

const (
    ConfigFile     = "/etc/zivpn/config.json"
    UserDB         = "/etc/zivpn/users.json"
    DomainFile     = "/etc/zivpn/domain"
    ApiKeyFile     = "/etc/zivpn/apikey"
    PortFile       = "/etc/zivpn/api_port"
    DateTimeFormat = "2006-01-02 15:04:05" // Format Tanggal & Jam Lengkap
	BackupConfFile = "/etc/zivpn/bot.conf"
)

var AuthToken = "ZxeedS-agskjgdvsbdreiWG1234512SDKrqw"

type Config struct {
    Listen string `json:"listen"`
    Cert   string `json:"cert"`
    Key    string `json:"key"`
    Obfs   string `json:"obfs"`
    Auth   struct {
        Mode   string   `json:"mode"`
        Config []string `json:"config"`
    } `json:"auth"`
}

type UserRequest struct {
    Password string `json:"password"`
    Days     int    `json:"days"`
    Minutes  int    `json:"minutes"`
}

type UserStore struct {
    Password  string `json:"password"`
    Expired   string `json:"expired"`   // Format: YYYY-MM-DD HH:MM:SS
    CreatedAt string `json:"created_at"` // Waktu pembuatan
    Status    string `json:"status"`
    Type      string `json:"type"` // "regular" atau "trial"
}

type Response struct {
    Success bool        `json:"success"`
    Message string      `json:"message"`
    Data    interface{} `json:"data,omitempty"`
}

var mutex = &sync.Mutex{}

func main() {
    port := flag.Int("port", 8080, "Port to run the API server on")
    flag.Parse()

    if keyBytes, err := ioutil.ReadFile(ApiKeyFile); err == nil {
        AuthToken = strings.TrimSpace(string(keyBytes))
    }

    http.HandleFunc("/api/user/create", authMiddleware(createUser))
    http.HandleFunc("/api/user/trial", authMiddleware(createTrialUser))
    http.HandleFunc("/api/user/delete", authMiddleware(deleteUser))
    http.HandleFunc("/api/user/renew", authMiddleware(renewUser))
    http.HandleFunc("/api/users", authMiddleware(listUsers))
    http.HandleFunc("/api/info", authMiddleware(getSystemInfo))
	http.HandleFunc("/api/backup/telegram", authMiddleware(backupToTelegram))
    
    // HANYA SATU ENDPOINT EXPIRE
    http.HandleFunc("/api/cron/expire", authMiddleware(checkExpiration))

    log.Printf("Server started at :%d", *port)
    log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        token := r.Header.Get("X-API-Key")
        if token != AuthToken {
            jsonResponse(w, http.StatusUnauthorized, false, "Unauthorized", nil)
            return
        }
        next(w, r)
    }
}

func jsonResponse(w http.ResponseWriter, status int, success bool, message string, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(Response{
        Success: success,
        Message: message,
        Data:    data,
    })
}

// Helper: Support format lama (Date Only) dan baru (DateTime)
func parseTime(dateStr string) time.Time {
    // Paksa Go membaca jam sesuai Local Timezone server (WIB)
    loc := time.Local

    // Coba format lengkap (YYYY-MM-DD HH:MM:SS)
    if t, err := time.ParseInLocation(DateTimeFormat, dateStr, loc); err == nil {
        return t
    }
    
    // Fallback format lama (YYYY-MM-DD) -> Set jam ke 00:00:00 sesuai Local Timezone
    if t, err := time.ParseInLocation("2006-01-02", dateStr, loc); err == nil {
        return t
    }
    
    return time.Time{}
}

// --- CREATE USER REGULAR ---
func createUser(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        jsonResponse(w, http.StatusMethodNotAllowed, false, "Method not allowed", nil)
        return
    }

    var req UserRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        jsonResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
        return
    }

    if req.Password == "" || req.Days <= 0 {
        jsonResponse(w, http.StatusBadRequest, false, "Password dan days harus valid", nil)
        return
    }

    mutex.Lock()
    defer mutex.Unlock()

    config, err := loadConfig()
    if err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal membaca config", nil)
        return
    }

    for _, p := range config.Auth.Config {
        if p == req.Password {
            jsonResponse(w, http.StatusConflict, false, "User sudah ada", nil)
            return
        }
    }

    config.Auth.Config = append(config.Auth.Config, req.Password)
    if err := saveConfig(config); err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal menyimpan config", nil)
        return
    }

    now := time.Now()
    // Simpan expired sebagai datetime (tapi user biasa biasanya set ke tengah malam atau jam sekarang + hari)
    // Di sini kita gunakan jam sekarang agar presisi dengan logika baru
    expDate := now.Add(time.Duration(req.Days) * 24 * time.Hour).Format(DateTimeFormat)
    createdAt := now.Format(DateTimeFormat)

    users, err := loadUsers()
    if err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal membaca database user", nil)
        return
    }

    newUser := UserStore{
        Password:  req.Password,
        Expired:   expDate,
        CreatedAt: createdAt,
        Status:    "active",
        Type:      "regular",
    }
    users = append(users, newUser)

    if err := saveUsers(users); err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal menyimpan database user", nil)
        return
    }

    if err := restartService(); err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal merestart service", nil)
        return
    }

    domain := "Tidak diatur"
    if domainBytes, err := ioutil.ReadFile(DomainFile); err == nil {
        domain = strings.TrimSpace(string(domainBytes))
    }

    jsonResponse(w, http.StatusOK, true, "User berhasil dibuat", map[string]string{
        "password":   req.Password,
        "expired":    expDate,
        "created_at": createdAt,
        "domain":     domain,
    })
}

// --- CREATE TRIAL USER ---
func createTrialUser(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        jsonResponse(w, http.StatusMethodNotAllowed, false, "Method not allowed", nil)
        return
    }

    var req UserRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        jsonResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
        return
    }

    if req.Password == "" || req.Minutes <= 0 {
        jsonResponse(w, http.StatusBadRequest, false, "Password dan minutes harus valid", nil)
        return
    }

    mutex.Lock()
    defer mutex.Unlock()

    config, err := loadConfig()
    if err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal membaca config", nil)
        return
    }

    for _, p := range config.Auth.Config {
        if p == req.Password {
            jsonResponse(w, http.StatusConflict, false, "User sudah ada", nil)
            return
        }
    }

    config.Auth.Config = append(config.Auth.Config, req.Password)
    if err := saveConfig(config); err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal menyimpan config", nil)
        return
    }

    now := time.Now()
    // Hitung expired presisi sampai detik
    expDate := now.Add(time.Duration(req.Minutes) * time.Minute).Format(DateTimeFormat)
    createdAt := now.Format(DateTimeFormat)

    users, err := loadUsers()
    if err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal membaca database user", nil)
        return
    }

    newUser := UserStore{
        Password:  req.Password,
        Expired:   expDate,
        CreatedAt: createdAt,
        Status:    "active",
        Type:      "trial",
    }
    users = append(users, newUser)

    if err := saveUsers(users); err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal menyimpan database user", nil)
        return
    }

    if err := restartService(); err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal merestart service", nil)
        return
    }

    jsonResponse(w, http.StatusOK, true, "Trial User berhasil dibuat", map[string]string{
        "password":   req.Password,
        "expired":    expDate,
        "created_at": createdAt,
        "type":       "trial",
    })
}

func deleteUser(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        jsonResponse(w, http.StatusMethodNotAllowed, false, "Method not allowed", nil)
        return
    }

    var req UserRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        jsonResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
        return
    }

    mutex.Lock()
    defer mutex.Unlock()

    config, err := loadConfig()
    if err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal membaca config", nil)
        return
    }

    foundInConfig := false
    newConfigAuth := []string{}
    for _, p := range config.Auth.Config {
        if p == req.Password {
            foundInConfig = true
        } else {
            newConfigAuth = append(newConfigAuth, p)
        }
    }

    if foundInConfig {
        config.Auth.Config = newConfigAuth
        if err := saveConfig(config); err != nil {
            jsonResponse(w, http.StatusInternalServerError, false, "Gagal menyimpan config", nil)
            return
        }
    }

    users, err := loadUsers()
    if err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal membaca database user", nil)
        return
    }

    foundInDB := false
    newUsers := []UserStore{}
    for _, u := range users {
        if u.Password == req.Password {
            foundInDB = true
            continue
        }
        newUsers = append(newUsers, u)
    }

    if !foundInConfig && !foundInDB {
        jsonResponse(w, http.StatusNotFound, false, "User tidak ditemukan", nil)
        return
    }

    if foundInDB {
        if err := saveUsers(newUsers); err != nil {
            jsonResponse(w, http.StatusInternalServerError, false, "Gagal menyimpan database user", nil)
            return
        }
    }

    if foundInConfig {
        if err := restartService(); err != nil {
            jsonResponse(w, http.StatusInternalServerError, false, "Gagal merestart service", nil)
            return
        }
    }

    jsonResponse(w, http.StatusOK, true, "User berhasil dihapus", nil)
}

func renewUser(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        jsonResponse(w, http.StatusMethodNotAllowed, false, "Method not allowed", nil)
        return
    }

    var req UserRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        jsonResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
        return
    }

    mutex.Lock()
    defer mutex.Unlock()

    users, err := loadUsers()
    if err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal membaca database user", nil)
        return
    }

    found := false
    newUsers := []UserStore{}
    var newExpDate string

    for _, u := range users {
        if u.Password == req.Password {
            found = true
            
            // Normalisasi tipe (jika kosong dianggap regular)
            userType := u.Type
            if userType == "" { 
                userType = "regular" 
            }

            // --- BLOKIRAN UNTUK USER TRIAL ---
            if userType == "trial" {
                jsonResponse(w, http.StatusBadRequest, false, "Akun trial tidak dapat diperpanjang", nil)
                return // Langsung hentikan proses di sini, tidak ada penyimpanan data
            }
            // ----------------------------------

            currentExp := parseTime(u.Expired)
            
            if currentExp.IsZero() {
                currentExp = time.Now()
            }
            
            if currentExp.Before(time.Now()) {
                currentExp = time.Now()
            }

            // Karena sudah dipastikan bukan trial di atas, hanya proses logika Regular (Days)
            days := req.Days
            if days <= 0 { 
                days = 1 
            }
            
            newExp := currentExp.Add(time.Duration(days) * 24 * time.Hour)
            newExpDate = newExp.Format(DateTimeFormat)
            
            u.Expired = newExpDate
            
            if u.Status == "locked" {
                u.Status = "active"
                go enableUser(req.Password)
            }

            newUsers = append(newUsers, u)
        } else {
            newUsers = append(newUsers, u)
        }
    }

    if !found {
        jsonResponse(w, http.StatusNotFound, false, "User tidak ditemukan di database", nil)
        return
    }

    if err := saveUsers(newUsers); err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal menyimpan database user", nil)
        return
    }

    if err := restartService(); err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal merestart service", nil)
        return
    }

    jsonResponse(w, http.StatusOK, true, "User berhasil diperpanjang", map[string]string{
        "password": req.Password,
        "expired":  newExpDate,
    })
}

func listUsers(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        jsonResponse(w, http.StatusMethodNotAllowed, false, "Method not allowed", nil)
        return
    }

    users, err := loadUsers()
    if err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal membaca database user", nil)
        return
    }

    type UserInfo struct {
        Password  string `json:"password"`
        Expired   string `json:"expired"`
        CreatedAt string `json:"created_at"`
        Status    string `json:"status"`
        Type      string `json:"type"`
    }

    userList := []UserInfo{}
    now := time.Now()

    for _, u := range users {
        expTime := parseTime(u.Expired)
        
        status := "Active"
        if u.Status == "locked" {
            status = "Locked"
        } else if expTime.Before(now) {
            status = "Expired"
        }
        
        // Normalisasi Type untuk display
        userType := u.Type
        if userType == "" { userType = "regular" }
        
        userList = append(userList, UserInfo{
            Password:  u.Password,
            Expired:   u.Expired,
            CreatedAt: u.CreatedAt,
            Status:    status,
            Type:      userType,
        })
    }

    jsonResponse(w, http.StatusOK, true, "Daftar user", userList)
}

func loadBackupConfig() (string, string, error) {
    data, err := ioutil.ReadFile(BackupConfFile)
    if err != nil {
        return "", "", err
    }

    var token, chatID string
    lines := strings.Split(string(data), "\n")

    for _, line := range lines {
        line = strings.TrimSpace(line)
        
        // Abaikan baris kosong atau komentar (diawali #)
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }

        // Pecah baris berdasarkan tanda =
        parts := strings.SplitN(line, "=", 2)
        if len(parts) < 2 {
            continue
        }

        key := strings.TrimSpace(parts[0])
        val := strings.TrimSpace(parts[1])

        // Mapping key (abaikan huruf besar kecil)
        if strings.EqualFold(key, "TOKEN") || strings.EqualFold(key, "BOT_TOKEN") {
            token = val
        } else if strings.EqualFold(key, "CHAT_ID") || strings.EqualFold(key, "CHAT") {
            chatID = val
        }
    }

    // Validasi minimal
    if token == "" || chatID == "" {
        return "", "", fmt.Errorf("token atau chat_id tidak ditemukan di %s", BackupConfFile)
    }

    return token, chatID, nil
}

func backupToTelegram(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        jsonResponse(w, http.StatusMethodNotAllowed, false, "Method not allowed", nil)
        return
    }

    // 1. Ambil Token dan Chat ID dari bot.conf
    token, chatID, err := loadBackupConfig()
    if err != nil {
        log.Printf("Error Backup Config: %v", err)
        jsonResponse(w, http.StatusInternalServerError, false, err.Error(), nil)
        return
    }

    // 2. Buat File Backup di /tmp
    timestamp := time.Now().Format("2006-01-02_15-04")
    tempFile := fmt.Sprintf("/tmp/zivpn-backup-%s.tar.gz", timestamp)
    
    tarCmd := exec.Command("tar", "-czf", tempFile, "-C", "/etc", "zivpn")
    if err := tarCmd.Run(); err != nil {
        log.Printf("Gagal membuat file tar: %v", err)
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal membuat backup file", nil)
        return
    }
    defer os.Remove(tempFile) // Hapus file sementara setelah selesai

    // 3. Kirim ke Telegram
    apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", token)
    
    curlCmd := exec.Command("curl", 
        "-s", 
        "-X", "POST",
        apiURL,
        "-F", fmt.Sprintf("chat_id=%s", chatID),
        "-F", fmt.Sprintf("document=@%s", tempFile),
        "-F", fmt.Sprintf("caption=ZiVPN Backup %s", timestamp))

    curlOutput, err := curlCmd.CombinedOutput()
    if err != nil {
        log.Printf("Gagal kirim ke Telegram: %s", string(curlOutput))
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal mengirim ke Telegram", nil)
        return
    }

    log.Printf("Backup berhasil dikirim ke Telegram: %s", string(curlOutput))
    jsonResponse(w, http.StatusOK, true, "Backup berhasil dikirim ke Telegram", nil)
}

func getSystemInfo(w http.ResponseWriter, r *http.Request) {
    cmd := exec.Command("curl", "-s", "ifconfig.me")
    ipPub, _ := cmd.Output()

    cmd = exec.Command("hostname", "-I")
    ipPriv, _ := cmd.Output()

    domain := "Tidak diatur"
    if domainBytes, err := ioutil.ReadFile(DomainFile); err == nil {
        domain = strings.TrimSpace(string(domainBytes))
    }

    apiPort := "8080"
    if pBytes, err := ioutil.ReadFile(PortFile); err == nil {
        apiPort = strings.TrimSpace(string(pBytes))
    }

    info := map[string]string{
        "domain":     domain,
        "public_ip":  strings.TrimSpace(string(ipPub)),
        "private_ip": strings.Fields(string(ipPriv))[0],
        "vpn_port":   "5667",
        "api_port":   apiPort, // Memakai variabel agar error hilang
        "service":    "zivpn",
    }

    jsonResponse(w, http.StatusOK, true, "System Info", info)
}

// --- CHECK EXPIRATION (OPTIMIZED) ---
func checkExpiration(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        jsonResponse(w, http.StatusMethodNotAllowed, false, "Method not allowed", nil)
        return
    }

    users, err := loadUsers()
    if err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal membaca database user", nil)
        return
    }

    config, err := loadConfig()
    if err != nil {
        jsonResponse(w, http.StatusInternalServerError, false, "Gagal membaca config", nil)
        return
    }

    // Buat map cepat untuk cek user yang aktif di config
    activeUsers := make(map[string]bool)
    for _, p := range config.Auth.Config {
        activeUsers[p] = true
    }

    // Siapkan list user yang akan dihapus
    passwordsToRevoke := []string{}
    now := time.Now()

    for _, u := range users {
        // Parse waktu expired user (support format lama & baru)
        expTime := parseTime(u.Expired)

        // Cek apakah waktu expired SUDAH LEWAT waktu sekarang (detik presisi)
        if expTime.Before(now) && activeUsers[u.Password] {
            log.Printf("User %s expired (Exp: %s). Marked for revocation.\n", u.Password, u.Expired)
            passwordsToRevoke = append(passwordsToRevoke, u.Password)
        }
    }

    if len(passwordsToRevoke) > 0 {
        // Update Config: Hapus semua user expired sekaligus
        newConfigAuth := []string{}
        for _, p := range config.Auth.Config {
            keep := true
            for _, revokePass := range passwordsToRevoke {
                if p == revokePass {
                    keep = false
                    break
                }
            }
            if keep {
                newConfigAuth = append(newConfigAuth, p)
            }
        }

        config.Auth.Config = newConfigAuth

        // Simpan Config
        if err := saveConfig(config); err != nil {
            jsonResponse(w, http.StatusInternalServerError, false, "Gagal menyimpan config setelah expire", nil)
            return
        }

        // Restart Service HANYA SEKALI
        if err := restartService(); err != nil {
            jsonResponse(w, http.StatusInternalServerError, false, "Gagal merestart service", nil)
            return
        }
    }

    jsonResponse(w, http.StatusOK, true, fmt.Sprintf("Expiration check complete. Revoked: %d", len(passwordsToRevoke)), nil)
}

func revokeAccess(password string) {
    mutex.Lock()
    defer mutex.Unlock()

    config, err := loadConfig()
    if err == nil {
        newConfigAuth := []string{}
        changed := false
        for _, p := range config.Auth.Config {
            if p == password {
                changed = true
            } else {
                newConfigAuth = append(newConfigAuth, p)
            }
        }
        if changed {
            config.Auth.Config = newConfigAuth
            saveConfig(config)
            restartService()
        }
    }
}

func enableUser(password string) {
    mutex.Lock()
    defer mutex.Unlock()

    config, err := loadConfig()
    if err != nil {
        return
    }

    exists := false
    for _, p := range config.Auth.Config {
        if p == password {
            exists = true
            break
        }
    }

    if !exists {
        config.Auth.Config = append(config.Auth.Config, password)
        saveConfig(config)
        restartService()
    }
}

func loadConfig() (Config, error) {
    var config Config
    file, err := ioutil.ReadFile(ConfigFile)
    if err != nil {
        return config, err
    }
    err = json.Unmarshal(file, &config)
    return config, err
}

func saveConfig(config Config) error {
    data, err := json.MarshalIndent(config, "", "  ")
    if err != nil {
        return err
    }
    return ioutil.WriteFile(ConfigFile, data, 0644)
}

func loadUsers() ([]UserStore, error) {
    var users []UserStore
    file, err := ioutil.ReadFile(UserDB)
    if err != nil {
        if os.IsNotExist(err) {
            return users, nil
        }
        return nil, err
    }
    err = json.Unmarshal(file, &users)
    return users, err
}

func saveUsers(users []UserStore) error {
    data, err := json.MarshalIndent(users, "", "  ")
    if err != nil {
        return err
    }
    return ioutil.WriteFile(UserDB, data, 0644)
}

func restartService() error {
    cmd := exec.Command("systemctl", "restart", "zivpn.service")
    return cmd.Run()
}