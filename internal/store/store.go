// Package store 管理 SQLite 配置库（database/config.db），表结构与原软件完全兼容：
// browsers / settings / prompts。可直接读写原软件生成的 config.db。
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// Browser 一行 browsers 表。
type Browser struct {
	ID          int64
	Platform    string
	Name        string
	Port        int
	UserDataDir string
	IsRunning   bool
	IsLoggedIn  bool
}

// Store 线程安全的配置库。
type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

// defaults 与原软件 settings 表实测默认值一一对应。
var defaults = map[string]string{
	"selected_platforms":     "豆包",
	"task_type":              "ImageGeneration",
	"send_strategy":          "顺序循环",
	"concurrency":            "1",
	"retry_count":            "3",
	"sent_count":             "1",
	"send_wait_seconds":      "5",
	"wait_before_sending":    "1",
	"wait_result":            "1",
	"wait_result_seconds":    "120",
	"task_switching":         "1",
	"task_switching_count":   "50",
	"start_browser_index":    "1",
	"prompts":                "",
	"prompt_suffix":          "",
	"use_suffix":             "0",
	"save_mode":              "单文件夹汇总",
	"naming_rule":            "原文件名",
	"output_path":            "",
	"file_prefix":            "",
	"file_prefix_enabled":    "0",
	"file_suffix":            "",
	"file_suffix_enabled":    "0",
	"source_images":          "",
	"captcha_enabled":        "1",
	"captcha_backend":        "free",
	"captcha_custom_url":     "https://api.siliconflow.cn/v1/chat/completions",
	"captcha_custom_key":     "",
	"captcha_custom_model":   "Qwen/Qwen3-VL-8B-Instruct",
	"captcha_custom_timeout": "30",
	"captcha_ikunocr_key":    "",
	"captcha_ikunocr_timeout": "30",
	"captcha_ikunocr_track":  "dom2",
	"minimize_browser":       "0",
	"sent_count_total":       "0",
}

const schema = `
CREATE TABLE IF NOT EXISTS browsers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	platform TEXT NOT NULL,
	name TEXT NOT NULL,
	port INTEGER NOT NULL,
	user_data_dir TEXT,
	is_running INTEGER DEFAULT 0,
	is_logged_in INTEGER DEFAULT 0,
	created_at TEXT DEFAULT (datetime('now','localtime'))
);
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT
);
CREATE TABLE IF NOT EXISTS prompts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	content TEXT NOT NULL,
	suffix TEXT,
	use_suffix INTEGER DEFAULT 0,
	send_strategy TEXT DEFAULT '顺序循环',
	created_at TEXT DEFAULT (datetime('now','localtime'))
);`

// Open 打开（或创建）配置库。
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化表结构: %w", err)
	}
	s := &Store{db: db, path: path}
	for k, v := range defaults {
		s.db.Exec(`INSERT OR IGNORE INTO settings(key, value) VALUES(?, ?)`, k, v)
	}
	return s, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// Get 读取设置（缺失时回默认值）。
func (s *Store) Get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err != nil {
		return defaults[key]
	}
	return v
}

// Set 写入设置。
func (s *Store) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
}

// GetInt 读取整型设置。
func (s *Store) GetInt(key string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s.Get(key)))
	return n
}

// GetBool 读取布尔设置（"1"/"0"）。
func (s *Store) GetBool(key string) bool { return s.GetInt(key) != 0 }

// SetInt 写入整型设置。
func (s *Store) SetInt(key string, n int) { s.Set(key, strconv.Itoa(n)) }

// Platforms 返回选中的平台列表（selected_platforms 以逗号分隔，如 "豆包,千问"）。
func (s *Store) Platforms() []string {
	var out []string
	for _, p := range strings.Split(s.Get("selected_platforms"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SetPlatforms 保存选中平台。
func (s *Store) SetPlatforms(ps []string) { s.Set("selected_platforms", strings.Join(ps, ",")) }

// ---- browsers ----

// Browsers 返回全部浏览器记录。
func (s *Store) Browsers() []Browser {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, platform, name, port, COALESCE(user_data_dir,''), is_running, is_logged_in FROM browsers ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Browser
	for rows.Next() {
		var b Browser
		var run, login int
		if err := rows.Scan(&b.ID, &b.Platform, &b.Name, &b.Port, &b.UserDataDir, &run, &login); err == nil {
			b.IsRunning = run != 0
			b.IsLoggedIn = login != 0
			out = append(out, b)
		}
	}
	return out
}

// AddBrowser 新增浏览器（端口从 9333 起自动分配）。
func (s *Store) AddBrowser(platform, name, dataDir string) (Browser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var maxPort int
	s.db.QueryRow(`SELECT COALESCE(MAX(port), 9332) FROM browsers`).Scan(&maxPort)
	port := maxPort + 1
	if port < 9333 {
		port = 9333
	}
	res, err := s.db.Exec(`INSERT INTO browsers(platform, name, port, user_data_dir) VALUES(?,?,?,?)`,
		platform, name, port, dataDir)
	if err != nil {
		return Browser{}, err
	}
	id, _ := res.LastInsertId()
	return Browser{ID: id, Platform: platform, Name: name, Port: port, UserDataDir: dataDir}, nil
}

// UpdateBrowser 更新浏览器运行/登录状态。
func (s *Store) UpdateBrowser(id int64, running, loggedIn bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, l := 0, 0
	if running {
		r = 1
	}
	if loggedIn {
		l = 1
	}
	s.db.Exec(`UPDATE browsers SET is_running=?, is_logged_in=? WHERE id=?`, r, l, id)
}

// DeleteBrowser 删除浏览器记录。
func (s *Store) DeleteBrowser(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`DELETE FROM browsers WHERE id=?`, id)
}
