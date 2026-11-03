package camoufox

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const Version = "0.5.9"
const nodeVersion = "v24.22.2"

const (
	// StageRuntime 是下载 Node 启动环境。
	StageRuntime = "runtime"
	// StagePackages 是安装官方启动库。
	StagePackages = "packages"
	// StageBrowser 是下载 Camoufox 内核。
	StageBrowser = "browser"
	// StageUnpack 是解压和校验内核。
	StageUnpack = "unpack"
	// StageDone 是安装完成。
	StageDone = "done"
)

// Progress 是一次安装进展。Done 和 Total 只在能按字节计量时非零；
// Total 为零表示这一步给不出百分比。
type Progress struct {
	Stage string
	Text  string
	Done  int64
	Total int64
}

func stageText(stage string) string {
	switch stage {
	case StageRuntime:
		return "正在下载 Camoufox 启动环境…"
	case StagePackages:
		return "正在安装 Camoufox 官方启动库…"
	case StageBrowser:
		return "正在下载 Camoufox 内核和指纹数据，首次安装可能需要数分钟…"
	case StageUnpack:
		return "正在解压并校验 Camoufox…"
	case StageDone:
		return "Camoufox 已就绪"
	}
	return ""
}

//go:embed driver/bridge.mjs driver/identity.mjs driver/package.json driver/package-lock.json
var driver embed.FS

type Runtime struct {
	Dir        string
	Node       string
	Executable string
}

type nodeArtifact struct{ File, SHA256 string }

// SHA256 values come from nodejs.org/dist/v24.22.2/SHASUMS256.txt.
var nodeArtifacts = map[string]nodeArtifact{
	"darwin/arm64":  {"node-v24.22.2-darwin-arm64.tar.gz", "aa9549349e47dffaa42dcf95d6e06ade8c4bdafc980979883b88767670df385a"},
	"darwin/amd64":  {"node-v24.22.2-darwin-x64.tar.gz", "d5e336d588c45be0c37dbd52cc29895593009ca4997a77a1fd02815131ed0e52"},
	"windows/amd64": {"node-v24.22.2-win-x64.zip", "ca1bff5d8ead13c9f2ea404a176db46bd430704d5b5113734049fa365e7799e2"},
	"linux/amd64":   {"node-v24.22.2-linux-x64.tar.gz", "361718c444e49af2ca5a110c83ba6a8901143f23ab285ebbef0341f391f482a9"},
	"linux/arm64":   {"node-v24.22.2-linux-arm64.tar.gz", "95c264150aceb7f3437a29fd6984924f5f8d1511ea3cbd6c508b83f6c555dae2"},
}

func Supported(goos, goarch string) bool { _, ok := nodeArtifacts[goos+"/"+goarch]; return ok }

func runtimeDir(dataDir string) string { return filepath.Join(dataDir, "runtime", "camoufox", Version) }
func nodePath(dir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "node", "node.exe")
	}
	return filepath.Join(dir, "node", "bin", "node")
}
func npmPath(dir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "node", "node_modules", "npm", "bin", "npm-cli.js")
	}
	return filepath.Join(dir, "node", "lib", "node_modules", "npm", "bin", "npm-cli.js")
}

func Installed(dataDir string) (Runtime, bool) {
	if strings.TrimSpace(dataDir) == "" {
		return Runtime{}, false
	}
	dir, err := filepath.Abs(runtimeDir(dataDir))
	if err != nil {
		return Runtime{}, false
	}
	var marker struct{ Executable, Version, Platform string }
	data, err := os.ReadFile(filepath.Join(dir, "ready.json"))
	if err != nil || json.Unmarshal(data, &marker) != nil || marker.Version != Version || marker.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		return Runtime{}, false
	}
	wantLock, _ := driver.ReadFile("driver/package-lock.json")
	gotLock, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
	if err != nil || !bytes.Equal(wantLock, gotLock) {
		return Runtime{}, false
	}
	for _, file := range []string{nodePath(dir), marker.Executable, filepath.Join(dir, "node_modules", "camoufox", "package.json")} {
		if info, err := os.Stat(file); err != nil || !info.Mode().IsRegular() {
			return Runtime{}, false
		}
	}
	return Runtime{Dir: dir, Node: nodePath(dir), Executable: marker.Executable}, true
}

var installMu sync.Mutex

// Ensure installs Node and the official launcher in the application data directory.
// Browser downloads and their version pairing are managed by the pinned Camoufox package.
func Ensure(ctx context.Context, dataDir, proxy string, progress func(Progress)) (Runtime, error) {
	installMu.Lock()
	defer installMu.Unlock()
	if strings.TrimSpace(dataDir) == "" {
		return Runtime{}, errors.New("没有应用数据目录")
	}
	if err := ctx.Err(); err != nil {
		return Runtime{}, err
	}
	if ready, ok := Installed(dataDir); ok {
		if err := writeDriver(ready.Dir); err != nil {
			return Runtime{}, err
		}
		return ready, nil
	}
	artifact, ok := nodeArtifacts[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return Runtime{}, errors.New("Camoufox 暂不支持此系统架构（Windows 需要 x64，macOS 支持 Intel 和 Apple Silicon）")
	}
	root, err := filepath.Abs(filepath.Dir(runtimeDir(dataDir)))
	if err != nil {
		return Runtime{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Runtime{}, err
	}
	stage, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return Runtime{}, err
	}
	defer os.RemoveAll(stage)
	note := func(stage string) {
		if progress != nil {
			progress(Progress{Stage: stage, Text: stageText(stage)})
		}
	}
	note(StageRuntime)
	var nodeProgress func(done, total int64)
	if progress != nil {
		nodeProgress = func(done, total int64) {
			progress(Progress{Stage: StageRuntime, Text: stageText(StageRuntime), Done: done, Total: total})
		}
	}
	archive := filepath.Join(stage, artifact.File)
	if err := downloadNode(ctx, archive, artifact, proxy, nodeProgress); err != nil {
		return Runtime{}, err
	}
	if err := extractNode(archive, filepath.Join(stage, "node")); err != nil {
		return Runtime{}, err
	}
	if err := os.Remove(archive); err != nil {
		return Runtime{}, err
	}
	if err := writeDriver(stage); err != nil {
		return Runtime{}, err
	}
	note(StagePackages)
	if _, err := runInstall(ctx, stage, proxy, nil, npmPath(stage), "ci", "--ignore-scripts", "--omit=dev", "--no-audit", "--no-fund"); err != nil {
		return Runtime{}, err
	}
	note(StageBrowser)
	var browserProgress func(Progress)
	if progress != nil {
		browserProgress = func(p Progress) {
			if p.Stage == "" {
				p.Stage = StageBrowser
			}
			if p.Text == "" {
				p.Text = stageText(p.Stage)
			}
			if p.Total > 0 && p.Done >= p.Total {
				p.Text = stageText(StageUnpack)
			}
			progress(p)
		}
	}
	out, err := runInstall(ctx, stage, proxy, browserProgress, filepath.Join(stage, "bridge.mjs"), "install")
	if err != nil {
		return Runtime{}, err
	}
	var marker struct {
		Executable string `json:"executable"`
		Version    string `json:"version"`
		Platform   string `json:"platform"`
	}
	if err := json.Unmarshal(out, &marker); err != nil {
		return Runtime{}, fmt.Errorf("读取 Camoufox 安装结果: %w", err)
	}
	if info, err := os.Stat(marker.Executable); err != nil || !info.Mode().IsRegular() {
		return Runtime{}, errors.New("Camoufox 安装未完成，缺少浏览器程序")
	}
	marker.Version = Version
	marker.Platform = runtime.GOOS + "/" + runtime.GOARCH
	data, _ := json.Marshal(marker)
	if err := os.WriteFile(filepath.Join(stage, "ready.json"), data, 0o600); err != nil {
		return Runtime{}, err
	}
	final := filepath.Join(root, Version)
	backup := final + ".old"
	if _, err := os.Stat(final); err == nil {
		if err := os.RemoveAll(backup); err != nil {
			return Runtime{}, err
		}
		if err := os.Rename(final, backup); err != nil {
			return Runtime{}, err
		}
	}
	if err := os.Rename(stage, final); err != nil {
		_ = os.Rename(backup, final)
		return Runtime{}, err
	}
	_ = os.RemoveAll(backup)
	if progress != nil {
		progress(Progress{Stage: StageDone, Text: stageText(StageDone), Done: 1, Total: 1})
	}
	return Runtime{Dir: final, Node: nodePath(final), Executable: marker.Executable}, nil
}

func writeDriver(dir string) error {
	for _, name := range []string{"bridge.mjs", "identity.mjs", "package.json", "package-lock.json"} {
		data, err := driver.ReadFile("driver/" + name)
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, name)
		if current, err := os.ReadFile(dest); err == nil && bytes.Equal(current, data) {
			continue
		}
		temporary, err := os.CreateTemp(dir, "."+name+"-*")
		if err != nil {
			return err
		}
		_, err = temporary.Write(data)
		err = errors.Join(err, temporary.Close())
		if err == nil {
			err = os.Rename(temporary.Name(), dest)
		}
		_ = os.Remove(temporary.Name())
		if err != nil {
			return err
		}
	}
	return nil
}

func runInstall(ctx context.Context, dir, proxy string, onProgress func(Progress), args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, nodePath(dir), args...)
	cmd.Dir, cmd.Env = dir, runtimeEnv(proxy, dir)
	configureCommand(cmd)
	cmd.Cancel = func() error { killCommand(cmd); return nil }
	cmd.WaitDelay = 3 * time.Second
	var stderr limitedLog
	cmd.Stderr = &stderr
	var out []byte
	var err error
	if onProgress == nil {
		out, err = cmd.Output()
	} else {
		out, err = outputWithProgress(cmd, onProgress)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("准备 Camoufox 失败: %w\n%s", err, stderr.String())
	}
	return out, nil
}

// outputWithProgress 读启动库写在 stdout 的进度事件，剩下的行留给调用方解析。
func outputWithProgress(cmd *exec.Cmd, onProgress func(Progress)) ([]byte, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if progress, ok := parseProgressLine(line); ok {
			onProgress(progress)
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if waitErr != nil {
		return nil, waitErr
	}
	if scanErr != nil {
		return nil, scanErr
	}
	return out.Bytes(), nil
}

// parseProgressLine 认启动库的 {"event":"progress",...} 行，其它行原样返回。
func parseProgressLine(line []byte) (Progress, bool) {
	if len(line) == 0 || line[0] != '{' {
		return Progress{}, false
	}
	var event struct {
		Event string `json:"event"`
		Stage string `json:"stage"`
		Done  int64  `json:"done"`
		Total int64  `json:"total"`
	}
	if json.Unmarshal(line, &event) != nil || event.Event != "progress" {
		return Progress{}, false
	}
	text := stageText(event.Stage)
	if event.Total > 0 && event.Done >= event.Total && event.Stage == StageBrowser {
		text = stageText(StageUnpack)
	}
	return Progress{Stage: event.Stage, Text: text, Done: event.Done, Total: event.Total}, true
}

type limitedLog struct{ bytes.Buffer }

func (l *limitedLog) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > 8192 {
		p = p[len(p)-8192:]
	}
	if l.Len()+len(p) > 8192 {
		l.Next(l.Len() + len(p) - 8192)
	}
	_, _ = l.Buffer.Write(p)
	return n, nil
}

func downloadNode(ctx context.Context, dest string, artifact nodeArtifact, proxy string, progress func(done, total int64)) error {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return err
		}
		transport.Proxy = http.ProxyURL(u)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://nodejs.org/dist/"+nodeVersion+"/"+artifact.File, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载启动环境: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载启动环境 HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	total := max(resp.ContentLength, 0)
	if progress != nil {
		progress(0, total)
	}
	body := io.Reader(resp.Body)
	if progress != nil {
		body = &progressReader{r: resp.Body, total: total, report: progress}
	}
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(body, 128<<20+1))
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if n > 128<<20 || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
		return errors.New("启动环境下载校验失败")
	}
	return nil
}

// progressReader 把已读字节数报给安装进度，最多每 150 毫秒一次。
type progressReader struct {
	r      io.Reader
	total  int64
	done   int64
	last   time.Time
	report func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.report != nil && (err != nil || p.last.IsZero() || time.Since(p.last) >= 150*time.Millisecond) {
		p.last = time.Now()
		p.report(p.done, p.total)
	}
	return n, err
}

func archivePath(root, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	_, relative, ok := strings.Cut(name, "/")
	if !ok || relative == "" {
		return "", nil
	}
	if !filepath.IsLocal(filepath.FromSlash(relative)) {
		return "", errors.New("启动环境压缩包路径无效")
	}
	return filepath.Join(root, filepath.FromSlash(relative)), nil
}

func extractNode(archive, root string) error {
	var total int64
	write := func(name string, size int64, mode os.FileMode, r io.Reader) error {
		dest, err := archivePath(root, name)
		if err != nil || dest == "" {
			return err
		}
		if size < 0 || size > 512<<20-total {
			return errors.New("启动环境解压大小超限")
		}
		total += size
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm()&0o755)
		if err != nil {
			return err
		}
		_, err = io.CopyN(f, r, size)
		return errors.Join(err, f.Close())
	}
	if strings.HasSuffix(archive, ".zip") {
		reader, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer reader.Close()
		for _, entry := range reader.File {
			if entry.FileInfo().IsDir() {
				continue
			}
			if !entry.Mode().IsRegular() {
				return errors.New("启动环境压缩包包含非常规文件")
			}
			r, err := entry.Open()
			if err != nil {
				return err
			}
			err = write(entry.Name, int64(entry.UncompressedSize64), entry.Mode(), r)
			r.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tarball := tar.NewReader(gz)
	for {
		header, err := tarball.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		// npm is invoked through its JS entry point; archive bin/npm symlinks are unnecessary.
		if header.Typeflag == tar.TypeDir || header.Typeflag == tar.TypeSymlink {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return errors.New("启动环境压缩包包含非常规文件")
		}
		if err := write(header.Name, header.Size, os.FileMode(header.Mode), tarball); err != nil {
			return err
		}
	}
}
