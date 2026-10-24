// ext-smoke 启动本机扩展桥，等待千问扩展 hello，并做一次 ping 与 status。
// 页面自动化不在这里执行。加载步骤见 extension/qianwen/README.md。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"xbs/internal/extbridge"
)

func main() {
	addr := os.Getenv("XBS_EXT_ADDR")
	if addr == "" {
		addr = extbridge.DefaultAddr
	}
	token := os.Getenv("XBS_EXT_TOKEN")
	if token == "" {
		token = "dev-token"
		fmt.Println("未设置 XBS_EXT_TOKEN，使用仅供本机开发的默认令牌 dev-token")
	}
	srv, err := extbridge.Listen(extbridge.Config{Addr: addr, Token: token})
	must(err)
	defer srv.Close()
	fmt.Printf("扩展桥监听 %s\n", srv.Addr())
	fmt.Printf("令牌 %s\n", token)
	fmt.Println("加载 extension/qianwen 后，在选项页保存同一令牌和端口。")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		sess, err := srv.Next(ctx)
		if err != nil {
			fmt.Println("退出")
			return
		}
		fmt.Printf("hello 已通过 session=%s platform=%s\n", sess.ID(), sess.Platform())
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if err := sess.Ping(callCtx); err != nil {
			fmt.Println("ping 失败:", err)
		} else {
			fmt.Println("ping/pong 成功")
		}
		cmd, err := extbridge.Command(extbridge.CmdStatus, nil)
		if err != nil {
			cancel()
			fmt.Println("status 命令:", err)
			continue
		}
		res, err := sess.Call(callCtx, cmd)
		cancel()
		if err != nil {
			fmt.Printf("status 失败: %v\n", err)
		} else {
			fmt.Printf("status 应答: %s\n", res.Payload)
		}
		fmt.Println("等待扩展断开或下一次 hello。Ctrl+C 退出。")
	}
}

func must(err error) {
	if err != nil {
		fmt.Println("失败:", err)
		os.Exit(1)
	}
}
