package strategy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	extbundle "github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
)

// ErrWashWaitTimeout is returned only after a submitted batch yielded no results.
var ErrWashWaitTimeout = errors.New("等待豆包批量洗图结果超时")

func DoubaoWashPrompt(instruction string, names []string) string {
	var prompt strings.Builder
	prompt.WriteString(instruction)
	fmt.Fprintf(&prompt, "\n请将以上 %d 张图片分别进行洗图，严格按照附件顺序依次生成 %d 张独立结果图，每张结果只对应同位置的原图。不要拼图、合并或遗漏。", len(names), len(names))
	prompt.WriteString("\n本批次仅使用本条消息新上传的以下附件，忽略历史消息中的图片。每张结果只能引用对应的当前附件：")
	for i, name := range names {
		fmt.Fprintf(&prompt, "\n第 %d 张：%s", i+1, name)
	}
	return prompt.String()
}

// PrepareWashBatch gives the native file chooser unique names, including for
// sources with the same basename. Keep them readable until the attempt ends.
func (d *Doubao) PrepareWashBatch(files, names []string) (func(), error) {
	if len(files) != len(names) {
		return nil, errors.New("豆包源图和附件数量不一致")
	}
	dir, err := os.MkdirTemp("", "tulai-wash-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	for i, path := range files {
		if !filepath.IsLocal(names[i]) || filepath.Base(names[i]) != names[i] {
			cleanup()
			return nil, errors.New("无效的豆包附件名")
		}
		if err := stageWashFile(path, filepath.Join(dir, names[i])); err != nil {
			cleanup()
			return nil, err
		}
	}
	d.washNames, d.washDir = names, dir
	return cleanup, nil
}

func stageWashFile(path, dest string) error {
	// A hard link avoids copying large inputs on the same volume. Copy from
	// the file handle for other volumes, shares and symbolic links.
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && os.Link(path, dest) == nil {
		return nil
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, src)
	return errors.Join(err, out.Close())
}

func (d Doubao) UploadFiles(s *cdp.Session, files []string) error {
	if len(d.washNames) == 0 {
		return d.base.UploadFiles(s, files)
	}
	s.EnsureBase()
	// 当豆包文件选择框支持 multiple（或尚未预挂载）时，一次性提交整批源图让浏览器并发上传到 TOS。
	// 不支持多选或整批未确认时，先清干净残留附件再回退逐张上传，避免重复叠传。
	if len(d.washNames) > 1 && s.EvaluateBool(`(() => {
		const input = document.querySelector('input[type="file"][data-testid="upload-file-input"]') || document.querySelector('input[type="file"]');
		return !input || !!input.multiple;
	})()`) {
		paths := make([]string, len(d.washNames))
		for i, name := range d.washNames {
			paths[i] = filepath.Join(d.washDir, name)
		}
		if err := s.UploadFiles("window.tulaiOpenUpload()", paths); err == nil {
			checkErr := checkWashAttachmentsSet(s, d.washNames, true)
			if checkErr == nil {
				return nil
			}
			if !clearWashAttachments(s) {
				return checkErr
			}
		} else {
			if diag, diagErr := s.Evaluate(`window.tulaiUploadAncestorDiagnostic ? tulaiUploadAncestorDiagnostic() : ""`); diagErr == nil && diag != "" {
				logging.Log.Debugf("豆包批量上传诊断: %s", diag)
			}
			if !strings.Contains(err.Error(), "不支持多选") && !clearWashAttachments(s) {
				return err
			}
		}
	}
	// Some Doubao file choosers accept just one file at a time. Verify each
	// attachment in the composer before selecting the next one for this message.
	for i, name := range d.washNames {
		if err := s.UploadFiles("window.tulaiOpenUpload()", []string{filepath.Join(d.washDir, name)}); err != nil {
			if diag, diagErr := s.Evaluate(`window.tulaiUploadAncestorDiagnostic ? tulaiUploadAncestorDiagnostic() : ""`); diagErr == nil && diag != "" {
				logging.Log.Debugf("豆包逐张上传诊断: %s", diag)
			}
			return err
		}
		if err := checkWashAttachments(s, d.washNames[:i+1], true); err != nil {
			return err
		}
	}
	return nil
}

// clearWashAttachments 在多选批量上传部分失败后清理输入区残留附件，确认归零后才允许回退逐张上传。
func clearWashAttachments(s *cdp.Session) bool {
	out, err := s.EvaluateAsync(`(async () => {
		const page = window.__tulaiWashPage;
		const names = () => (page && typeof page.attachmentNames === 'function') ? page.attachmentNames(document) : [];
		if (names().length === 0) return true;
		const deadline = Date.now() + 4000;
		for (let round = 0; round < 3 && Date.now() < deadline; round++) {
			if (page && typeof page.removeButtons === 'function' && typeof page.composer === 'function') {
				for (let step = 0; step < 20 && Date.now() < deadline; step++) {
					const buttons = page.removeButtons(document, page.composer(document));
					if (!buttons.length) break;
					const btn = buttons[0];
					try {
						if (typeof btn.click === 'function') btn.click();
						else if (typeof btn.dispatchEvent === 'function') btn.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
					} catch (e) {}
					await new Promise(resolve => setTimeout(resolve, 30));
					if (names().length === 0) return true;
				}
			}
			if (typeof window.tulaiCleanComposer === 'function') {
				try { await window.tulaiCleanComposer(); } catch (e) {}
			}
			const waitUntil = Math.min(deadline, Date.now() + 800);
			while (Date.now() < waitUntil) {
				if (names().length === 0) return true;
				await new Promise(resolve => setTimeout(resolve, 40));
			}
		}
		return names().length === 0;
	})()`)
	return err == nil && out == "true"
}

func checkWashAttachmentsSet(s *cdp.Session, names []string, wait bool) error {
	expected, _ := json.Marshal(names)
	timeout := 0
	if wait {
		timeout = 30000
	}
	out, err := s.EvaluateAsync(fmt.Sprintf(`(async () => {
		const page = window.__tulaiWashPage, expected = %s, want = new Set(expected), deadline = Date.now() + %d;
		for (;;) {
			const names = page.attachmentNames(document);
			if (names.length === expected.length && new Set(names).size === want.size &&
				names.every(name => want.has(name)) && page.freshAttachment(document, new Set())) return true;
			if (Date.now() >= deadline) return false;
			await new Promise(resolve => setTimeout(resolve, 40));
		}
	})()`, expected, timeout))
	if err != nil {
		return err
	}
	if out != "true" {
		return fmt.Errorf("豆包输入区未确认本批 %d 张附件的名称", len(names))
	}
	return nil
}

func checkWashAttachments(s *cdp.Session, names []string, wait bool) error {
	expected, _ := json.Marshal(names)
	timeout := 0
	if wait {
		timeout = 30000
	}
	out, err := s.EvaluateAsync(fmt.Sprintf(`(async () => {
		const page = window.__tulaiWashPage, expected = %s, deadline = Date.now() + %d;
		for (;;) {
			const names = page.attachmentNames(document);
			if (names.length === expected.length && expected.every((name, i) => name === names[i]) &&
				page.freshAttachment(document, new Set())) return true;
			if (Date.now() >= deadline) return false;
			await new Promise(resolve => setTimeout(resolve, 40));
		}
	})()`, expected, timeout))
	if err != nil {
		return err
	}
	if out != "true" {
		return fmt.Errorf("豆包输入区未确认本批前 %d 张附件的名称和顺序", len(names))
	}
	return nil
}

func (d Doubao) WaitForResult(s *cdp.Session, timeout time.Duration) ([]Result, error) {
	return waitForResultFor(s, "豆包", timeout, d.washNames)
}

type doubaoRouteInfo struct {
	Degraded     bool   `json:"degraded"`
	AgentPSM     string `json:"agentPsm"`
	AgentName    string `json:"agentName"`
	HasCreations bool   `json:"hasCreations"`
	HasCodeBlock bool   `json:"hasCodeBlock"`
	TextPreview  string `json:"textPreview"`
}

// inspectDoubaoRoute 拉取当前豆包对话的 SSR 页面，检查服务端是否把本轮生图请求静默改道至 flow.agent.shell。
func inspectDoubaoRoute(s *cdp.Session, names []string) *doubaoRouteInfo {
	if s == nil {
		return nil
	}
	expected, _ := json.Marshal(names)
	multi := len(names) > 1
	out, err := s.EvaluateAsync(fmt.Sprintf(`(async () => {
		const multi = %t;
		if (multi && !window.__tulaiCurrentDoubaoTurn?.()) return null;
		if (!window.TULAIDoubaoSourceMap) { %s }
		const map = window.TULAIDoubaoSourceMap;
		if (!map || typeof map.inspectRouteFromConversation !== 'function') return null;
		const names = %s;
		const here = () => location.origin + location.pathname.replace(/\/$/, '');
		const readyURL = raw => {
			try {
				const u = new URL(raw);
				if (!/^\/chat\/[^/]+$/.test(u.pathname) || /^\/chat\/local_/i.test(u.pathname)) return '';
				return u.origin + u.pathname;
			} catch (e) { return ''; }
		};
		let url = readyURL(here());
		if (!url) return null;
		let found = await map.inspectRouteFromConversation(url, names);
		const current = readyURL(here());
		if (!found && current && current !== url) {
			url = current;
			found = await map.inspectRouteFromConversation(url, names);
		}
		if (!found || readyURL(here()) !== url) return null;
		if (multi && !window.__tulaiCurrentDoubaoTurn?.()) return null;
		return found;
	})()`, multi, extbundle.DoubaoSourceMapJS(), expected))
	if err != nil {
		return nil
	}
	var info doubaoRouteInfo
	if cdp.ParseJSValue(out, &info) != nil {
		return nil
	}
	if !info.Degraded && info.AgentPSM == "" && !info.HasCreations && !info.HasCodeBlock && info.TextPreview == "" {
		return nil
	}
	return &info
}

// resolveDoubaoWash uses the same attachment URI checks as the page
// bridge. A nil result leaves visible candidates unassigned for manual review.
func resolveDoubaoWash(s *cdp.Session, names []string, visible []Result) []Result {
	expected, _ := json.Marshal(names)
	candidates, _ := json.Marshal(visible)
	out, err := s.EvaluateAsync(fmt.Sprintf(`(async () => {
		if (!window.__tulaiCurrentDoubaoTurn?.()) return null;
		if (!window.TULAIDoubaoSourceMap) { %s }
		const map = window.TULAIDoubaoSourceMap, names = %s, images = %s;
		const here = () => location.origin + location.pathname.replace(/\/$/, '');
		const read = url => images?.length ? map.resolveFromConversation(url, names, images)
			: map.recoverFromConversation(url, names);
		let url = here(), found = await read(url);
		const current = here();
		if (url !== current && /^\/chat\/local_\d+$/.test(new URL(url).pathname) &&
			new URL(url).origin === location.origin && /^\/chat\/\d+$/.test(new URL(current).pathname)) {
			url = current;
			found = await read(url);
		}
		return found && here() === url && window.__tulaiCurrentDoubaoTurn?.() ? found : null;
	})()`, extbundle.DoubaoSourceMapJS(), expected, candidates))
	var matched struct {
		Images  []Result `json:"images"`
		Indices []int    `json:"indices"`
	}
	if err != nil || cdp.ParseJSValue(out, &matched) != nil || len(matched.Images) != len(matched.Indices) {
		return nil
	}
	for i := range matched.Images {
		matched.Images[i].SourceIndex = matched.Indices[i]
	}
	return matched.Images
}
