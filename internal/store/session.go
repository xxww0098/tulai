package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	// PageGen 是生图页的会话。
	PageGen = "gen"
	// PageWash 是洗图页的会话。
	PageWash = "wash"
)

const historyLimit = 100

// Session 是某一页的一份工作区：提示词、参考图或源图、进度。
type Session struct {
	ID       int64
	Page     string
	Created  int64
	Updated  int64
	Prompts  string
	Images   string
	Progress float64
	ProgText string
	Active   bool
	Tasks    int
}

// SessionTask 是会话里的一条任务。Outputs 用换行分隔结果路径。
type SessionTask struct {
	ID          int64
	SessionID   int64
	Ord         int
	Prompt      string
	Source      string
	Platform    string
	AccountID   int64
	AccountName string
	Status      string
	Outputs     string
	Error       string
	At          int64
}

// SaveActive 写入这一页的当前会话并把它标成活动。同页其他会话收起。
// sess.ID 为 0 或已经不存在时新建一行。
func (s *Store) SaveActive(sess Session, items []SessionTask) (int64, error) {
	if s == nil {
		return 0, fmt.Errorf("配置库未打开")
	}
	if sess.Page != PageGen && sess.Page != PageWash {
		return 0, fmt.Errorf("任务页无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("保存任务记录失败: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	id := sess.ID
	if id > 0 {
		var one int
		err := tx.QueryRow(`SELECT 1 FROM sessions WHERE id=?`, id).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			id = 0
		} else if err != nil {
			return 0, fmt.Errorf("保存任务记录失败: %w", err)
		}
	}
	if id > 0 {
		_, err = tx.Exec(`UPDATE sessions SET page=?, updated_at=?, prompts=?, images=?, progress=?, prog_text=?, active=1 WHERE id=?`,
			sess.Page, now, sess.Prompts, sess.Images, sess.Progress, sess.ProgText, id)
	} else {
		var res sql.Result
		res, err = tx.Exec(`INSERT INTO sessions(page, created_at, updated_at, prompts, images, progress, prog_text, active)
			VALUES(?,?,?,?,?,?,?,1)`, sess.Page, now, now, sess.Prompts, sess.Images, sess.Progress, sess.ProgText)
		if err == nil {
			id, err = res.LastInsertId()
		}
	}
	if err != nil {
		return 0, fmt.Errorf("保存任务记录失败: %w", err)
	}
	if _, err = tx.Exec(`UPDATE sessions SET active=0 WHERE page=? AND id<>?`, sess.Page, id); err != nil {
		return 0, fmt.Errorf("保存任务记录失败: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM session_tasks WHERE session_id=?`, id); err != nil {
		return 0, fmt.Errorf("保存任务记录失败: %w", err)
	}
	for i, item := range items {
		ord := item.Ord
		if ord <= 0 {
			ord = i + 1
		}
		if _, err = tx.Exec(`INSERT INTO session_tasks(session_id, ord, prompt, source, platform, account_id, account_name, status, outputs, error, at_unix)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			id, ord, item.Prompt, item.Source, item.Platform, item.AccountID, item.AccountName, item.Status, item.Outputs, item.Error, item.At); err != nil {
			return 0, fmt.Errorf("保存任务记录失败: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("保存任务记录失败: %w", err)
	}
	return id, nil
}

// ActiveSession 读取这一页正在用的会话。没有时 ok 为 false，不是错误。
func (s *Store) ActiveSession(page string) (Session, []SessionTask, bool, error) {
	if s == nil {
		return Session{}, nil, false, fmt.Errorf("配置库未打开")
	}
	if page != PageGen && page != PageWash {
		return Session{}, nil, false, fmt.Errorf("任务页无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := scanOne(s.db, `SELECT id, page, created_at, updated_at, prompts, images, progress, prog_text, active
		FROM sessions WHERE page=? AND active=1 ORDER BY updated_at DESC LIMIT 1`, page)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, nil, false, nil
	}
	if err != nil {
		return Session{}, nil, false, fmt.Errorf("读取任务记录失败: %w", err)
	}
	items, err := loadTasks(s.db, sess.ID)
	if err != nil {
		return Session{}, nil, false, err
	}
	sess.Tasks = len(items)
	return sess, items, true, nil
}

// History 是这一页已经收起的会话，新的在前，最多 100 条。不含任务正文。
func (s *Store) History(page string) ([]Session, error) {
	if s == nil {
		return nil, fmt.Errorf("配置库未打开")
	}
	if page != PageGen && page != PageWash {
		return nil, fmt.Errorf("任务页无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, page, created_at, updated_at, prompts, images, progress, prog_text, active,
		(SELECT COUNT(*) FROM session_tasks t WHERE t.session_id = sessions.id)
		FROM sessions WHERE page=? AND active=0 ORDER BY updated_at DESC LIMIT ?`, page, historyLimit)
	if err != nil {
		return nil, fmt.Errorf("读取任务记录失败: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		sess, n, err := scanCounted(rows)
		if err != nil {
			return nil, fmt.Errorf("读取任务记录失败: %w", err)
		}
		sess.Tasks = n
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取任务记录失败: %w", err)
	}
	return out, nil
}

// Session 按 id 读取会话和任务。
func (s *Store) Session(id int64) (Session, []SessionTask, error) {
	if s == nil {
		return Session{}, nil, fmt.Errorf("配置库未打开")
	}
	if id <= 0 {
		return Session{}, nil, fmt.Errorf("任务记录不存在")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := scanOne(s.db, `SELECT id, page, created_at, updated_at, prompts, images, progress, prog_text, active
		FROM sessions WHERE id=?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, nil, fmt.Errorf("任务记录不存在")
	}
	if err != nil {
		return Session{}, nil, fmt.Errorf("读取任务记录失败: %w", err)
	}
	items, err := loadTasks(s.db, sess.ID)
	if err != nil {
		return Session{}, nil, err
	}
	sess.Tasks = len(items)
	return sess, items, nil
}

// ArchiveSession 收起一条会话，只改记录，不删图片。
func (s *Store) ArchiveSession(id int64) error {
	if s == nil {
		return fmt.Errorf("配置库未打开")
	}
	if id <= 0 {
		return fmt.Errorf("任务记录不存在")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := mustSession(s.db, id); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE sessions SET active=0, updated_at=? WHERE id=?`, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("收起当前任务失败: %w", err)
	}
	return nil
}

// ActivateSession 把一条会话重新标成这一页的当前会话。
func (s *Store) ActivateSession(id int64) error {
	if s == nil {
		return fmt.Errorf("配置库未打开")
	}
	if id <= 0 {
		return fmt.Errorf("任务记录不存在")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("读取任务记录失败: %w", err)
	}
	defer tx.Rollback()
	var page string
	if err := tx.QueryRow(`SELECT page FROM sessions WHERE id=?`, id).Scan(&page); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("任务记录不存在")
		}
		return fmt.Errorf("读取任务记录失败: %w", err)
	}
	now := time.Now().Unix()
	if _, err = tx.Exec(`UPDATE sessions SET active=0 WHERE page=? AND id<>?`, page, id); err != nil {
		return fmt.Errorf("读取任务记录失败: %w", err)
	}
	if _, err = tx.Exec(`UPDATE sessions SET active=1, updated_at=? WHERE id=?`, now, id); err != nil {
		return fmt.Errorf("读取任务记录失败: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("读取任务记录失败: %w", err)
	}
	return nil
}

// DeleteSession 删掉会话和任务记录。不碰磁盘上的图片。
func (s *Store) DeleteSession(id int64) error {
	if s == nil {
		return fmt.Errorf("配置库未打开")
	}
	if id <= 0 {
		return fmt.Errorf("任务记录不存在")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("删除历史记录失败: %w", err)
	}
	defer tx.Rollback()
	if err := mustSession(tx, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM session_tasks WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("删除历史记录失败: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE id=?`, id); err != nil {
		return fmt.Errorf("删除历史记录失败: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("删除历史记录失败: %w", err)
	}
	return nil
}

type rowQuery interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func mustSession(q rowQuery, id int64) error {
	var one int
	err := q.QueryRow(`SELECT 1 FROM sessions WHERE id=?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("任务记录不存在")
	}
	if err != nil {
		return fmt.Errorf("读取任务记录失败: %w", err)
	}
	return nil
}

func scanOne(q rowQuery, query string, args ...any) (Session, error) {
	var sess Session
	var active int
	err := q.QueryRow(query, args...).Scan(&sess.ID, &sess.Page, &sess.Created, &sess.Updated, &sess.Prompts, &sess.Images, &sess.Progress, &sess.ProgText, &active)
	sess.Active = active != 0
	return sess, err
}

func scanCounted(rows *sql.Rows) (Session, int, error) {
	var sess Session
	var active, n int
	err := rows.Scan(&sess.ID, &sess.Page, &sess.Created, &sess.Updated, &sess.Prompts, &sess.Images, &sess.Progress, &sess.ProgText, &active, &n)
	sess.Active = active != 0
	return sess, n, err
}

func loadTasks(q rowQuery, sessionID int64) ([]SessionTask, error) {
	rows, err := q.Query(`SELECT id, session_id, ord, prompt, source, platform, account_id, account_name, status, outputs, error, at_unix
		FROM session_tasks WHERE session_id=? ORDER BY ord, id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("读取任务记录失败: %w", err)
	}
	defer rows.Close()
	var out []SessionTask
	for rows.Next() {
		var item SessionTask
		if err := rows.Scan(&item.ID, &item.SessionID, &item.Ord, &item.Prompt, &item.Source, &item.Platform, &item.AccountID, &item.AccountName, &item.Status, &item.Outputs, &item.Error, &item.At); err != nil {
			return nil, fmt.Errorf("读取任务记录失败: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取任务记录失败: %w", err)
	}
	return out, nil
}
