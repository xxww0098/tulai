// tulai 是豆包和千问的批量生图桌面应用。
// 本地工具，不含授权激活。
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/appdata"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/ui"
	"github.com/xxww0098/tulai/internal/update"
)

func main() {
	if handleCommand(os.Args) {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		log.Fatalf("定位应用失败: %v", err)
	}
	update.MarkStarted(executable)
	go func() {
		time.Sleep(5 * time.Second)
		update.CommitPrevious(executable)
	}()
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
	if err := logging.Open("."); err != nil {
		log.Printf("打开日志文件失败: %v", err)
		logging.Log.Errorf("打开日志文件失败: %v", err)
	} else {
		defer logging.Close()
	}
	dataRoot := "BrowserData"
	db, err := store.Open(filepath.Join("database", "config.db"))
	if err != nil {
		logging.Log.Errorf("打开配置库失败: %v", err)
		_ = logging.Close()
		log.Fatalf("打开配置库失败: %v", err)
	}
	bm := browser.NewManager()
	if err := db.PrepareCamoufoxProfiles(dataRoot); err != nil {
		log.Fatalf("迁移 Camoufox 账号: %v", err)
	}

	engine := tasks.NewEngine(db, bm, nil, dataRoot)
	// mygo build 会把 mygo.json 的版本链进二进制；go run 没有这段链接信息。
	if dataDir == "" {
		dataDir, err = os.Getwd()
		if err != nil {
			dataDir = "."
		}
	}
	logging.Log.Info("tulai %s 启动（数据目录: %s）", runningVersion(), dataDir)
	defer bm.StopAll()
	defer engine.CloseSessions()
	defer db.Close()
	ui.Run(db, bm, engine, dataRoot)
}

func packagedDataDir(goos, executable string) (string, error) {
	if goos != "windows" && goos != "darwin" {
		macosDir := filepath.Dir(executable)
		contentsDir := filepath.Dir(macosDir)
		bundleDir := filepath.Dir(contentsDir)
		if filepath.Base(macosDir) != "MacOS" || filepath.Base(contentsDir) != "Contents" || !strings.EqualFold(filepath.Ext(bundleDir), ".app") {
			return "", nil
		}
	}
	return appdata.Dir()
}
