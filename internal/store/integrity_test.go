package store

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRejectsExistingEmptyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("an existing empty database was initialized without warning")
	} else if !strings.Contains(err.Error(), "文件为空") {
		t.Fatalf("unexpected error: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Fatalf("empty database was changed: info=%v err=%v", info, err)
	}
}

func TestOpenRejectsNonNumericDailyCapWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid-cap.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO settings(key,value) VALUES('daily_cap','豆包')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(data)
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("non-numeric cap was silently treated as unlimited")
	} else if !strings.Contains(err.Error(), "每日上限无效") {
		t.Fatalf("unexpected error: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != before {
		t.Fatal("validating a bad daily cap changed the database")
	}
}

func TestOpenRejectsDamagedSettingsIndexWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "damaged.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO settings(key,value) VALUES('daily_cap','17'),('output_path','somewhere')`); err != nil {
		t.Fatal(err)
	}
	var rootPage, pageSize int
	if err := db.QueryRow(`SELECT rootpage FROM sqlite_master WHERE name='sqlite_autoindex_settings_1'`).Scan(&rootPage); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	offset := (rootPage - 1) * pageSize
	if offset < 100 || offset >= len(data) {
		t.Fatalf("invalid index page: root=%d size=%d file=%d", rootPage, pageSize, len(data))
	}
	data[offset] = 0 // invalid SQLite b-tree page type
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(data)
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("opening a damaged settings index succeeded")
	} else if !strings.Contains(err.Error(), "完整性校验失败") {
		t.Fatalf("unexpected error: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != before {
		t.Fatal("checking a damaged database changed its contents")
	}
}

func TestOpenRejectsMismatchedSettingsIndexEvenWhenQuickCheckPasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mismatched.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO settings(key,value) VALUES('daily_cap','17'),('output_path','somewhere')`); err != nil {
		t.Fatal(err)
	}
	var rootPage, pageSize int
	if err := db.QueryRow(`SELECT rootpage FROM sqlite_master WHERE name='sqlite_autoindex_settings_1'`).Scan(&rootPage); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	start := (rootPage - 1) * pageSize
	if start < 100 || start+pageSize > len(data) {
		t.Fatalf("invalid index page: root=%d size=%d file=%d", rootPage, pageSize, len(data))
	}
	key := []byte("daily_cap")
	pos := bytes.Index(data[start:start+pageSize], key)
	if pos < 0 {
		t.Fatal("test index key not found")
	}
	data[start+pos+len(key)-1] = 'q' // same-length key; index structure remains valid
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(data)
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var quick string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&quick); err != nil || quick != "ok" {
		t.Fatalf("fixture should pass quick_check: result=%q err=%v", quick, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("opening an inconsistent settings index succeeded")
	} else if !strings.Contains(err.Error(), "设置表校验失败") {
		t.Fatalf("unexpected error: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != before {
		t.Fatal("checking an inconsistent settings index changed the database")
	}
}

func TestLegacyCapMigrationPreservesIndependentProviderSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO settings(key,value) VALUES('daily_cap','15'),('daily_cap_qianwen','4')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"daily_cap": "15", "daily_cap_qianwen": "4", "daily_cap_agy": "15", "daily_cap_chatgpt": "15",
	} {
		if got := s.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	s.SetInt("daily_cap", 0)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.GetInt("daily_cap") != 0 || s.GetInt("daily_cap_qianwen") != 4 || s.GetInt("daily_cap_agy") != 15 {
		t.Fatal("changing Doubao's cap changed another platform or was not persisted")
	}
}
