package cdp

import (
	"context"
	"errors"
	"strings"
	"time"
)

const extensionName = "tulai 页面桥"

const manifestNameExpr = `({name:(chrome.runtime.getManifest()||{}).name||""})`

// 冷启动时 Edge 自带的 service worker 还没有执行上下文。
// 对它 Runtime.evaluate 会一直堵到外层超时，后面的页面桥就看不到了。
// 附着本身也可能把正在启动的 worker 暂停住，所以先放行再读清单。
const (
	extensionWorkerProbe  = time.Second
	targetListBudget      = 2 * time.Second
	ensureExtensionBudget = 20 * time.Second
)

// EnsureExtension 确认这个 profile 里已经有页面桥。
// --load-extension 被品牌版 Chrome / Edge 忽略时，再用 Extensions.loadUnpacked。
// 调用方需要带上 --enable-unsafe-extension-debugging，否则后一条会失败。
//
// 调试端口比扩展服务更早就绪。组件扩展的 service worker 卡住时不能吃掉整段等待，
// 装上之后只等我们自己的 worker 开始执行。
func EnsureExtension(ctx context.Context, endpoint, dir string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, ensureExtensionBudget)
	defer cancel()
	found, err := extensionReady(ctx, endpoint, "")
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	extID, err := loadUnpacked(ctx, endpoint, dir)
	if err != nil {
		return err
	}
	for {
		found, err = extensionReady(ctx, endpoint, extID)
		if found {
			return nil
		}
		if err != nil && ctx.Err() == nil {
			return err
		}
		if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
			return errors.New("扩展已加载但没有看到页面桥")
		}
	}
}

func loadUnpacked(ctx context.Context, endpoint, dir string) (string, error) {
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	var out struct {
		ID string `json:"id"`
	}
	if err := conn.call(ctx, "", "Extensions.loadUnpacked", map[string]any{"path": dir}, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("扩展没有装上")
	}
	return out.ID, nil
}

// extensionReady 报告页面桥的 service worker 是否已经能读清单。
// extID 为空时任何一个页面桥都算。非空时只看这个扩展，不去附着别的 worker。
func extensionReady(ctx context.Context, endpoint, extID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	targets, err := boundedTargets(ctx, conn)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, nil
	}
	for _, item := range targets {
		if !extensionWorker(item, extID) {
			continue
		}
		var snap struct {
			Name string `json:"name"`
		}
		err := probeWorker(ctx, conn, item.ID, manifestNameExpr, &snap, false)
		if err == nil && snap.Name == extensionName {
			return true, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
	}
	return false, nil
}

func extensionWorker(item sliderTarget, extID string) bool {
	if item.ID == "" || !strings.HasPrefix(item.URL, "chrome-extension://") {
		return false
	}
	if item.Type != "service_worker" && item.Type != "background_page" {
		return false
	}
	if extID == "" {
		return true
	}
	return strings.HasPrefix(item.URL, "chrome-extension://"+extID+"/")
}

func boundedTargets(ctx context.Context, conn *SliderConn) ([]sliderTarget, error) {
	scan, cancel := context.WithTimeout(ctx, targetListBudget)
	defer cancel()
	return conn.targets(scan)
}

// probeWorker 在单个扩展 worker 上执行 expression。
// 每个 worker 单独计时，并且先放行等待调试器的启动暂停。
func probeWorker(ctx context.Context, conn *SliderConn, targetID, expression string, dest any, await bool) error {
	one, cancel := context.WithTimeout(ctx, extensionWorkerProbe)
	defer cancel()
	session, err := conn.session(one, targetID)
	if err != nil {
		return err
	}
	_ = conn.call(one, session, "Runtime.runIfWaitingForDebugger", map[string]any{}, nil)
	return conn.evalJSON(one, targetID, expression, dest, await)
}

// ExtensionPairState 读取这个 profile 里页面桥自己保存的配置文件标识和令牌。
// found 表示已经能看到页面桥的 service worker。标识还没写上时 profileID 为空。
func ExtensionPairState(ctx context.Context, endpoint string) (profileID, token string, found bool, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return "", "", false, err
	}
	defer conn.Close()
	targets, err := boundedTargets(ctx, conn)
	if err != nil {
		if ctx.Err() != nil || !errors.Is(err, context.DeadlineExceeded) {
			return "", "", false, err
		}
		return "", "", false, nil
	}
	expr := `chrome.storage.local.get(['profileId','token']).then(function (v) {
		return {name:(chrome.runtime.getManifest()||{}).name||'', id:String((v&&v.profileId)||''), token:String((v&&v.token)||'')};
	})`
	for _, item := range targets {
		if !extensionWorker(item, "") {
			continue
		}
		var snap struct {
			Name  string `json:"name"`
			ID    string `json:"id"`
			Token string `json:"token"`
		}
		// 别的扩展没有这段存储，脚本异常或这个 worker 还没起来就跳过。
		if err := probeWorker(ctx, conn, item.ID, expr, &snap, true); err != nil || snap.Name != extensionName {
			if ctx.Err() != nil {
				return "", "", false, ctx.Err()
			}
			continue
		}
		return snap.ID, snap.Token, true, nil
	}
	return "", "", false, nil
}
