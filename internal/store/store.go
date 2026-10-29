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
	"time"

	_ "modernc.org/sqlite"
)

// 账号健康。空字符串与 ready 都表示可以接任务。
// cooling 到 cool_until 后自动恢复。exhausted 在 cool_until 为 0 时要等用户点恢复。
const (
	HealthReady     = "ready"
	HealthCooling   = "cooling"
	HealthExhausted = "exhausted"
)

// Browser 一行 browsers 表。一行是一个账号：Chrome / Edge 的独立 profile，或一条扩展会话。
type Browser struct {
	ID          int64
	Platform    string
	Name        string
	Port        int
	UserDataDir string
	IsRunning   bool
	IsLoggedIn  bool
	Health      string
	CoolUntil   int64 // Unix 秒。0 表示不会自动恢复。
	LastUsed    int64
	LastError   string
	ExtAccount  string // 扩展 hello 里的 profile id。空字符串表示本机启动的浏览器。
	DayKey      string // 本地日期 2006-01-02。换日之后 UsedToday 归零。
	DayCount    int
}

// Store 线程安全的配置库。
type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

// defaults 是新库的初始设置。除 browserBackend、extBridgePort、extBridgeToken、concurrency 外，键与原软件 settings 表实测默认值对应。
// browserBackend 只接受 extension 或 ego，默认 ego。
// extension 时千问和豆包走扩展桥，不要求本机浏览器实例。
// ego 表示不走扩展，页面由 browser_backend（Chrome 或 Edge）打开。
// concurrency 为 0 时按当前可用账号数自动调度；正数是手动上限。
var defaults = map[string]string{
	"selected_platforms":      "豆包",
	"task_type":               "ImageGeneration",
	"send_strategy":           "顺序循环",
	"concurrency":             "0",
	"retry_count":             "3",
	"sent_count":              "1",
	"send_wait_seconds":       "5",
	"wait_before_sending":     "1",
	"wait_result":             "1",
	"wait_result_seconds":     "120",
	"task_switching":          "1",
	"task_switching_count":    "50",
	"start_browser_index":     "1",
	"prompts":                 "",
	"prompt_suffix":           "",
	"use_suffix":              "0",
	"save_mode":               "单文件夹汇总",
	"naming_rule":             "原文件名",
	"output_path":             "",
	"file_prefix":             "",
	"file_prefix_enabled":     "0",
	"file_suffix":             "",
	"file_suffix_enabled":     "0",
	"source_images":           "",
	"captcha_enabled":         "1",
	"captcha_backend":         "free",
	"captcha_custom_url":      "https://api.siliconflow.cn/v1/chat/completions",
	"captcha_custom_key":      "",
	"captcha_custom_model":    "Qwen/Qwen3-VL-8B-Instruct",
	"captcha_custom_timeout":  "30",
	"captcha_ikunocr_key":     "",
	"captcha_ikunocr_timeout": "30",
	"captcha_ikunocr_track":   "dom2",
	"minimize_browser":        "0",
	"sent_count_total":        "0",
	"browserBackend":          "ego",
	"extBridgePort":           "18765",
	"extBridgeToken":          "dev-token",
	"auto_check_updates":      "1",
	"update_mirror":           "",
	"update_skipped_version":  "",
	"proxy_mode":              "system",
	"proxy_server":            "",
	"daily_cap":               "20",
	"risky_words":             "血腥,暴力,裸体,色情,性感,裸露,枪支,武器,自杀,毒品",
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
	if err := migrateBrowsers(db); err != nil {
		db.Close()
		return nil, err
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
	rows, err := s.db.Query(`SELECT id, platform, name, port, COALESCE(user_data_dir,''), is_running, is_logged_in,
		COALESCE(health,'ready'), COALESCE(cool_until,0), COALESCE(last_used,0), COALESCE(last_error,''), COALESCE(ext_account,''),
		COALESCE(day_key,''), COALESCE(day_count,0)
		FROM browsers ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Browser
	for rows.Next() {
		var b Browser
		var run, login int
		if err := rows.Scan(&b.ID, &b.Platform, &b.Name, &b.Port, &b.UserDataDir, &run, &login, &b.Health, &b.CoolUntil, &b.LastUsed, &b.LastError, &b.ExtAccount, &b.DayKey, &b.DayCount); err == nil {
			b.IsRunning = run != 0
			b.IsLoggedIn = login != 0
			out = append(out, b)
		}
	}
	return out
}

// Usable 报告这个账号现在能不能接任务。冷却或额度到期后自动恢复。
func (b Browser) Usable(now time.Time) bool {
	switch b.Health {
	case "", HealthReady:
		return true
	case HealthCooling, HealthExhausted:
		return b.CoolUntil > 0 && !now.Before(time.Unix(b.CoolUntil, 0))
	default:
		return true
	}
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

// RenameBrowser 只改显示名。用户数据目录保持创建时的路径。
func (s *Store) RenameBrowser(id int64, name string) {
	if s == nil || id <= 0 {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE browsers SET name=? WHERE id=?`, name, id)
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

func loginHold(health, last string) bool {
	if health != HealthExhausted && health != HealthCooling {
		return false
	}
	return strings.Contains(last, "登录") || strings.Contains(last, "验证")
}

// ConfirmLogin 记下一次成功的登录探测。
// 未登录或安全验证的挂起会清掉，账号可以继续轮换。额度和限流的冷却不动。
// 已经登录且没有这类挂起时返回 false。
func (s *Store) ConfirmLogin(id int64) bool {
	if s == nil || id <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var login int
	var health, last string
	err := s.db.QueryRow(`SELECT is_logged_in, COALESCE(health,'ready'), COALESCE(last_error,'') FROM browsers WHERE id=?`, id).Scan(&login, &health, &last)
	if err != nil {
		return false
	}
	hold := loginHold(health, last)
	if login != 0 && !hold {
		return false
	}
	if hold {
		_, err = s.db.Exec(`UPDATE browsers SET is_running=1, is_logged_in=1, health=?, cool_until=0, last_error='' WHERE id=?`, HealthReady, id)
	} else {
		_, err = s.db.Exec(`UPDATE browsers SET is_running=1, is_logged_in=1 WHERE id=?`, id)
	}
	return err == nil
}

// DropUnloggedLocal 删掉还没登录成功的本机账号。
// 扩展行和 keep 里的 id 留着（登录还在进行）。返回实际删掉的行，便于清配置目录。
func (s *Store) DropUnloggedLocal(keep []int64) []Browser {
	if s == nil {
		return nil
	}
	hold := map[int64]struct{}{}
	for _, id := range keep {
		if id > 0 {
			hold[id] = struct{}{}
		}
	}
	var candidates []Browser
	for _, b := range s.Browsers() {
		if b.ExtAccount != "" || b.IsLoggedIn {
			continue
		}
		if _, ok := hold[b.ID]; ok {
			continue
		}
		candidates = append(candidates, b)
	}
	if len(candidates) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var dropped []Browser
	for _, b := range candidates {
		res, err := s.db.Exec(`DELETE FROM browsers WHERE id=? AND is_logged_in=0 AND COALESCE(ext_account,'')=''`, b.ID)
		if err != nil {
			continue
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			dropped = append(dropped, b)
		}
	}
	return dropped
}

// DeleteByExtensionAccount 删掉这个配置文件标识在各平台上的扩展账号。
// 空标识不能用：本地浏览器的 ext_account 就是空字符串，一刀会把它们全删掉。
func (s *Store) DeleteByExtensionAccount(account string) {
	if s == nil {
		return
	}
	account = strings.TrimSpace(account)
	if account == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`DELETE FROM browsers WHERE ext_account=?`, account)
}

// UpdateBrowserHealth 记下额度、限流、验证码或登录失败。coolUntil 为 0 时不会自动恢复。
func (s *Store) UpdateBrowserHealth(id int64, health string, coolUntil int64, lastError string) {
	if s == nil || id <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE browsers SET health=?, cool_until=?, last_error=? WHERE id=?`, health, coolUntil, lastError, id)
}

// ResetBrowserHealth 把账号标回可使用。用户重新登录或确认额度恢复后点这一下。
func (s *Store) ResetBrowserHealth(id int64) {
	s.UpdateBrowserHealth(id, HealthReady, 0, "")
}

// UsedToday 是这个账号在 now 这一天已经派出的次数。换日之后是 0。
func (b Browser) UsedToday(now time.Time) int {
	if b.DayKey != now.Format("2006-01-02") || b.DayCount < 0 {
		return 0
	}
	return b.DayCount
}

// AddUse 给这个账号的当天次数加一。日期不同就从 1 重新计。
func (s *Store) AddUse(id int64, now time.Time) {
	if s == nil || id <= 0 {
		return
	}
	key := now.Format("2006-01-02")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE browsers SET day_count = CASE WHEN day_key = ? THEN COALESCE(day_count,0) + 1 ELSE 1 END, day_key = ? WHERE id = ?`, key, key, id)
}

// TouchBrowser 记下最近一次接任务的时间，供最少最近使用的轮换。
func (s *Store) TouchBrowser(id int64, now time.Time) {
	if s == nil || id <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE browsers SET last_used=? WHERE id=?`, now.Unix(), id)
}

// SaveLoggedInExtension 记下一个已经确认登录的扩展账号。豆包和千问同一规则。
// 只是连上页面桥还不调用这里。已有行的冷却和今日次数保留，登录挂起会清掉。
func (s *Store) SaveLoggedInExtension(platform, account, name string) (Browser, error) {
	b, err := s.UpsertExtensionAccount(platform, account, name)
	if err != nil {
		return Browser{}, err
	}
	s.ConfirmLogin(b.ID)
	for _, row := range s.Browsers() {
		if row.ID == b.ID {
			return row, nil
		}
	}
	b.IsLoggedIn = true
	if b.Health == "" {
		b.Health = HealthReady
	}
	return b, nil
}

// DropExtensionAccount 删掉这个平台上的一条扩展账号。空标识不能用。
func (s *Store) DropExtensionAccount(platform, account string) {
	if s == nil {
		return
	}
	platform = strings.TrimSpace(platform)
	account = strings.TrimSpace(account)
	if platform == "" || account == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`DELETE FROM browsers WHERE platform=? AND ext_account=?`, platform, account)
}

// DropUnloggedExtensions 删掉还没确认登录的扩展账号，豆包和千问都算。
func (s *Store) DropUnloggedExtensions() {
	if s == nil {
		return
	}
	var ids []int64
	for _, b := range s.Browsers() {
		if b.ExtAccount != "" && !b.IsLoggedIn {
			ids = append(ids, b.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		s.db.Exec(`DELETE FROM browsers WHERE id=? AND is_logged_in=0 AND COALESCE(ext_account,'')<>''`, id)
	}
}

// UpsertExtensionAccount 按扩展 profile id 登记或取回账号。
// 同一配置文件重连不会清掉冷却状态。hello 只是连上时不要调用它；
// 确认登录后走 SaveLoggedInExtension。
func (s *Store) UpsertExtensionAccount(platform, account, name string) (Browser, error) {
	if s == nil {
		return Browser{}, fmt.Errorf("没有配置库")
	}
	platform = strings.TrimSpace(platform)
	account = strings.TrimSpace(account)
	name = strings.TrimSpace(name)
	if platform == "" || account == "" {
		return Browser{}, fmt.Errorf("扩展账号缺少平台或标识")
	}
	if name == "" {
		short := account
		if len(short) > 4 {
			short = short[:4]
		}
		name = "扩展账号" + short
	}
	s.mu.Lock()
	var id int64
	err := s.db.QueryRow(`SELECT id FROM browsers WHERE platform=? AND ext_account=?`, platform, account).Scan(&id)
	s.mu.Unlock()
	if err == nil {
		s.mu.Lock()
		s.db.Exec(`UPDATE browsers SET name=? WHERE id=? AND name LIKE '扩展账号%'`, name, id)
		s.mu.Unlock()
		for _, b := range s.Browsers() {
			if b.ID == id {
				return b, nil
			}
		}
		return Browser{}, fmt.Errorf("扩展账号 %d 读取失败", id)
	}
	if err != sql.ErrNoRows {
		return Browser{}, err
	}
	b, err := s.AddBrowser(platform, name, "")
	if err != nil {
		return Browser{}, err
	}
	s.mu.Lock()
	_, err = s.db.Exec(`UPDATE browsers SET ext_account=? WHERE id=?`, account, b.ID)
	s.mu.Unlock()
	if err != nil {
		return Browser{}, err
	}
	b.ExtAccount = account
	b.Health = HealthReady
	return b, nil
}

func migrateBrowsers(db *sql.DB) error {
	alters := []string{
		`ALTER TABLE browsers ADD COLUMN health TEXT DEFAULT 'ready'`,
		`ALTER TABLE browsers ADD COLUMN cool_until INTEGER DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN last_used INTEGER DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN last_error TEXT DEFAULT ''`,
		`ALTER TABLE browsers ADD COLUMN ext_account TEXT DEFAULT ''`,
		`ALTER TABLE browsers ADD COLUMN day_key TEXT DEFAULT ''`,
		`ALTER TABLE browsers ADD COLUMN day_count INTEGER DEFAULT 0`,
	}
	for _, stmt := range alters {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("升级 browsers 表: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS browsers_ext_account ON browsers(platform, ext_account) WHERE ext_account <> ''`); err != nil {
		return fmt.Errorf("升级 browsers 索引: %w", err)
	}
	return nil
}
