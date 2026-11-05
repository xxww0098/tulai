package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
)

// PrepareCamoufoxProfiles preserves the old directories and account IDs. Firefox
// needs its own profile and a fresh login; the one-time migration is transactional.
func (s *Store) PrepareCamoufoxProfiles(dataRoot string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var done string
	err = tx.QueryRow(`SELECT value FROM settings WHERE key='camoufox_profiles_v1'`).Scan(&done)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if done == "1" {
		return nil
	}
	rows, err := tx.Query(`SELECT id FROM browsers WHERE COALESCE(ext_account,'')='' AND platform IN ('豆包','千问')`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	root, err := filepath.Abs(dataRoot)
	if err != nil {
		return err
	}
	for _, id := range ids {
		dir := filepath.Join(root, "Camoufox", "account-"+strconv.FormatInt(id, 10))
		if _, err := tx.Exec(`UPDATE browsers SET legacy_user_data_dir=COALESCE(user_data_dir,''), user_data_dir=?, is_running=0, is_logged_in=0, last_error='切换 Camoufox 后请重新登录' WHERE id=?`, dir, id); err != nil {
			return err
		}
	}
	for key, value := range map[string]string{"camoufox_profiles_v1": "1", "browser_backend": "Camoufox", "browserBackend": "camoufox_extension"} {
		if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetBrowserProxy(id int64, proxy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE browsers SET proxy=? WHERE id=? AND COALESCE(ext_account,'')='' AND platform IN ('豆包','千问','ChatGPT')`, proxy, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("没有可设置代理的 Camoufox 账号")
	}
	return nil
}
