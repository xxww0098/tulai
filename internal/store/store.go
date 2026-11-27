// Package store 管理 SQLite 配置库（database/config.db）：
// browsers / settings / prompts，以及生图、洗图各自的任务会话。
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xxww0098/tulai/internal/logging"

	_ "modernc.org/sqlite"
)

// 账号健康。空字符串与 ready 都表示可以接任务。
// cooling 到 cool_until 后自动恢复。exhausted 在 cool_until 为 0 时要等用户点恢复。
const (
	HealthReady     = "ready"
	HealthCooling   = "cooling"
	HealthExhausted = "exhausted"
)

// EngineFingerprint 标识由 tulai 管理的 fingerprint-chromium 指纹浏览器账号运行环境。
const EngineFingerprint = "Fingerprint"

// AccountQuota 记录从平台网页同步回来的积分/额度、刷新时间、会员等级、限额比例与单号并发上限等状态。
type AccountQuota struct {
	CreditsRemain      int    // -1 表示未同步或无固定余额（如豆包免费版）；>=0 表示真实剩余积分/额度
	CreditsTotal       int    // 本周期/套餐总积分或总额度（千问免费通常为 40）
	CreditCost         int    // 单张生图消耗积分（千问 Qwen-Image 2.0 为 1）
	CreditsResetAt     int64  // 积分到期或下次刷新/恢复时间（Unix 秒，0 表示未知）
	CreditsCycle       string // "daily" | "7d" | "monthly" | "yearly" | ""
	MemberTier         string // "free" | "vip" | ""
	MemberName         string // 如 "免费套餐"、"免费体验"、"标准会员"
	AccountConcurrency int    // 单号并发上限（-1=不限并发，>0=具体上限，0=未知）
	WatermarkFree      bool   // 是否支持/已开启去水印
	UsagePercent       int    // 平台直接上报的窗口已用百分比（-1=未上报，0..100，如豆包 limit_status.used_percent）
	PlatformLimited    bool   // 平台明确返回已触发限额或发送频控
	LimitTips          string // 平台返回的限流/限额说明文案
	ResetCardCount     int    // 可用额度重置卡数量（豆包）
	SyncedAt           int64  // 最近同步时间（Unix 秒，0 表示未同步）
}

// Browser 一行 browsers 表。一行是一个受管 Fingerprint 浏览器账号的独立 profile。
type Browser struct {
	ID                 int64
	Platform           string
	Name               string
	Port               int
	UserDataDir        string
	Engine             string // Fingerprint or empty on legacy accounts.
	Proxy              string // 空值继承全局代理，非空时只用于这个网页账号。
	IsRunning          bool
	IsLoggedIn         bool
	Disabled           bool
	Health             string
	CoolUntil          int64 // Unix 秒。0 表示不会自动恢复。
	LastUsed           int64
	LastError          string
	DayKey             string // 本地日期 2006-01-02。换日之后 UsedToday 归零。
	DayCount           int
	DaySuccess         int
	DayFailed          int
	CreditsRemain      int    // 仅在 CreditsSyncedAt > 0 且 >= 0 时有效；-1 表示未同步或无固定余额。
	CreditsTotal       int    // 当前周期/套餐总积分或额度。
	CreditCost         int    // 单张生图消耗积分。
	CreditsResetAt     int64  // 积分刷新/到期/解限 Unix 秒时间戳。
	CreditsCycle       string // daily / 7d / monthly / yearly / ""
	MemberTier         string // free / vip / ""
	MemberName         string // 套餐或会员名称。
	AccountConcurrency int    // 单号并发上限（-1=不限并发，>0=上限，0=未知）。
	WatermarkFree      bool   // 是否享有去水印权益。
	UsagePercent       int    // 平台直接上报的已用百分比（-1=未上报，0..100）。
	PlatformLimited    bool   // 平台明确返回限额/频控。
	LimitTips          string // 平台限额/频控文案。
	ResetCardCount     int    // 可用额度重置卡张数。
	CreditsSyncedAt    int64  // 最近一次同步平台状态的时间（Unix 秒）。
}

// QuotaSynced 报告该账号是否已经同步过平台积分/会员状态。
func (b Browser) QuotaSynced() bool {
	return b.CreditsSyncedAt > 0
}

// QuotaExpired 报告已同步的积分周期是否已经超过刷新/到期时间。
func (b Browser) QuotaExpired(now time.Time) bool {
	return b.CreditsSyncedAt > 0 && b.CreditsResetAt > 0 && !now.Before(time.Unix(b.CreditsResetAt, 0))
}

// QuotaExhausted 报告已同步且未过刷新时间的平台额度是否已耗尽或被限额。
func (b Browser) QuotaExhausted(now time.Time) bool {
	if !b.QuotaSynced() || b.QuotaExpired(now) {
		return false
	}
	return b.CreditsRemain == 0 || b.PlatformLimited
}

// Quota 返回账号的平台额度快照。未同步时 CreditsRemain 和 UsagePercent 返回 -1。
func (b Browser) Quota() AccountQuota {
	remain := b.CreditsRemain
	usagePct := b.UsagePercent
	if !b.QuotaSynced() {
		remain = -1
		usagePct = -1
	}
	return AccountQuota{
		CreditsRemain:      remain,
		CreditsTotal:       b.CreditsTotal,
		CreditCost:         b.CreditCost,
		CreditsResetAt:     b.CreditsResetAt,
		CreditsCycle:       b.CreditsCycle,
		MemberTier:         b.MemberTier,
		MemberName:         b.MemberName,
		AccountConcurrency: b.AccountConcurrency,
		WatermarkFree:      b.WatermarkFree,
		UsagePercent:       usagePct,
		PlatformLimited:    b.PlatformLimited,
		LimitTips:          b.LimitTips,
		ResetCardCount:     b.ResetCardCount,
		SyncedAt:           b.CreditsSyncedAt,
	}
}

// Store 线程安全的配置库。
type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	path string
}

// defaults 是新库的初始设置。除 browser_backend、concurrency 外，键与原软件 settings 表实测默认值对应。
// 账号统一使用受管 Fingerprint 浏览器。
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
	"wait_result_seconds":     "240",
	"task_switching":          "1",
	"task_switching_count":    "50",
	"start_browser_index":     "1",
	"prompts":                 "",
	"prompt_suffix":           "",
	"use_suffix":              "0",
	"output_path":             "",
	"gen_auto_number":         "0",
	"wash_auto_number":        "0",
	"number_prefix":           "",
	"number_start":            "1",
	"number_width":            "3",
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
	"browser_backend":         EngineFingerprint,
	"auto_check_updates":      "1",
	"update_mirror":           "",
	"update_skipped_version":  "",
	"proxy_mode":              "system",
	"proxy_server":            "",
	"daily_cap":               "0",
	"daily_cap_qianwen":       "100",
	"daily_cap_qwen_intl":     "100",
	"daily_cap_gemini":        "100",
	"risky_words":             "血腥,暴力,裸体,色情,性感,裸露,枪支,武器,自杀,毒品",
	"dot_matrix_effect":       "grain",
	"auto_delete_history":     "0",
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
);
CREATE TABLE IF NOT EXISTS sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	page TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	prompts TEXT NOT NULL DEFAULT '',
	images TEXT NOT NULL DEFAULT '',
	progress REAL NOT NULL DEFAULT 0,
	prog_text TEXT NOT NULL DEFAULT '',
	active INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_sessions_page ON sessions(page, active, updated_at);
CREATE TABLE IF NOT EXISTS session_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id INTEGER NOT NULL,
	ord INTEGER NOT NULL,
	prompt TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT '',
	platform TEXT NOT NULL DEFAULT '',
	account_id INTEGER NOT NULL DEFAULT 0,
	account_name TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	outputs TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	at_unix INTEGER NOT NULL DEFAULT 0,
	elapsed_ms INTEGER NOT NULL DEFAULT 0,
	undispatched INTEGER NOT NULL DEFAULT 0,
	quality_note TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_session_tasks_session ON session_tasks(session_id, ord);
CREATE TABLE IF NOT EXISTS session_sources (
	session_id INTEGER NOT NULL,
	path TEXT NOT NULL,
	washed INTEGER NOT NULL DEFAULT 0,
	output TEXT NOT NULL DEFAULT '',
	updated_at INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(session_id, path)
);`

// Open 打开（或创建）配置库。
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// Check an existing database read-only before migrations or default inserts.
	// In particular, a damaged settings index can return another key's value.
	if err := checkIntegrity(path); err != nil {
		return nil, err
	}
	// Reserve the writer before a transaction reads state it will later update.
	// A deferred read-to-write upgrade can fail with SQLITE_BUSY_SNAPSHOT even
	// when busy_timeout is set and another Store instance has already committed.
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	// Store operations are serialized by mu; one connection avoids unrelated
	// connections racing through initialization or WAL checkpoint transitions.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化表结构: %w", err)
	}
	if err := migrateBrowsers(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateSessions(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := rewriteLegacyNames(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := initSettings(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

func checkIntegrity(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("检查配置数据库: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("配置数据库完整性校验失败: 文件为空")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("检查配置数据库: %w", err)
	}
	filePath := filepath.ToSlash(abs)
	if filepath.VolumeName(abs) != "" && !strings.HasPrefix(filePath, "/") {
		filePath = "/" + filePath
	}
	u := url.URL{Scheme: "file", Path: filePath, RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	probe, err := sql.Open("sqlite", u.String())
	if err != nil {
		return fmt.Errorf("检查配置数据库: %w", err)
	}
	defer probe.Close()
	var result string
	if err := probe.QueryRow(`PRAGMA quick_check(1)`).Scan(&result); err != nil {
		return fmt.Errorf("配置数据库完整性校验失败: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("配置数据库完整性校验失败: %s", result)
	}
	// quick_check skips index-to-table consistency. The settings index is used
	// for every option lookup, so check that table and its indexes fully.
	var hasSettings int
	if err := probe.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='settings'`).Scan(&hasSettings); err != nil {
		return fmt.Errorf("检查配置数据库: %w", err)
	}
	if hasSettings != 0 {
		if err := probe.QueryRow(`PRAGMA integrity_check('settings')`).Scan(&result); err != nil {
			return fmt.Errorf("配置数据库设置表校验失败: %w", err)
		}
		if result != "ok" {
			return fmt.Errorf("配置数据库设置表校验失败: %s", result)
		}
		rows, err := probe.Query(`SELECT key,value FROM settings WHERE key IN
			('daily_cap','daily_cap_qianwen','daily_cap_qwen_intl','daily_cap_gemini')`)
		if err != nil {
			return fmt.Errorf("检查每日上限: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			var value sql.NullString
			if err := rows.Scan(&key, &value); err != nil {
				return fmt.Errorf("检查每日上限: %w", err)
			}
			// Empty legacy provider values mean “inherit the previous value”.
			if value.Valid && strings.TrimSpace(value.String) != "" {
				if _, err := strconv.Atoi(strings.TrimSpace(value.String)); err != nil {
					return fmt.Errorf("配置数据库每日上限无效: %s", key)
				}
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("检查每日上限: %w", err)
		}
	}
	return nil
}

func initSettings(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("初始化设置: %w", err)
	}
	defer tx.Rollback()
	// A legacy daily_cap applied to every platform. Materialize it for other
	// providers before using the new Doubao-only default on a fresh database.
	var legacy string
	err = tx.QueryRow(`SELECT value FROM settings WHERE key='daily_cap'`).Scan(&legacy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("读取旧每日上限: %w", err)
	}
	hasLegacy := err == nil
	for k, v := range defaults {
		if hasLegacy && k == "daily_cap_qianwen" {
			v = legacy
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO settings(key, value) VALUES(?, ?)`, k, v); err != nil {
			return fmt.Errorf("初始化设置 %s: %w", k, err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM settings WHERE key IN ('browserBackend', 'extBridgePort', 'extBridgeToken')`); err != nil {
		return fmt.Errorf("清理旧扩展桥设置: %w", err)
	}
	if _, err := tx.Exec(`UPDATE settings SET value=? WHERE key='browser_backend'`, EngineFingerprint); err != nil {
		return fmt.Errorf("统一浏览器引擎设置: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("保存初始设置: %w", err)
	}
	return nil
}

// rewriteLegacyNames 把旧的默认名「浏览器3」改成「千问1」。只改这一次数据，之后不再认旧名字。
func rewriteLegacyNames(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, platform, name FROM browsers ORDER BY id`)
	if err != nil {
		return err
	}
	type row struct {
		id       int64
		platform string
		name     string
	}
	var all []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.platform, &item.name); err != nil {
			rows.Close()
			return err
		}
		all = append(all, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var used []string
	var legacy []row
	for _, item := range all {
		if legacyDefaultName(item.name) {
			legacy = append(legacy, item)
			continue
		}
		used = append(used, item.name)
	}
	for _, item := range legacy {
		name := nextPlatformName(item.platform, used)
		if _, err := db.Exec(`UPDATE browsers SET name=? WHERE id=?`, name, item.id); err != nil {
			return err
		}
		used = append(used, name)
	}
	return nil
}

func legacyDefaultName(name string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(name), "浏览器")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func nextPlatformName(platform string, used []string) string {
	prefix := strings.TrimSpace(platform)
	if prefix == "" {
		prefix = "账号"
	}
	taken := map[int]bool{}
	for _, name := range used {
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		n := 0
		for _, r := range name[len(prefix):] {
			if r < '0' || r > '9' {
				n = 0
				break
			}
			n = n*10 + int(r-'0')
		}
		if n > 0 {
			taken[n] = true
		}
	}
	for i := 1; i < 10000; i++ {
		if !taken[i] {
			return prefix + strconv.Itoa(i)
		}
	}
	return prefix
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// File 是配置库文件路径。
func (s *Store) File() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

// Get 读取设置（缺失时回默认值）。
func (s *Store) Get(key string) string {
	s.mu.Lock()
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	s.mu.Unlock()
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logging.Log.Errorf("读取设置 %s 失败: %v", key, err)
		}
		return defaults[key]
	}
	return v
}

// Set 写入设置。保留旧接口；失败会写入应用日志。
func (s *Store) Set(key, value string) { _ = s.SetChecked(key, value) }

// SetChecked 写入设置并向需要确认落盘的调用者返回错误。
func (s *Store) SetChecked(key, value string) error {
	s.mu.Lock()
	_, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	s.mu.Unlock()
	if err != nil {
		logging.Log.Errorf("保存设置 %s 失败: %v", key, err)
	}
	return err
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
	out, err := s.browsersLocked()
	s.mu.Unlock()
	if err != nil {
		logging.Log.Errorf("读取账号列表失败: %v", err)
	}
	return out
}

func (s *Store) browsersLocked() ([]Browser, error) {
	now := time.Now()
	rows, err := s.db.Query(`SELECT id, platform, name, port, COALESCE(user_data_dir,''), is_running, is_logged_in,
		COALESCE(health,'ready'), COALESCE(cool_until,0), COALESCE(last_used,0), COALESCE(last_error,''),
		COALESCE(day_key,''), COALESCE(day_count,0), COALESCE(day_success,0), COALESCE(day_failed,0), COALESCE(proxy,''), COALESCE(disabled,0), COALESCE(browser_engine,''),
		COALESCE(credits_remain,-1), COALESCE(credits_total,0), COALESCE(credit_cost,0), COALESCE(credits_reset_at,0),
		COALESCE(credits_cycle,''), COALESCE(member_tier,''), COALESCE(member_name,''), COALESCE(account_concurrency,0),
		COALESCE(watermark_free,0), COALESCE(usage_percent,-1), COALESCE(platform_limited,0), COALESCE(limit_tips,''), COALESCE(reset_card_count,0),
		COALESCE(credits_synced_at,0)
		FROM browsers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []Browser
	var recovered []int64
	for rows.Next() {
		var b Browser
		var run, login, disabled, watermarkFree, platformLimited int
		if err := rows.Scan(
			&b.ID, &b.Platform, &b.Name, &b.Port, &b.UserDataDir, &run, &login, &b.Health, &b.CoolUntil, &b.LastUsed, &b.LastError,
			&b.DayKey, &b.DayCount, &b.DaySuccess, &b.DayFailed, &b.Proxy, &disabled, &b.Engine,
			&b.CreditsRemain, &b.CreditsTotal, &b.CreditCost, &b.CreditsResetAt,
			&b.CreditsCycle, &b.MemberTier, &b.MemberName, &b.AccountConcurrency,
			&watermarkFree, &b.UsagePercent, &platformLimited, &b.LimitTips, &b.ResetCardCount,
			&b.CreditsSyncedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		b.IsRunning = run != 0
		b.IsLoggedIn = login != 0
		b.Disabled = disabled != 0
		b.WatermarkFree = watermarkFree != 0
		b.PlatformLimited = platformLimited != 0
		if b.CreditsSyncedAt <= 0 {
			b.CreditsRemain = -1
			b.UsagePercent = -1
		}
		// 换日之后，昨天的额度用尽已经到期。读的时候收成就绪，不让旧的错误提示一直挂着。
		if fixed, ok := b.Recover(now); ok {
			b = fixed
			recovered = append(recovered, b.ID)
		}
		out = append(out, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// 恢复也写回库里，下次读和别的界面看到的是同一份状态。
	s.saveRecoveredLocked(recovered, now.Unix())
	return out, nil
}

// saveRecoveredLocked 把到期的冷却和额度挂起写回就绪。
// 只清恢复时间已经过去的行，读的间隙里新记下的故障不会被一起抹掉。
func (s *Store) saveRecoveredLocked(ids []int64, now int64) {
	for _, id := range ids {
		if _, err := s.db.Exec(`UPDATE browsers SET health=?, cool_until=0, last_error=''
			WHERE id=? AND health IN (?, ?) AND cool_until > 0 AND cool_until <= ?`,
			HealthReady, id, HealthCooling, HealthExhausted, now); err != nil {
			logging.Log.Errorf("保存账号 %d 的恢复状态失败: %v", id, err)
			return
		}
	}
}

// SetBrowserDisabled 只更改账号是否参与任务，不改登录或健康状态。
func (s *Store) SetBrowserDisabled(id int64, disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE browsers SET disabled=? WHERE id=?`, disabled, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("账号不存在: %d", id)
	}
	return nil
}

// IsBrowserDisabled 单行查询账号是否处于禁用状态，避免热路径全表扫描。
func (s *Store) IsBrowserDisabled(id int64) bool {
	if s == nil || id <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var disabled int
	if err := s.db.QueryRow(`SELECT COALESCE(disabled,0) FROM browsers WHERE id=?`, id).Scan(&disabled); err != nil {
		return false
	}
	return disabled != 0
}

// Usable 只判断账号健康状态与已同步额度；冷却或额度到期后自动恢复。
func (b Browser) Usable(now time.Time) bool {
	if b.QuotaExhausted(now) {
		return false
	}
	switch b.Health {
	case "", HealthReady:
		return true
	case HealthCooling, HealthExhausted:
		return b.CoolUntil > 0 && !now.Before(time.Unix(b.CoolUntil, 0))
	default:
		return true
	}
}

// Expired 表示冷却或额度用尽已经过了恢复时间。这种挂起是过去的事，不是现在还在挡着账号。
// 恢复时间为 0 的挂起（例如未登录）要等用户处理，不会自己到期。
func (b Browser) Expired(now time.Time) bool {
	switch b.Health {
	case HealthCooling, HealthExhausted:
		return b.CoolUntil > 0 && !now.Before(time.Unix(b.CoolUntil, 0))
	default:
		return false
	}
}

// Recover 把已经到期的冷却或额度挂起收成就绪：状态回到可调度，额度错误提示一起清掉。
// 第二个返回值表示有没有改动。
func (b Browser) Recover(now time.Time) (Browser, bool) {
	if !b.Expired(now) {
		return b, false
	}
	b.Health, b.CoolUntil, b.LastError = HealthReady, 0, ""
	return b, true
}

// AddBrowser 新增浏览器（端口从 9333 起自动分配）。
func (s *Store) AddBrowser(platform, name, dataDir string) (Browser, error) {
	return s.AddManagedBrowser(platform, name, dataDir, "")
}

func (s *Store) AddManagedBrowser(platform, name, dataDir, engine string) (Browser, error) {
	if engine != "" && (engine != EngineFingerprint || (platform != "千问" && platform != "千问国际" && platform != "豆包" && platform != "Gemini")) {
		return Browser{}, fmt.Errorf("服务商 %s 不支持浏览器 %s", platform, engine)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Browser{}, err
	}
	defer tx.Rollback()
	b, err := addBrowserTx(tx, platform, name, dataDir, false, engine)
	if err != nil {
		return Browser{}, err
	}
	if err := tx.Commit(); err != nil {
		return Browser{}, err
	}
	return b, nil
}

// addBrowserTx allocates the port and inserts the account under one writer lock.
func addBrowserTx(tx *sql.Tx, platform, name, dataDir string, loggedIn bool, engine string) (Browser, error) {
	var maxPort int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(port), 9332) FROM browsers`).Scan(&maxPort); err != nil {
		return Browser{}, err
	}
	port := maxPort + 1
	if port < 9333 {
		port = 9333
	}
	login := 0
	if loggedIn {
		login = 1
	}
	res, err := tx.Exec(`INSERT INTO browsers(platform, name, port, user_data_dir, is_running, is_logged_in, browser_engine) VALUES(?,?,?,?,?,?,?)`,
		platform, name, port, dataDir, login, login, engine)
	if err != nil {
		return Browser{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Browser{}, err
	}
	return Browser{
		ID: id, Platform: platform, Name: name, Port: port, UserDataDir: dataDir, Engine: engine,
		IsRunning: loggedIn, IsLoggedIn: loggedIn, Health: HealthReady, CreditsRemain: -1, UsagePercent: -1,
	}, nil
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
	r, l := 0, 0
	if running {
		r = 1
	}
	if loggedIn {
		l = 1
	}
	_, err := s.db.Exec(`UPDATE browsers SET is_running=?, is_logged_in=? WHERE id=?`, r, l, id)
	s.mu.Unlock()
	if err != nil {
		logging.Log.Errorf("保存账号 %d 的登录状态失败: %v", id, err)
	}
}

// SetRunning 只改运行标记，不动登录标记。启动和停止不能把刚确认的登录写回去。
func (s *Store) SetRunning(id int64, running bool) {
	if s == nil || id <= 0 {
		return
	}
	s.mu.Lock()
	r := 0
	if running {
		r = 1
	}
	_, err := s.db.Exec(`UPDATE browsers SET is_running=? WHERE id=?`, r, id)
	s.mu.Unlock()
	if err != nil {
		logging.Log.Errorf("保存账号 %d 的运行状态失败: %v", id, err)
	}
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

// DropUnloggedLocal 只删除 confirmedOut 里、且库里仍是未登录的本机账号。
// 不在列表里的行留下：is_logged_in=0 本身不能当退出。
func (s *Store) DropUnloggedLocal(confirmedOut []int64) []Browser {
	if s == nil {
		return nil
	}
	allow := map[int64]struct{}{}
	for _, id := range confirmedOut {
		if id > 0 {
			allow[id] = struct{}{}
		}
	}
	var candidates []Browser
	for _, b := range s.Browsers() {
		if b.IsLoggedIn {
			continue
		}
		if _, ok := allow[b.ID]; !ok {
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
		res, err := s.db.Exec(`DELETE FROM browsers WHERE id=? AND is_logged_in=0`, b.ID)
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

// UpdateBrowserHealth 记下额度、限流、验证码或登录失败。coolUntil 为 0 时不会自动恢复。
func (s *Store) UpdateBrowserHealth(id int64, health string, coolUntil int64, lastError string) {
	if s == nil || id <= 0 {
		return
	}
	s.mu.Lock()
	_, err := s.db.Exec(`UPDATE browsers SET health=?, cool_until=?, last_error=? WHERE id=?`, health, coolUntil, lastError, id)
	s.mu.Unlock()
	if err != nil {
		logging.Log.Errorf("保存账号 %d 的健康状态失败: %v", id, err)
	}
}

// ResetBrowserHealth 把账号标回可使用。用户重新登录或确认额度恢复后点这一下。
func (s *Store) ResetBrowserHealth(id int64) {
	if s == nil || id <= 0 {
		return
	}
	s.mu.Lock()
	_, err := s.db.Exec(`UPDATE browsers SET health=?, cool_until=0, last_error='', platform_limited=0, limit_tips='' WHERE id=?`, HealthReady, id)
	s.mu.Unlock()
	if err != nil {
		logging.Log.Errorf("恢复账号 %d 的健康状态失败: %v", id, err)
	}
}

// UsedToday 是这个账号在 now 这一天已经派出的次数。换日之后是 0。
func (b Browser) UsedToday(now time.Time) int {
	if b.DayKey != now.Format("2006-01-02") || b.DayCount < 0 {
		return 0
	}
	return b.DayCount
}

// ResultsToday 返回账号当天已完成任务的成功和失败数。
func (b Browser) ResultsToday(now time.Time) (int, int) {
	if b.DayKey != now.Format("2006-01-02") {
		return 0, 0
	}
	return max(b.DaySuccess, 0), max(b.DayFailed, 0)
}

// AddUse 给这个账号的当天次数加一。日期不同就从 1 重新计。
func (s *Store) AddUse(id int64, now time.Time) {
	if s == nil || id <= 0 {
		return
	}
	_ = s.RecordUse(id, now, 1, 0, 0)
}

// RecordUse 同时保存用量和已完成任务结果，换日时一起归零。
func (s *Store) RecordUse(id int64, now time.Time, used, success, failed int) error {
	key := now.Format("2006-01-02")
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE browsers SET
		day_count = CASE WHEN day_key = ? THEN COALESCE(day_count,0) ELSE 0 END + ?,
		day_success = CASE WHEN day_key = ? THEN COALESCE(day_success,0) ELSE 0 END + ?,
		day_failed = CASE WHEN day_key = ? THEN COALESCE(day_failed,0) ELSE 0 END + ?,
		day_key = ? WHERE id = ?`, key, used, key, success, key, failed, key, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fmt.Errorf("账号不存在: %d", id)
	}
	return nil
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

func quotaHold(health, last string) bool {
	if health != HealthExhausted && health != HealthCooling {
		return false
	}
	if loginHold(health, last) {
		return false
	}
	lower := strings.ToLower(last)
	if strings.Contains(last, "积分") || strings.Contains(last, "额度") || strings.Contains(last, "上限") ||
		strings.Contains(last, "限额") || strings.Contains(lower, "quota") || strings.Contains(lower, "limit") {
		return true
	}
	if health == HealthCooling {
		return strings.Contains(last, "限流") || strings.Contains(last, "频繁") || strings.Contains(last, "过多") ||
			strings.Contains(lower, "rate")
	}
	return false
}

// UpdateBrowserQuota 持久化从平台网页实时同步回来的积分、刷新时间、会员等级与并发上限。
// 若账号先前因额度/积分不足处于 exhausted 且最新剩余积分 > 0，自动恢复为就绪。
func (s *Store) UpdateBrowserQuota(id int64, q AccountQuota) error {
	if s == nil || id <= 0 {
		return nil
	}
	if q.SyncedAt <= 0 {
		q.SyncedAt = time.Now().Unix()
	}
	wf := 0
	if q.WatermarkFree {
		wf = 1
	}
	limited := 0
	if q.PlatformLimited {
		limited = 1
	}
	usagePct := q.UsagePercent
	if usagePct < -1 {
		usagePct = -1
	}
	if usagePct > 100 {
		usagePct = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var health, last string
	if err := s.db.QueryRow(`SELECT COALESCE(health,'ready'), COALESCE(last_error,'') FROM browsers WHERE id=?`, id).Scan(&health, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("账号不存在: %d", id)
		}
		return err
	}
	shouldRecover := quotaHold(health, last) && ((q.CreditsRemain > 0) || (q.CreditsRemain < 0 && !q.PlatformLimited && usagePct >= 0 && usagePct < 100))
	quotaActiveExhausted := (q.CreditsRemain == 0 || q.PlatformLimited) && (q.CreditsResetAt <= 0 || q.CreditsResetAt > q.SyncedAt)
	if shouldRecover {
		_, err := s.db.Exec(`UPDATE browsers SET
			credits_remain=?, credits_total=?, credit_cost=?, credits_reset_at=?,
			credits_cycle=?, member_tier=?, member_name=?, account_concurrency=?,
			watermark_free=?, usage_percent=?, platform_limited=?, limit_tips=?, reset_card_count=?,
			credits_synced_at=?,
			health=?, cool_until=0, last_error=''
			WHERE id=?`,
			q.CreditsRemain, max(q.CreditsTotal, 0), max(q.CreditCost, 0), max(q.CreditsResetAt, int64(0)),
			strings.TrimSpace(q.CreditsCycle), strings.TrimSpace(q.MemberTier), strings.TrimSpace(q.MemberName), q.AccountConcurrency,
			wf, usagePct, limited, strings.TrimSpace(q.LimitTips), max(q.ResetCardCount, 0),
			q.SyncedAt, HealthReady, id)
		return err
	}
	if quotaActiveExhausted && !loginHold(health, last) && (health == "" || health == HealthReady || quotaHold(health, last)) {
		nextHealth := HealthExhausted
		if q.PlatformLimited && q.CreditsRemain < 0 {
			nextHealth = HealthCooling
		}
		_, err := s.db.Exec(`UPDATE browsers SET
			credits_remain=?, credits_total=?, credit_cost=?, credits_reset_at=?,
			credits_cycle=?, member_tier=?, member_name=?, account_concurrency=?,
			watermark_free=?, usage_percent=?, platform_limited=?, limit_tips=?, reset_card_count=?,
			credits_synced_at=?,
			health=?, cool_until=?
			WHERE id=?`,
			q.CreditsRemain, max(q.CreditsTotal, 0), max(q.CreditCost, 0), max(q.CreditsResetAt, int64(0)),
			strings.TrimSpace(q.CreditsCycle), strings.TrimSpace(q.MemberTier), strings.TrimSpace(q.MemberName), q.AccountConcurrency,
			wf, usagePct, limited, strings.TrimSpace(q.LimitTips), max(q.ResetCardCount, 0),
			q.SyncedAt, nextHealth, max(q.CreditsResetAt, int64(0)), id)
		return err
	}
	_, err := s.db.Exec(`UPDATE browsers SET
		credits_remain=?, credits_total=?, credit_cost=?, credits_reset_at=?,
		credits_cycle=?, member_tier=?, member_name=?, account_concurrency=?,
		watermark_free=?, usage_percent=?, platform_limited=?, limit_tips=?, reset_card_count=?,
		credits_synced_at=?
		WHERE id=?`,
		q.CreditsRemain, max(q.CreditsTotal, 0), max(q.CreditCost, 0), max(q.CreditsResetAt, int64(0)),
		strings.TrimSpace(q.CreditsCycle), strings.TrimSpace(q.MemberTier), strings.TrimSpace(q.MemberName), q.AccountConcurrency,
		wf, usagePct, limited, strings.TrimSpace(q.LimitTips), max(q.ResetCardCount, 0),
		q.SyncedAt, id)
	return err
}

// MarkBrowserCreditsExhausted 在运行期明确触发积分不足时将已知剩余积分记为 0，
// 保留先前同步的总额度、刷新时间与会员信息。
func (s *Store) MarkBrowserCreditsExhausted(id int64, now time.Time) {
	if s == nil || id <= 0 {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(`UPDATE browsers SET credits_remain=0, platform_limited=1, credits_synced_at=? WHERE id=?`, now.Unix(), id)
}

// MarkBrowserPlatformLimited 在运行期明确触发限额或频控（如豆包免费版限额）时标记受限状态，
// 不覆盖已有订阅余额，保留套餐与重置卡信息。
func (s *Store) MarkBrowserPlatformLimited(id int64, tips string, resetAt int64, now time.Time) {
	if s == nil || id <= 0 {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if resetAt > 0 {
		_, _ = s.db.Exec(`UPDATE browsers SET platform_limited=1, usage_percent=100, limit_tips=?, credits_reset_at=?, credits_synced_at=? WHERE id=?`,
			strings.TrimSpace(tips), resetAt, now.Unix(), id)
		return
	}
	_, _ = s.db.Exec(`UPDATE browsers SET platform_limited=1, usage_percent=100, limit_tips=?, credits_synced_at=? WHERE id=?`,
		strings.TrimSpace(tips), now.Unix(), id)
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
		`ALTER TABLE browsers ADD COLUMN day_success INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN day_failed INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN proxy TEXT DEFAULT ''`,
		`ALTER TABLE browsers ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN browser_engine TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE browsers ADD COLUMN credits_remain INTEGER NOT NULL DEFAULT -1`,
		`ALTER TABLE browsers ADD COLUMN credits_total INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN credit_cost INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN credits_reset_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN credits_cycle TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE browsers ADD COLUMN member_tier TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE browsers ADD COLUMN member_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE browsers ADD COLUMN account_concurrency INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN watermark_free INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN usage_percent INTEGER NOT NULL DEFAULT -1`,
		`ALTER TABLE browsers ADD COLUMN platform_limited INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN limit_tips TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE browsers ADD COLUMN reset_card_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE browsers ADD COLUMN credits_synced_at INTEGER NOT NULL DEFAULT 0`,
	}
	for _, stmt := range alters {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("升级 browsers 表: %w", err)
		}
	}
	if _, err := db.Exec(`DELETE FROM browsers WHERE COALESCE(ext_account,'') <> ''`); err != nil {
		return fmt.Errorf("清理旧扩展账号: %w", err)
	}
	// 已经成功同步过实时额度/套餐的账号说明登录态有效，修复早期同步未回写 is_logged_in 的历史行。
	if _, err := db.Exec(`UPDATE browsers SET is_logged_in=1 WHERE is_logged_in=0 AND credits_synced_at > 0 AND NOT (COALESCE(health,'')='exhausted' AND COALESCE(last_error,'') LIKE '%登录%')`); err != nil {
		return fmt.Errorf("修复已同步账号登录态: %w", err)
	}
	return nil
}

func migrateSessions(db *sql.DB) error {
	alters := []string{
		`ALTER TABLE session_tasks ADD COLUMN elapsed_ms INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE session_tasks ADD COLUMN undispatched INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE session_tasks ADD COLUMN quality_note TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range alters {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("升级 session_tasks 表: %w", err)
		}
	}
	return nil
}
