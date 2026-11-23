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
	// Some Doubao file choosers accept just one file at a time. Verify each
	// attachment in the composer before selecting the next one for this message.
	for i, name := range d.washNames {
		if err := s.UploadFiles("window.tulaiOpenUpload()", []string{filepath.Join(d.washDir, name)}); err != nil {
			return err
		}
		if err := checkWashAttachments(s, d.washNames[:i+1], true); err != nil {
			return err
		}
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
			await new Promise(resolve => setTimeout(resolve, 200));
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
	return waitForResult(s, timeout, d.washNames)
}

// resolveDoubaoWash uses the same attachment URI checks as the Firefox page
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
