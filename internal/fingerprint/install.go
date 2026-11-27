package fingerprint

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
	"sync/atomic"
	"time"

	"github.com/xxww0098/realbrowser/fingerprintkernel/downloader"
	fpdriver "github.com/xxww0098/realbrowser/fingerprintkernel/driver"
	"github.com/xxww0098/realbrowser/fingerprintkernel/process"
)

const Version = FingerprintVersion
const driverVersion = "1.62.0"
const nodeVersion = "v24.22.2"

const (
	// StageWaiting 表示另一进程正在安装同一份运行环境。
	StageWaiting = "waiting"
	// StageRuntime 是下载 Node 启动环境。
	StageRuntime = "runtime"
	// StagePackages 是安装 Playwright 驱动依赖。
	StagePackages = "packages"
	// StageBrowser 是下载 Fingerprint 浏览器内核。
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
	case StageWaiting:
		return "正在等待另一项 Fingerprint 安装完成…"
	case StageRuntime:
		return "正在下载浏览器启动环境…"
	case StagePackages:
		return "正在安装 Playwright 驱动…"
	case StageBrowser:
		return "正在下载 Fingerprint 浏览器内核…"
	case StageUnpack:
		return "正在解压并校验 Fingerprint…"
	case StageDone:
		return "Fingerprint 已就绪"
	}
	return ""
}

//go:embed driver/bridge.mjs driver/package.json driver/package-lock.json
var driver embed.FS

type Runtime struct {
	Version    string
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

func Supported(goos, goarch string) bool { return FingerprintSupported(goos, goarch) }

// nodeDistBases 是 Node 启动环境的下载源，依次尝试；镜像与官方发布内容一致，SHA-256 校验兜底。
var nodeDistBases = []string{
	"https://nodejs.org/dist/" + nodeVersion,
	"https://cdn.npmmirror.com/binaries/node/" + nodeVersion,
}

// npmMirrorRegistry 是 npm 依赖的镜像 registry。npm 默认的 replace-registry-host
// 会把 lock 文件里 npmjs.org 的地址改写到当前 registry，包完整性仍由 integrity 校验。
const npmMirrorRegistry = "https://registry.npmmirror.com"

// kernelMirrorDefaults 是内核下载使用的 GitHub 加速镜像列表（ghproxy 形式，
// base + '/' + 原 URL）。下载内容由固定 sha256 校验，镜像只能加速、无法篡改。
var kernelMirrorDefaults = []string{
	"https://ghfast.top",
	"https://gh-proxy.com",
}

func appendUniqueMirrors(primary, extra []string) []string {
	out := make([]string, 0, len(primary)+len(extra))
	seen := make(map[string]bool, len(primary)+len(extra))
	for _, list := range [][]string{primary, extra} {
		for _, item := range list {
			base := strings.TrimRight(strings.TrimSpace(item), "/")
			if base == "" || seen[base] {
				continue
			}
			seen[base] = true
			out = append(out, base)
		}
	}
	return out
}

// kernelAttempts 排列内核安装的尝试环境：未设代理时优先使用多镜像测速与并发分段下载，
// 设了代理时直连与镜像同轮测速（直连优先），显式配置 TULAI_FINGERPRINT_MIRROR 时优先该镜像。
func kernelAttempts(mirrors []string, explicit bool, proxy string) [][]string {
	if len(mirrors) == 0 {
		return [][]string{nil}
	}
	if explicit {
		all := appendUniqueMirrors(mirrors, kernelMirrorDefaults)
		return [][]string{
			{"TULAI_FINGERPRINT_MIRROR=" + mirrors[0], "TULAI_FINGERPRINT_MIRRORS=" + strings.Join(all, ",")},
			nil,
		}
	}
	csv := strings.Join(mirrors, ",")
	if strings.TrimSpace(proxy) != "" {
		return [][]string{
			{"TULAI_FINGERPRINT_MIRRORS=" + csv, "TULAI_FINGERPRINT_DIRECT_FIRST=1"},
			{"TULAI_FINGERPRINT_MIRRORS=" + csv},
			nil,
		}
	}
	return [][]string{
		{"TULAI_FINGERPRINT_MIRRORS=" + csv},
		nil,
	}
}

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

func nodePackageVersion(dir, name string) string {
	data, err := os.ReadFile(filepath.Join(dir, "node_modules", name, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	return pkg.Version
}

var installMu sync.Mutex

func Installed(dataDir string) (Runtime, bool) {
	return InstalledFingerprint(dataDir)
}

func Ensure(ctx context.Context, dataDir, proxy string, progress func(Progress)) (Runtime, error) {
	return EnsureFingerprint(ctx, dataDir, proxy, progress)
}

var errApplicationControl = errors.New("Windows 应用控制策略阻止加载 Fingerprint 启动组件")

// CheckLauncher checks the installed driver before assigning work to a browser account.
func CheckLauncher(ctx context.Context, dataDir string) error {
	rt, ok := InstalledFingerprint(dataDir)
	if !ok {
		return errors.New("Fingerprint 尚未准备好，请先在「设置 → 浏览器与网络」安装 Fingerprint")
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := runInstall(checkCtx, rt.Dir, "", nil, nil, "--input-type=module", "-e", "await import('playwright-core')")
	if err == nil || checkCtx.Err() != nil {
		return err
	}
	return launcherComponentError("playwright-core", err)
}

func launcherComponentError(component string, err error) error {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"application control policy", "blocked by group policy", "应用程序控制策略", "应用控制策略", "组策略阻止"} {
		if strings.Contains(message, marker) {
			return fmt.Errorf("%w %s。请联系设备管理员核准该组件", errApplicationControl, component)
		}
	}
	return fmt.Errorf("Fingerprint 启动组件 %s 无法加载: %w", component, err)
}

func publishRuntime(stage, final string) error {
	backup := final + ".old"
	if _, err := os.Stat(final); err == nil {
		if err := os.RemoveAll(backup); err != nil {
			return err
		}
		if err := os.Rename(final, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(stage, final); err != nil {
		_ = os.Rename(backup, final)
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
}

func prepareRuntime(ctx context.Context, stage, proxy string, artifact nodeArtifact, progress func(Progress)) error {
	if source, size, bundled := embeddedRuntimeBundle(); bundled {
		if progress != nil {
			progress(Progress{Stage: StageRuntime, Text: "正在准备内置的启动环境和依赖…"})
		}
		if err := unpackRuntimeBundle(ctx, source, size, stage, runtime.GOOS+"/"+runtime.GOARCH); err != nil {
			return err
		}
		if err := writeDriver(stage); err != nil {
			return err
		}
		if progress != nil {
			progress(Progress{Stage: StagePackages, Text: "正在检查内置的 Playwright 驱动…"})
		}
		if err := verifyNodePackages(ctx, stage, proxy); err != nil {
			return fmt.Errorf("内置驱动无法加载: %w", err)
		}
		return nil
	}
	if progress != nil {
		progress(Progress{Stage: StageRuntime, Text: stageText(StageRuntime)})
	}
	if err := installNode(ctx, stage, proxy, artifact, progress); err != nil {
		return err
	}
	if err := writeDriver(stage); err != nil {
		return err
	}
	if progress != nil {
		progress(Progress{Stage: StagePackages, Text: stageText(StagePackages)})
	}
	return installLockedPackages(ctx, stage, proxy, progress)
}

func verifyNodePackages(ctx context.Context, dir, proxy string) error {
	_, err := runInstall(ctx, dir, proxy, nil, nil, "--input-type=module", "-e", "await import('playwright-core')")
	return err
}

func installNode(ctx context.Context, dir, proxy string, artifact nodeArtifact, progress func(Progress)) error {
	archive := filepath.Join(dir, artifact.File)
	if err := downloadNode(ctx, archive, artifact, proxy, progress); err != nil {
		return err
	}
	if err := extractNode(archive, filepath.Join(dir, "node")); err != nil {
		return err
	}
	return os.Remove(archive)
}

func installLockedPackages(ctx context.Context, dir, proxy string, progress func(Progress)) error {
	npmArgs := []string{npmPath(dir), "ci", "--ignore-scripts", "--omit=dev", "--no-audit", "--no-fund"}
	primaryCtx, cancelPrimary := context.WithTimeout(ctx, 30*time.Second)
	_, err := runInstall(primaryCtx, dir, proxy, nil, nil, append(append([]string{}, npmArgs...), "--fetch-timeout=15000", "--fetch-retries=1")...)
	cancelPrimary()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if progress != nil {
			progress(Progress{Stage: StagePackages, Text: "官方源安装启动库较慢或失败，正在通过国内镜像重试…"})
		}
		if _, err := runInstall(ctx, dir, proxy, nil, nil, append(npmArgs, "--registry="+npmMirrorRegistry)...); err != nil {
			return err
		}
	}
	return nil
}

var bridgeScripts = []string{"bridge.mjs", "fingerprint-identity.mjs", "download-watch.mjs"}

func writeDriver(dir string) error {
	return writeDriverFiles(dir, append([]string{"package.json", "package-lock.json"}, bridgeScripts...))
}

func driverFile(name string) ([]byte, error) {
	if name == "fingerprint-identity.mjs" {
		return fpdriver.Files.ReadFile(name)
	}
	if name == "download-watch.mjs" {
		return downloader.Files.ReadFile(name)
	}
	return driver.ReadFile("driver/" + name)
}

func writeDriverFiles(dir string, files []string) error {
	for _, name := range files {
		data, err := driverFile(name)
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
			// On Windows a concurrent reader can briefly prevent replacing a file.
			// Another refresher may also have already written exactly these bytes.
			for attempt := 0; attempt < 8; attempt++ {
				err = os.Rename(temporary.Name(), dest)
				if err == nil {
					break
				}
				if current, readErr := os.ReadFile(dest); readErr == nil && bytes.Equal(current, data) {
					err = nil
					break
				}
				if attempt < 7 {
					time.Sleep(25 * time.Millisecond)
				}
			}
		}
		_ = os.Remove(temporary.Name())
		if err != nil {
			return err
		}
	}
	return nil
}

func runInstall(ctx context.Context, dir, proxy string, extraEnv []string, onProgress func(Progress), args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, nodePath(dir), args...)
	cmd.Dir, cmd.Env = dir, append(runtimeEnv(proxy, dir), extraEnv...)
	process.ConfigureCommand(cmd, process.PrivateDriver)
	cmd.Cancel = func() error { return process.KillCommand(cmd) }
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
		return nil, fmt.Errorf("准备浏览器组件失败: %w\n%s", err, stderr.String())
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

// downloadNode 依次尝试 nodeDistBases 里的下载源；官方源失败或过慢时换镜像重试，
// 重试前报告换源状态，内容始终用固定 SHA-256 校验。
func downloadNode(ctx context.Context, dest string, artifact nodeArtifact, proxy string, progress func(Progress)) error {
	var errs []error
	for i, base := range nodeDistBases {
		text := stageText(StageRuntime)
		var report func(done, total int64)
		if progress != nil {
			if i > 0 {
				text = "官方源下载启动环境失败，正在通过国内镜像重试…"
				progress(Progress{Stage: StageRuntime, Text: text})
			}
			finalText := text
			report = func(done, total int64) {
				progress(Progress{Stage: StageRuntime, Text: finalText, Done: done, Total: total})
			}
		}
		_ = os.Remove(dest)
		hasFallback := i < len(nodeDistBases)-1
		if err := fetchNodeArchive(ctx, dest, base+"/"+artifact.File, artifact, proxy, hasFallback, report); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", base, err))
			continue
		}
		return nil
	}
	return fmt.Errorf("下载启动环境失败: %w", errors.Join(errs...))
}

func fetchNodeArchive(ctx context.Context, dest, downloadURL string, artifact nodeArtifact, proxy string, hasFallback bool, report func(done, total int64)) error {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if hasFallback {
		transport.ResponseHeaderTimeout = 12 * time.Second
	}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return err
		}
		transport.Proxy = http.ProxyURL(u)
	}
	defer transport.CloseIdleConnections()
	attemptCtx, cancelAttempt := context.WithCancel(ctx)
	defer cancelAttempt()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Minute}
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, downloadURL, nil)
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
	if report != nil {
		report(0, total)
	}
	var bytesRead atomic.Int64
	var lastByteNano atomic.Int64
	lastByteNano.Store(time.Now().UnixNano())
	var slowErr atomic.Value
	if hasFallback {
		started := time.Now()
		doneWatch := make(chan struct{})
		defer close(doneWatch)
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-doneWatch:
					return
				case <-attemptCtx.Done():
					return
				case now := <-ticker.C:
					last := time.Unix(0, lastByteNano.Load())
					if now.Sub(last) >= 10*time.Second {
						slowErr.Store(errors.New("下载启动环境超时"))
						cancelAttempt()
						return
					}
					if elapsed := now.Sub(started); elapsed >= 4*time.Second {
						const minBytesPerSec = 256 * 1024
						if bytesRead.Load() < int64(elapsed.Seconds()*minBytesPerSec) {
							slowErr.Store(errors.New("下载启动环境速度过慢"))
							cancelAttempt()
							return
						}
					}
				}
			}
		}()
	}
	body := io.Reader(&progressReader{
		r:     resp.Body,
		total: total,
		onRead: func(done int64) {
			bytesRead.Store(done)
			lastByteNano.Store(time.Now().UnixNano())
		},
		report: report,
	})
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(body, 128<<20+1))
	err = errors.Join(err, f.Close())
	if err != nil {
		if stored, ok := slowErr.Load().(error); ok && ctx.Err() == nil {
			return stored
		}
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
	onRead func(done int64)
	report func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.onRead != nil && n > 0 {
		p.onRead(p.done)
	}
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
