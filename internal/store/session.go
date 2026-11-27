package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// PageGen 是生图页的会话。
	PageGen = "gen"
	// PageWash 是洗图页的会话。
	PageWash = "wash"
	// PageFission 是裂变图片页的会话。
	PageFission = "fission"
	// PageGenQianwen 是千问自由生图页的独立会话。
	PageGenQianwen = "qianwen:gen"
	// PageWashQianwen 是千问批量洗图页的独立会话。
	PageWashQianwen = "qianwen:wash"
	// PageFissionQianwen 是千问裂变图片页的独立会话。
	PageFissionQianwen = "qianwen:fission"
	// PageGenQwenIntl 是千问国际自由生图页的独立会话。
	PageGenQwenIntl = "qwen_intl:gen"
	// PageWashQwenIntl 是千问国际批量洗图页的独立会话。
	PageWashQwenIntl = "qwen_intl:wash"
	// PageFissionQwenIntl 是千问国际裂变图片页的独立会话。
	PageFissionQwenIntl = "qwen_intl:fission"
	// PageGenGemini 是 Gemini 自由生图页的独立会话。
	PageGenGemini = "gemini:gen"
	// PageWashGemini 是 Gemini 批量洗图页的独立会话。
	PageWashGemini = "gemini:wash"
	// PageFissionGemini 是 Gemini 裂变图片页的独立会话。
	PageFissionGemini = "gemini:fission"
)

func validSessionPage(page string) bool {
	switch page {
	case PageGen, PageWash, PageFission,
		PageGenQianwen, PageWashQianwen, PageFissionQianwen,
		PageGenQwenIntl, PageWashQwenIntl, PageFissionQwenIntl,
		PageGenGemini, PageWashGemini, PageFissionGemini:
		return true
	default:
		return false
	}
}

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
	Sources  []SessionSource
}

// SessionSource 记录一张原图在会话里的洗图进度。
// 没有记录或 Washed=false 都算未洗，成功后同时保留第一张结果路径。
type SessionSource struct {
	Path   string
	Washed bool
	Output string
}

// SessionTask 是会话里的一条任务。Outputs 用换行分隔结果路径。
type SessionTask struct {
	ID           int64
	SessionID    int64
	Ord          int
	Prompt       string
	Source       string
	Platform     string
	AccountID    int64
	AccountName  string
	Status       string
	Outputs      string
	Error        string
	At           int64
	ElapsedMS    int64
	Undispatched bool
	QualityNote  string
}

// SaveActive 写入这一页的当前会话并把它标成活动。同页其他会话收起。
// sess.ID 为 0 或已经不存在时新建一行。
func (s *Store) SaveActive(sess Session, items []SessionTask) (int64, error) {
	if s == nil {
		return 0, fmt.Errorf("配置库未打开")
	}
	if !validSessionPage(sess.Page) {
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
	if _, err = tx.Exec(`DELETE FROM session_sources WHERE session_id=?`, id); err != nil {
		return 0, fmt.Errorf("保存洗图进度失败: %w", err)
	}
	if len(items) > 0 {
		taskStmt, err := tx.Prepare(`INSERT INTO session_tasks(session_id, ord, prompt, source, platform, account_id, account_name, status, outputs, error, at_unix, elapsed_ms, undispatched, quality_note)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return 0, fmt.Errorf("保存任务记录失败: %w", err)
		}
		defer taskStmt.Close()
		for i, item := range items {
			ord := item.Ord
			if ord <= 0 {
				ord = i + 1
			}
			undispatched := 0
			if item.Undispatched {
				undispatched = 1
			}
			if _, err = taskStmt.Exec(
				id, ord, item.Prompt, item.Source, item.Platform, item.AccountID, item.AccountName, item.Status, item.Outputs, item.Error, item.At, item.ElapsedMS, undispatched, item.QualityNote); err != nil {
				return 0, fmt.Errorf("保存任务记录失败: %w", err)
			}
		}
	}
	if len(sess.Sources) > 0 {
		srcStmt, err := tx.Prepare(`INSERT OR REPLACE INTO session_sources(session_id, path, washed, output, updated_at)
			VALUES(?,?,?,?,?)`)
		if err != nil {
			return 0, fmt.Errorf("保存洗图进度失败: %w", err)
		}
		defer srcStmt.Close()
		for _, item := range sess.Sources {
			if strings.TrimSpace(item.Path) == "" {
				continue
			}
			washed := 0
			if item.Washed {
				washed = 1
			}
			if _, err = srcStmt.Exec(id, item.Path, washed, item.Output, now); err != nil {
				return 0, fmt.Errorf("保存洗图进度失败: %w", err)
			}
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
	if !validSessionPage(page) {
		return Session{}, nil, false, fmt.Errorf("任务页无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Session{}, nil, false, fmt.Errorf("读取任务记录失败: %w", err)
	}
	defer tx.Rollback()
	sess, err := scanOne(tx, `SELECT id, page, created_at, updated_at, prompts, images, progress, prog_text, active
		FROM sessions WHERE page=? AND active=1 ORDER BY updated_at DESC LIMIT 1`, page)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, nil, false, nil
	}
	if err != nil {
		return Session{}, nil, false, fmt.Errorf("读取任务记录失败: %w", err)
	}
	sources, err := loadSources(tx, sess.ID)
	if err != nil {
		return Session{}, nil, false, err
	}
	sess.Sources = sources
	items, err := loadTasks(tx, sess.ID)
	if err != nil {
		return Session{}, nil, false, err
	}
	sess.Tasks = len(items)
	if err := tx.Commit(); err != nil {
		return Session{}, nil, false, fmt.Errorf("读取任务记录失败: %w", err)
	}
	return sess, items, true, nil
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
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Session{}, nil, fmt.Errorf("读取任务记录失败: %w", err)
	}
	defer tx.Rollback()
	sess, err := scanOne(tx, `SELECT id, page, created_at, updated_at, prompts, images, progress, prog_text, active
		FROM sessions WHERE id=?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, nil, fmt.Errorf("任务记录不存在")
	}
	if err != nil {
		return Session{}, nil, fmt.Errorf("读取任务记录失败: %w", err)
	}
	sources, err := loadSources(tx, sess.ID)
	if err != nil {
		return Session{}, nil, err
	}
	sess.Sources = sources
	items, err := loadTasks(tx, sess.ID)
	if err != nil {
		return Session{}, nil, err
	}
	sess.Tasks = len(items)
	if err := tx.Commit(); err != nil {
		return Session{}, nil, fmt.Errorf("读取任务记录失败: %w", err)
	}
	return sess, items, nil
}

type rowQuery interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func scanOne(q rowQuery, query string, args ...any) (Session, error) {
	var sess Session
	var active int
	err := q.QueryRow(query, args...).Scan(&sess.ID, &sess.Page, &sess.Created, &sess.Updated, &sess.Prompts, &sess.Images, &sess.Progress, &sess.ProgText, &active)
	sess.Active = active != 0
	return sess, err
}

func loadTasks(q rowQuery, sessionID int64) ([]SessionTask, error) {
	rows, err := q.Query(`SELECT id, session_id, ord, prompt, source, platform, account_id, account_name, status, outputs, error, at_unix, COALESCE(elapsed_ms,0), COALESCE(undispatched,0), COALESCE(quality_note,'')
		FROM session_tasks WHERE session_id=? ORDER BY ord, id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("读取任务记录失败: %w", err)
	}
	defer rows.Close()
	var out []SessionTask
	for rows.Next() {
		var item SessionTask
		var undispatched int
		if err := rows.Scan(&item.ID, &item.SessionID, &item.Ord, &item.Prompt, &item.Source, &item.Platform, &item.AccountID, &item.AccountName, &item.Status, &item.Outputs, &item.Error, &item.At, &item.ElapsedMS, &undispatched, &item.QualityNote); err != nil {
			return nil, fmt.Errorf("读取任务记录失败: %w", err)
		}
		item.Undispatched = undispatched != 0
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取任务记录失败: %w", err)
	}
	return out, nil
}

func loadSources(q rowQuery, sessionID int64) ([]SessionSource, error) {
	rows, err := q.Query(`SELECT path, washed, output FROM session_sources WHERE session_id=? ORDER BY path`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("读取洗图进度失败: %w", err)
	}
	defer rows.Close()
	var out []SessionSource
	for rows.Next() {
		var item SessionSource
		var washed int
		if err := rows.Scan(&item.Path, &washed, &item.Output); err != nil {
			return nil, fmt.Errorf("读取洗图进度失败: %w", err)
		}
		item.Washed = washed != 0
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("读取洗图进度失败: %w", err)
	}
	return out, nil
}
