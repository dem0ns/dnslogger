package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var defaultConfig = map[string]string{
	"return_ip":    "127.0.0.1",
	"upstream_dns": "",
	"listen_dns":   "0.0.0.0:53",
	"listen_http":  "127.0.0.1:8053",
	"db_file":      "dnslog.db",
}

func InitDB(dbPath string) {
	fmt.Println("[*] Initializing database...")
	var err error
	db, err = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	if err = db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	createTables()
	migrateDB()
	loadConfigToMemory()
	fmt.Println("[*] Database ready.")
}

func createTables() {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS configs (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS filter_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			pattern TEXT NOT NULL,
			match_type TEXT NOT NULL DEFAULT 'wildcard',
			ip TEXT DEFAULT '',
			action TEXT NOT NULL DEFAULT 'allow',
			enabled INTEGER DEFAULT 1,
			description TEXT,
			created_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS dnslog (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT,
			type TEXT,
			resp TEXT,
			src TEXT,
			created_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dnslog_domain ON dnslog(domain)`,
		`CREATE INDEX IF NOT EXISTS idx_dnslog_created ON dnslog(created_at)`,
	}
	for _, ddl := range tables {
		if _, err := db.Exec(ddl); err != nil {
			log.Fatalf("Failed to create table: %v", err)
		}
	}

	// Insert default config if empty
	var count int
	db.QueryRow("SELECT COUNT(*) FROM configs").Scan(&count)
	if count == 0 {
		for k, v := range defaultConfig {
			db.Exec("INSERT INTO configs (key, value) VALUES (?, ?)", k, v)
		}
		fmt.Println("[*] Default config inserted.")
	}
}

func migrateDB() {
	// Add ip column to filter_rules if not exists (for upgrades)
	db.Exec("ALTER TABLE filter_rules ADD COLUMN ip TEXT DEFAULT ''")
}

func loadConfigToMemory() {
	config = make(map[string]string)
	rows, err := db.Query("SELECT key, value FROM configs")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		config[k] = v
	}
}

// --- DNS Log Operations ---

func InsertDNSLog(domain, typ, resp, src string) {
	_, err := db.Exec("INSERT INTO dnslog (domain, type, resp, src, created_at) VALUES (?, ?, ?, ?, ?)",
		domain, typ, resp, src, time.Now().Format(TimeLayout))
	if err != nil {
		log.Printf("[-] Failed to save DNS log: %v", err)
	}
}

func QueryDNSLogs(domain string, limit, offset int) ([]DNSLog, error) {
	var rows *sql.Rows
	var err error

	if domain != "" {
		rows, err = db.Query("SELECT id, domain, type, resp, src, datetime(created_at) FROM dnslog WHERE domain LIKE ? ORDER BY id DESC LIMIT ? OFFSET ?",
			"%"+domain+"%", limit, offset)
	} else {
		rows, err = db.Query("SELECT id, domain, type, resp, src, datetime(created_at) FROM dnslog ORDER BY id DESC LIMIT ? OFFSET ?",
			limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []DNSLog
	for rows.Next() {
		var d DNSLog
		var timeCreated string
		if err := rows.Scan(&d.Id, &d.Domain, &d.Type, &d.Resp, &d.Src, &timeCreated); err != nil {
			continue
		}
		d.Created, _ = time.Parse(TimeLayout, timeCreated)
		logs = append(logs, d)
	}
	if logs == nil {
		logs = []DNSLog{}
	}
	return logs, nil
}

func CountDNSLogs(domain string) (int, error) {
	var count int
	if domain != "" {
		err := db.QueryRow("SELECT COUNT(*) FROM dnslog WHERE domain LIKE ?", "%"+domain+"%").Scan(&count)
		return count, err
	}
	err := db.QueryRow("SELECT COUNT(*) FROM dnslog").Scan(&count)
	return count, err
}

func ValidateDomain(domain string, minutes int) (*DNSLog, error) {
	var d DNSLog
	var timeCreated string
	since := time.Now().Add(time.Duration(-minutes) * time.Minute).Format(TimeLayout)
	err := db.QueryRow("SELECT id, domain, type, resp, src, datetime(created_at) FROM dnslog WHERE domain = ? AND created_at >= ? LIMIT 1",
		domain, since).Scan(&d.Id, &d.Domain, &d.Type, &d.Resp, &d.Src, &timeCreated)
	if err != nil {
		return nil, err
	}
	d.Created, _ = time.Parse(TimeLayout, timeCreated)
	return &d, nil
}

func ClearDNSLogs() error {
	_, err := db.Exec("DELETE FROM dnslog")
	return err
}

// --- Config Operations ---

func GetConfigMap() map[string]string {
	result := make(map[string]string)
	rows, err := db.Query("SELECT key, value FROM configs")
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		result[k] = v
	}
	return result
}

func UpdateConfigMap(items map[string]string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for k, v := range items {
		_, err := tx.Exec("INSERT OR REPLACE INTO configs (key, value) VALUES (?, ?)", k, v)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Update in-memory config
	for k, v := range items {
		config[k] = v
	}
	return nil
}

// --- Filter Rule Operations ---

func GetFilterRules() ([]FilterRule, error) {
	rows, err := db.Query("SELECT id, name, pattern, match_type, COALESCE(ip,''), action, enabled, COALESCE(description,''), created_at FROM filter_rules ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []FilterRule
	for rows.Next() {
		var r FilterRule
		var enabled int
		var createdAt string
		if err := rows.Scan(&r.Id, &r.Name, &r.Pattern, &r.MatchType, &r.IP, &r.Action, &enabled, &r.Description, &createdAt); err != nil {
			continue
		}
		r.Enabled = enabled == 1
		r.CreatedAt, _ = time.Parse(TimeLayout, createdAt)
		rules = append(rules, r)
	}
	if rules == nil {
		rules = []FilterRule{}
	}
	return rules, nil
}

func GetEnabledFilterRules() ([]FilterRule, error) {
	rows, err := db.Query("SELECT id, name, pattern, match_type, COALESCE(ip,''), action, enabled, COALESCE(description,'') FROM filter_rules WHERE enabled = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []FilterRule
	for rows.Next() {
		var r FilterRule
		var enabled int
		rows.Scan(&r.Id, &r.Name, &r.Pattern, &r.MatchType, &r.IP, &r.Action, &enabled, &r.Description)
		r.Enabled = true
		rules = append(rules, r)
	}
	return rules, nil
}

func CreateFilterRule(r *FilterRule) error {
	result, err := db.Exec("INSERT INTO filter_rules (name, pattern, match_type, ip, action, enabled, description, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		r.Name, r.Pattern, r.MatchType, r.IP, r.Action, boolToInt(r.Enabled), r.Description, time.Now().Format(TimeLayout))
	if err != nil {
		return err
	}
	id, _ := result.LastInsertId()
	r.Id = int(id)
	return nil
}

func UpdateFilterRule(r *FilterRule) error {
	_, err := db.Exec("UPDATE filter_rules SET name=?, pattern=?, match_type=?, ip=?, action=?, enabled=?, description=? WHERE id=?",
		r.Name, r.Pattern, r.MatchType, r.IP, r.Action, boolToInt(r.Enabled), r.Description, r.Id)
	return err
}

func DeleteFilterRule(id int) error {
	_, err := db.Exec("DELETE FROM filter_rules WHERE id = ?", id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
