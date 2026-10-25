// xbs 是豆包和千问的批量生图桌面应用（窗口标题「小帮手」）。
// 本地工具，不含授权激活。
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"xbs/internal/browser"
	"xbs/internal/logging"
	"xbs/internal/store"
	"xbs/internal/tasks"
	"xbs/internal/ui"
)

func main() {
	if handleCommand(os.Args) {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		log.Fatalf("定位应用失败: %v", err)
	}
	dataDir, err := packagedDataDir(runtime.GOOS, executable)
	if err != nil {
		log.Fatalf("定位应用数据目录失败: %v", err)
	}
	// 图形应用的工作目录不保证可写，统一相对数据路径到用户目录。
	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			log.Fatalf("创建应用数据目录失败: %v", err)
		}
		if err := os.Chdir(dataDir); err != nil {
			log.Fatalf("进入应用数据目录失败: %v", err)
		}
	}
	dataRoot := "BrowserData"
	db, err := store.Open(filepath.Join("database", "config.db"))
	if err != nil {
		log.Fatalf("打开配置库失败: %v", err)
	}
	bm := browser.NewManager()
	engine := tasks.NewEngine(db, bm, nil, dataRoot)
	// mygo build 会把 mygo.json 的版本链进二进制；go run 没有这段链接信息。
	logging.Log.Info("小帮手 %s 启动（数据目录: %s）", runningVersion(), dataRoot)
	defer bm.StopAll()
	defer engine.CloseSessions()
	defer db.Close()
	ui.Run(db, bm, engine, dataRoot)
}

func packagedDataDir(goos, executable string) (string, error) {
	if goos != "windows" {
		macosDir := filepath.Dir(executable)
		contentsDir := filepath.Dir(macosDir)
		bundleDir := filepath.Dir(contentsDir)
		if filepath.Base(macosDir) != "MacOS" || filepath.Base(contentsDir) != "Contents" || !strings.EqualFold(filepath.Ext(bundleDir), ".app") {
			return "", nil
		}
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "xbs"), nil
}
