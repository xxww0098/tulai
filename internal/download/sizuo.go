package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var sizuoArticlePath = regexp.MustCompile(`^/[a-z]{1,40}/([1-9][0-9]{0,19})\.html$`)

func sizuoArticleID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Hostname(), "www.sizuo.com") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		path.Clean(u.Path) != u.Path || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(u.Path, "\\") {
		return ""
	}
	m := sizuoArticlePath.FindStringSubmatch(u.Path)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}
func isSizuoArticle(u *url.URL) bool { return sizuoArticleID(u) != "" }
func sizuoSameArticle(a, b *url.URL) bool {
	return sizuoArticleID(a) != "" && sizuoArticleID(a) == sizuoArticleID(b) && a.Path == b.Path &&
		!(a.Scheme == "https" && b.Scheme != "https")
}
func sizuoPageGetter(getter Getter, origin *url.URL) Getter {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || !sizuoSameArticle(origin, req.URL) {
			return errors.New("四座网文章跳转到其他地址")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("四座网文章重定向次数过多")
		}
		return nil
	}
	copyGetter.client = &client
	return &copyGetter
}
func discoverSizuo(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isSizuoArticle(root) {
		return result, errors.New("四座网文章地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, sizuoPageGetter(getter, root), crawlPage{url: root.String(), depth: 1})
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var status *HTTPStatusError
		if errors.As(err, &status) {
			return result, fmt.Errorf("四座网文章读取失败：HTTP %d", status.StatusCode)
		}
		return result, errors.New("四座网文章读取失败；历史站点可能已不可用")
	}
	if doc == nil || final == nil || !sizuoSameArticle(root, final) {
		return result, errors.New("四座网文章不可用或已跳转")
	}
	return sizuoCollect(ctx, getter, final, doc)
}
func sizuoEndpoint(page *url.URL, raw string) (*url.URL, bool) {
	id := sizuoArticleID(page)
	if id == "" || len(raw) == 0 || len(raw) > 2048 {
		return nil, false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil {
		return nil, false
	}
	u = page.ResolveReference(u)
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || !sameOrigin(page, u) || u.Path != "/getimgs.php" || u.Fragment != "" || len(q) != 1 || len(q["aid"]) != 1 || q.Get("aid") != id {
		return nil, false
	}
	return u, true
}
func sizuoEndpointGetter(getter Getter, endpoint *url.URL) Getter {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.User != nil || !sameOrigin(endpoint, req.URL) || req.URL.Path != endpoint.Path || req.URL.RawQuery != endpoint.RawQuery || req.URL.Fragment != "" {
			return errors.New("四座网图片数据接口发生跳转")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("四座网图片数据接口重定向次数过多")
		}
		return nil
	}
	copyGetter.client = &client
	return &copyGetter
}
func sizuoAsset(page *url.URL, raw string) (string, bool) {
	if raw == "" || len(raw) > 4096 || !strings.HasPrefix(raw, "/uploads/allimg/") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" || u.Fragment != "" ||
		path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "\\") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return "", false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
	default:
		return "", false
	}
	return page.ResolveReference(u).String(), true
}

// sizuoDataParser reads a small literal-only subset of the historical response.
// It never evaluates JavaScript or executes any code from the site.
type sizuoDataParser struct {
	data       string
	pos, nodes int
}

func (p *sizuoDataParser) space() error {
	for p.pos < len(p.data) {
		if strings.ContainsRune(" \t\r\n", rune(p.data[p.pos])) {
			p.pos++
			continue
		}
		if strings.HasPrefix(p.data[p.pos:], "//") {
			if i := strings.IndexByte(p.data[p.pos:], '\n'); i >= 0 {
				p.pos += i + 1
				continue
			}
			p.pos = len(p.data)
			return nil
		}
		if strings.HasPrefix(p.data[p.pos:], "/*") {
			i := strings.Index(p.data[p.pos+2:], "*/")
			if i < 0 {
				return errors.New("未结束的注释")
			}
			p.pos += i + 4
			continue
		}
		break
	}
	return nil
}
func sizuoIdentByte(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
func (p *sizuoDataParser) ident() string {
	start := p.pos
	for p.pos < len(p.data) && sizuoIdentByte(p.data[p.pos]) {
		p.pos++
	}
	return p.data[start:p.pos]
}
func (p *sizuoDataParser) value(depth int) (any, error) {
	p.nodes++
	if p.nodes > 50000 || depth > 12 {
		return nil, errors.New("数据结构超过限制")
	}
	if err := p.space(); err != nil {
		return nil, err
	}
	if p.pos >= len(p.data) {
		return nil, io.ErrUnexpectedEOF
	}
	switch p.data[p.pos] {
	case '\'', '"':
		quote := p.data[p.pos]
		p.pos++
		var out strings.Builder
		for p.pos < len(p.data) {
			if p.data[p.pos] == quote {
				p.pos++
				return out.String(), nil
			}
			if p.data[p.pos] == '\n' || p.data[p.pos] == '\r' {
				return nil, errors.New("字符串未结束")
			}
			r, _, tail, err := strconv.UnquoteChar(p.data[p.pos:], quote)
			if err != nil {
				return nil, err
			}
			consumed := len(p.data[p.pos:]) - len(tail)
			if consumed < 1 {
				return nil, errors.New("字符串无效")
			}
			p.pos += consumed
			out.WriteRune(r)
		}
		return nil, io.ErrUnexpectedEOF
	case '[':
		p.pos++
		return p.array(depth+1, ']')
	}
	if p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		start := p.pos
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
		n, err := strconv.Atoi(p.data[start:p.pos])
		if err != nil {
			return nil, err
		}
		return n, nil
	}
	ident := p.ident()
	switch ident {
	case "new":
		if err := p.space(); err != nil {
			return nil, err
		}
		if p.ident() != "Array" {
			return nil, errors.New("数据表达式不是数组")
		}
		if err := p.space(); err != nil {
			return nil, err
		}
		if p.pos >= len(p.data) || p.data[p.pos] != '(' {
			return nil, errors.New("数组语法无效")
		}
		p.pos++
		return p.array(depth+1, ')')
	case "null":
		return nil, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return nil, errors.New("数据含有非字面量表达式")
	}
}
func (p *sizuoDataParser) array(depth int, end byte) ([]any, error) {
	var out []any
	for {
		if err := p.space(); err != nil {
			return nil, err
		}
		if p.pos >= len(p.data) {
			return nil, io.ErrUnexpectedEOF
		}
		if p.data[p.pos] == end {
			p.pos++
			return out, nil
		}
		if len(out) >= 5000 {
			return nil, errors.New("数组长度超过限制")
		}
		v, err := p.value(depth)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		if err = p.space(); err != nil {
			return nil, err
		}
		if p.pos >= len(p.data) {
			return nil, io.ErrUnexpectedEOF
		}
		if p.data[p.pos] == end {
			p.pos++
			return out, nil
		}
		if p.data[p.pos] != ',' {
			return nil, errors.New("数组分隔符无效")
		}
		p.pos++
	}
}

// Skip other statements while respecting strings and nesting. Names within
// unrelated strings and neighboring gallery arrays are never interpreted.
func (p *sizuoDataParser) skipStatement() error {
	var quote byte
	depth := 0
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		p.pos++
		if quote != 0 {
			if c == '\\' {
				if p.pos < len(p.data) {
					p.pos++
				}
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			continue
		}
		if c == '/' && p.pos < len(p.data) {
			if p.data[p.pos] == '/' {
				if i := strings.IndexByte(p.data[p.pos:], '\n'); i >= 0 {
					p.pos += i + 1
					continue
				}
				p.pos = len(p.data)
				break
			}
			if p.data[p.pos] == '*' {
				i := strings.Index(p.data[p.pos+1:], "*/")
				if i < 0 {
					return errors.New("未结束的注释")
				}
				p.pos += i + 3
				continue
			}
		}
		switch c {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			depth--
			if depth < 0 {
				return errors.New("语句括号无效")
			}
		case ';':
			if depth == 0 {
				return nil
			}
		}
	}
	if quote != 0 || depth != 0 {
		return errors.New("语句未结束")
	}
	return nil
}
func sizuoParseAssignments(raw string) (map[string]any, error) {
	p := sizuoDataParser{data: raw}
	out := map[string]any{}
	for p.pos < len(p.data) {
		if err := p.space(); err != nil {
			return nil, err
		}
		if p.pos >= len(p.data) {
			break
		}
		name := p.ident()
		if name == "var" {
			if err := p.space(); err != nil {
				return nil, err
			}
			name = p.ident()
		}
		if err := p.space(); err != nil {
			return nil, err
		}
		if name == "CurGallery" || name == "PicArr" || name == "CurPos" {
			if p.pos >= len(p.data) || p.data[p.pos] != '=' {
				return nil, errors.New("图片数据赋值无效")
			}
			p.pos++
			v, err := p.value(0)
			if err != nil {
				return nil, err
			}
			if _, exists := out[name]; exists {
				return nil, errors.New("重复图片数据")
			}
			out[name] = v
			if err = p.space(); err != nil {
				return nil, err
			}
			if p.pos < len(p.data) {
				if p.data[p.pos] != ';' {
					return nil, errors.New("图片数据语句未结束")
				}
				p.pos++
			}
			continue
		}
		if err := p.skipStatement(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func sizuoCurrentImages(raw, id string) ([]string, bool) {
	values, err := sizuoParseAssignments(raw)
	if err != nil {
		return nil, false
	}
	var images []string
	if v, ok := values["CurGallery"]; ok {
		row, ok := v.([]any)
		if !ok || len(row) < 5 || row[0] != id || row[3] != id+".html" {
			return nil, false
		}
		list, ok := row[4].([]any)
		if !ok {
			return nil, false
		}
		for _, v := range list {
			s, ok := v.(string)
			if !ok {
				return nil, false
			}
			images = append(images, s)
		}
	}
	if v, ok := values["PicArr"]; ok {
		rows, ok := v.([]any)
		if !ok {
			return nil, false
		}
		pos, ok := values["CurPos"].(int)
		if !ok || pos < 0 || pos >= len(rows) {
			return nil, false
		}
		row, ok := rows[pos].([]any)
		if !ok || len(row) < 5 || row[0] != id || row[3] != id+".html" {
			return nil, false
		}
		image, ok := row[4].(string)
		if !ok {
			return nil, false
		}
		images = append(images, image)
	}
	return images, len(images) > 0
}
func sizuoCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := sizuoArticleID(page)
	if id == "" || getter == nil || doc == nil {
		return result, errors.New("四座网文章页面无效")
	}
	var endpoint *url.URL
	bad := false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "script" {
			return
		}
		raw := attribute(n, "src")
		if !strings.Contains(raw, "getimgs.php") {
			return
		}
		if u, ok := sizuoEndpoint(page, raw); ok {
			if endpoint != nil && endpoint.String() != u.String() {
				bad = true
			}
			endpoint = u
		} else {
			bad = true
		}
	})
	if bad || endpoint == nil {
		return result, errors.New("四座网文章未提供匹配的图片数据接口")
	}
	resp, err := sizuoEndpointGetter(getter, endpoint).Get(ctx, endpoint.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/javascript,text/javascript,text/plain,*/*;q=0.8")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", page.Scheme+"://"+page.Host+page.Path)
	})
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("四座网图片数据读取失败；历史站点可能已不可用")
	}
	if resp == nil || resp.Body == nil {
		return result, errors.New("四座网图片数据响应无效")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("四座网图片数据读取失败：HTTP %d", resp.StatusCode)
	}
	final, err := url.Parse(firstNonEmpty(resp.FinalURL, endpoint.String()))
	if err != nil || final.User != nil || !sameOrigin(endpoint, final) || final.Path != endpoint.Path || final.RawQuery != endpoint.RawQuery {
		return result, errors.New("四座网图片数据接口发生跳转")
	}
	if v := strings.TrimSpace(resp.Header.Get("Content-Encoding")); v != "" && !strings.EqualFold(v, "identity") {
		return result, errors.New("四座网图片数据压缩格式不受支持")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil || len(body) > 2<<20 {
		return result, errors.New("四座网图片数据无法读取或超过上限")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	images, valid := sizuoCurrentImages(string(body), id)
	if !valid {
		return result, errors.New("四座网图片数据无法绑定当前作品")
	}
	seen := map[string]bool{}
	partial := false
	for _, raw := range images {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		u, ok := sizuoAsset(page, raw)
		if !ok {
			partial = true
			continue
		}
		if seen[u] {
			continue
		}
		seen[u] = true
		item := Item{URL: u, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"sizuo_article_id": id}}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", u); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) == 0 {
		return result, errors.New("四座网当前作品没有可确认的图片")
	}
	if partial {
		return result, errors.New("四座网已保留当前作品图片；部分地址未能确认")
	}
	return result, nil
}
