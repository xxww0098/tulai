package chatgpt

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/images"
)

// Generation stays with a tulai task so retries poll the original conversation.
// shortcut: recovery is in memory, persist this state before supporting app-restart recovery.
type Generation struct {
	Attempted      bool
	Submitted      bool
	ConversationID string
	files          []string
	sediments      []string
	inputs         []string
}

type reference struct {
	FileID string `json:"file_id"`
	Name   string
	Size   int
	Width  int
	Height int
	MIME   string
}

func (c *Client) upload(ctx context.Context, path string) (reference, error) {
	file, err := os.Open(path)
	if err != nil {
		return reference{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return reference{}, errors.New("ChatGPT 参考图超过 64 MiB 或读取失败")
	}
	if !images.IsValidImageBytes(data) {
		return reference{}, errors.New("ChatGPT 参考图需使用 PNG、JPEG、GIF 或 WebP")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 100000000 {
		return reference{}, errors.New("ChatGPT 参考图格式或尺寸无效")
	}
	mime := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}[format]
	if mime == "" {
		return reference{}, errors.New("ChatGPT 参考图需使用 PNG、JPEG、GIF 或 WebP")
	}
	r := reference{Name: filepath.Base(path), Size: len(data), Width: cfg.Width, Height: cfg.Height, MIME: mime}
	var meta struct {
		FileID    string `json:"file_id"`
		UploadURL string `json:"upload_url"`
	}
	if err := c.json(ctx, http.MethodPost, "/backend-api/files", map[string]any{
		"file_name": r.Name, "file_size": r.Size, "use_case": "multimodal", "width": r.Width, "height": r.Height,
	}, nil, &meta); err != nil {
		return reference{}, err
	}
	if !validID.MatchString(meta.FileID) || !allowedAssetURL(meta.UploadURL) {
		return reference{}, errors.New("ChatGPT 返回的上传地址或文件标识无效")
	}
	resp, err := c.request(ctx, http.MethodPut, meta.UploadURL, data, http.Header{
		"Content-Type": {mime}, "X-Ms-Blob-Type": {"BlockBlob"}, "X-Ms-Version": {"2020-04-08"},
	})
	if err != nil {
		return reference{}, err
	}
	resp.Body.Close()
	r.FileID = meta.FileID
	if err := c.json(ctx, http.MethodPost, "/backend-api/files/"+r.FileID+"/uploaded", map[string]any{}, nil, nil); err != nil {
		return reference{}, err
	}
	return r, nil
}

func allowedAssetURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && allowedURL(raw) && u.Hostname() != "chatgpt.com"
}

func imageModel(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" || model == DefaultModel {
		return "gpt-5-5", nil
	}
	if model == "codex-gpt-image-2" {
		return "", errors.New("当前 ChatGPT 服务商使用网页生图，请选择 gpt-image-2 或 auto")
	}
	if len(model) > 100 || !validID.MatchString(model) {
		return "", errors.New("ChatGPT 模型名称无效")
	}
	return model, nil
}

func (c *Client) Start(ctx context.Context, g *Generation, prompt, model string, paths []string) error {
	if g.Attempted {
		return ErrSubmissionUncertain
	}
	if strings.TrimSpace(prompt) == "" {
		return errors.New("ChatGPT 提示词不能为空")
	}
	upstreamModel, err := imageModel(model)
	if err != nil {
		return err
	}
	var refs []reference
	for _, path := range paths {
		r, err := c.upload(ctx, path)
		if err != nil {
			return err
		}
		refs = append(refs, r)
		g.inputs = append(g.inputs, r.FileID)
	}
	req, err := c.requirements(ctx)
	if err != nil {
		return err
	}
	headers := http.Header{"Openai-Sentinel-Chat-Requirements-Token": {req.token}}
	if req.proof != "" {
		headers.Set("OpenAI-Sentinel-Proof-Token", req.proof)
	}
	base := map[string]any{
		"action": "next", "parent_message_id": uuid(), "model": upstreamModel,
		"timezone_offset_min": c.fp.Offset, "timezone": c.fp.Timezone,
		"conversation_mode": map[string]any{"kind": "primary_assistant"},
		"system_hints":      []string{"picture_v2"}, "supports_buffering": true,
		"supported_encodings": []string{"v1"}, "client_contextual_info": map[string]any{"app_name": "chatgpt.com"},
	}
	base["fork_from_shared_post"], base["client_prepare_state"] = false, "success"
	base["partial_query"] = map[string]any{"id": uuid(), "author": map[string]any{"role": "user"}, "content": map[string]any{"content_type": "text", "parts": []string{prompt}}}
	var prepared struct {
		ConduitToken string `json:"conduit_token"`
	}
	if err := c.json(ctx, http.MethodPost, "/backend-api/f/conversation/prepare", base, headers, &prepared); err != nil {
		return err
	}
	delete(base, "partial_query")
	delete(base, "fork_from_shared_post")
	parts := []any{}
	attachments := []any{}
	for _, r := range refs {
		parts = append(parts, map[string]any{"content_type": "image_asset_pointer", "asset_pointer": "file-service://" + r.FileID, "width": r.Width, "height": r.Height, "size_bytes": r.Size})
		attachments = append(attachments, map[string]any{"id": r.FileID, "name": r.Name, "mimeType": r.MIME, "size": r.Size, "width": r.Width, "height": r.Height})
	}
	parts = append(parts, prompt)
	kind := "text"
	if len(refs) > 0 {
		kind = "multimodal_text"
	}
	metadata := map[string]any{"system_hints": []string{"picture_v2"}, "serialization_metadata": map[string]any{"custom_symbol_offsets": []any{}}}
	if len(attachments) > 0 {
		metadata["attachments"] = attachments
	}
	base["messages"] = []any{map[string]any{"id": uuid(), "author": map[string]any{"role": "user"}, "create_time": float64(time.Now().UnixMilli()) / 1000,
		"content": map[string]any{"content_type": kind, "parts": parts}, "metadata": metadata}}
	base["client_prepare_state"], base["enable_message_followups"] = "sent", true
	base["force_parallel_switch"] = "auto"
	headers.Set("Accept", "text/event-stream")
	headers.Set("X-Oai-Turn-Trace-Id", uuid())
	if prepared.ConduitToken != "" {
		headers.Set("X-Conduit-Token", prepared.ConduitToken)
	}
	data, err := json.Marshal(base)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	g.Attempted = true
	resp, err := c.request(ctx, http.MethodPost, "/backend-api/f/conversation", data, headers)
	if err != nil {
		var rejected *httpError
		if errors.As(err, &rejected) && rejected.status >= 400 && rejected.status < 500 && rejected.status != 408 {
			g.Attempted = false
			return err
		}
		return errors.Join(ErrSubmissionUncertain, err)
	}
	defer resp.Body.Close()
	g.Submitted = true
	err = g.readStream(resp.Body)
	if errors.Is(err, ErrModeration) || errors.Is(err, ErrQuota) || errors.Is(err, ErrRate) || errors.Is(err, ErrNeedLogin) {
		if g.ConversationID == "" && len(g.files) == 0 {
			g.Attempted, g.Submitted = false, false
		}
		return err
	}
	if g.ConversationID == "" && len(g.files) == 0 {
		return errors.Join(ErrSubmissionUncertain, err)
	}
	// A dropped SSE with a known conversation is recovered by polling, never by resending.
	return ctx.Err()
}

var (
	validID         = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	filePointer     = regexp.MustCompile(`file-service://([A-Za-z0-9_-]+)|\b(file_00000000[a-f0-9]{24})\b`)
	sedimentPointer = regexp.MustCompile(`sediment://([A-Za-z0-9_-]+)`)
)

func (g *Generation) collect(value any) {
	switch v := value.(type) {
	case map[string]any:
		if author, ok := v["author"].(map[string]any); ok && author["role"] == "user" {
			return
		}
		if id, ok := v["conversation_id"].(string); ok && validID.MatchString(id) {
			g.ConversationID = id
		}
		if path, ok := v["p"].(string); ok && strings.HasSuffix(path, "/conversation_id") {
			if id, ok := v["v"].(string); ok && validID.MatchString(id) {
				g.ConversationID = id
			}
		}
		for _, child := range v {
			g.collect(child)
		}
	case []any:
		for _, child := range v {
			g.collect(child)
		}
	case string:
		for _, match := range filePointer.FindAllStringSubmatch(v, -1) {
			id := match[1]
			if id == "" {
				id = match[2]
			}
			if id != "file_upload" && !slices.Contains(g.inputs, id) && !slices.Contains(g.files, id) {
				g.files = append(g.files, id)
			}
		}
		for _, match := range sedimentPointer.FindAllStringSubmatch(v, -1) {
			if !slices.Contains(g.sediments, match[1]) {
				g.sediments = append(g.sediments, match[1])
			}
		}
	}
}

func (g *Generation) readStream(r io.Reader) error {
	scanner := bufio.NewScanner(io.LimitReader(r, maxBytes+1))
	scanner.Buffer(make([]byte, 4096), 8<<20)
	var frame []string
	consume := func() error {
		payload := strings.Join(frame, "\n")
		frame = nil
		if payload == "[DONE]" {
			return io.EOF
		}
		if payload == "" {
			return nil
		}
		var event map[string]any
		if json.Unmarshal([]byte(payload), &event) != nil {
			return errors.New("ChatGPT 生成流格式无效")
		}
		if value, ok := event["error"]; ok && value != nil {
			b, _ := json.Marshal(value)
			return responseError(http.StatusBadRequest, b)
		}
		g.collect(event)
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := consume(); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
		} else if strings.HasPrefix(line, "data:") {
			frame = append(frame, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	err := consume()
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (c *Client) Results(ctx context.Context, g *Generation) ([]string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if g.ConversationID != "" {
			var conversation struct {
				Mapping map[string]struct {
					Message map[string]any `json:"message"`
				} `json:"mapping"`
			}
			err := c.json(ctx, http.MethodGet, "/backend-api/conversation/"+g.ConversationID, nil, nil, &conversation)
			if err != nil && !isNotFound(err) {
				return nil, err
			}
			for _, node := range conversation.Mapping {
				if err := g.outputMessage(node.Message); err != nil {
					return nil, err
				}
			}
		}
		var results []string
		pendingImage := false
		for _, id := range g.files {
			raw, err := c.downloadURL(ctx, "/backend-api/files/"+id+"/download")
			if err == nil {
				results = append(results, raw)
			} else if isNotFound(err) {
				pendingImage = true
			} else {
				return nil, err
			}
		}
		for _, id := range g.sediments {
			if g.ConversationID == "" {
				pendingImage = true
				continue
			}
			raw, err := c.downloadURL(ctx, "/backend-api/conversation/"+g.ConversationID+"/attachment/"+id+"/download")
			if err == nil && !slices.Contains(results, raw) {
				results = append(results, raw)
			} else if isNotFound(err) {
				pendingImage = true
			} else if err != nil {
				return nil, err
			}
		}
		if len(results) > 0 && !pendingImage {
			return results, nil
		}
		if g.ConversationID == "" {
			return nil, ErrSubmissionUncertain
		}
		var pending struct {
			Tasks []struct {
				Conversation string         `json:"conversation_id"`
				Original     string         `json:"original_conversation_id"`
				Message      map[string]any `json:"image_gen_message"`
			} `json:"tasks"`
		}
		if err := c.json(ctx, http.MethodGet, "/backend-api/tasks", nil, nil, &pending); err != nil && !isNotFound(err) {
			return nil, err
		}
		for _, task := range pending.Tasks {
			if task.Conversation == g.ConversationID || task.Original == g.ConversationID {
				if err := g.outputMessage(task.Message); err != nil {
					return nil, err
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (g *Generation) outputMessage(message map[string]any) error {
	author, _ := message["author"].(map[string]any)
	if author["role"] != "tool" && author["role"] != "assistant" {
		return nil
	}
	metadata, _ := message["metadata"].(map[string]any)
	if metadata["is_blocked"] == true || metadata["is_content_policy_violation"] == true {
		return ErrModeration
	}
	if metadata["is_error"] == true {
		data, _ := json.Marshal(message["content"])
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, "content_policy") || strings.Contains(lower, "content policy") || strings.Contains(lower, "内容政策") {
			return ErrModeration
		}
	}
	g.collect(message["content"])
	g.collect(metadata)
	return nil
}

func isNotFound(err error) bool {
	var e *httpError
	return errors.As(err, &e) && e.status == http.StatusNotFound
}

func (c *Client) downloadURL(ctx context.Context, path string) (string, error) {
	var result struct {
		URL         string `json:"url"`
		DownloadURL string `json:"download_url"`
	}
	if err := c.json(ctx, http.MethodGet, path, nil, nil, &result); err != nil {
		return "", err
	}
	raw := result.DownloadURL
	if raw == "" {
		raw = result.URL
	}
	if !allowedURL(raw) {
		return "", errors.New("ChatGPT 返回的图片地址无效")
	}
	return raw, nil
}
