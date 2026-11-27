package store

import "fmt"

// SetBrowserProxy updates the per-account proxy for a managed Doubao/Qianwen/QwenIntl/Gemini/Jimeng account.
func (s *Store) SetBrowserProxy(id int64, proxy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE browsers SET proxy=? WHERE id=? AND platform IN ('豆包','千问','千问国际','Gemini','即梦')`, proxy, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("没有可设置代理的受管账号")
	}
	return nil
}
