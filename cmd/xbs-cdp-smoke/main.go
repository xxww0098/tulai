// cdp-smoke：验证 CDP 内核与真 Chrome 的连通性。
// 启动一个独立 profile 的 Chrome → 连接 → 建会话 → 注入基础脚本 → evaluate → 桥回调。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"xbs/internal/browser"
	"xbs/internal/cdp"
)

func main() {
	chrome, err := browser.FindChromePath()
	must(err)
	fmt.Println("Chrome:", chrome)

	dir, _ := os.MkdirTemp("", "xbs-smoke-*")
	defer os.RemoveAll(dir)
	port := 9555
	cmd := exec.Command(chrome,
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+dir,
		"--no-first-run", "--headless=new", "--remote-allow-origins=*",
		"about:blank")
	must(cmd.Start())
	defer cmd.Process.Kill()

	ws, err := browser.WaitWSURL(port, 15*time.Second)
	must(err)
	fmt.Println("WS 就绪:", ws)

	client, err := cdp.NewClient(ws)
	must(err)
	sess, err := client.NewSession("about:blank", func(line string) { fmt.Println("[console]", line) })
	must(err)
	defer sess.Close()

	// 1) evaluate
	out, err := sess.Evaluate(`1 + 1`)
	must(err)
	fmt.Println("evaluate 1+1 =", out)

	// 2) 桥往返
	got := make(chan string, 1)
	sess.Handle("ping", func(data string) string {
		got <- data
		return `{"echo":` + data + `}`
	})
	res, err := sess.EvaluateAsync(`window.xbsCall ? window.xbsCall('ping', '42').then(function(a){return a;}) : Promise.resolve('no-bridge')`)
	must(err)
	fmt.Println("桥返回:", res)
	select {
	case d := <-got:
		fmt.Println("桥回调收到:", d)
	case <-time.After(3 * time.Second):
		fmt.Println("!! 桥回调未收到")
	}

	// 3) 截图
	png, err := sess.CaptureScreenshotRect(0, 0, 200, 100)
	must(err)
	fmt.Printf("截图 %d 字节\n", len(png))

	// 4) DOM 原语
	out2, err := sess.Evaluate(`(function(){document.body.innerHTML='<textarea id=t></textarea>'; window.xbsInsertText && xbsInsertText('#t','hello'); return document.querySelector('#t').value;})()`)
	must(err)
	fmt.Println("插入文本:", out2)

	fmt.Println("CDP 冒烟测试完成 ✔")
}

func must(err error) {
	if err != nil {
		fmt.Println("失败:", err)
		os.Exit(1)
	}
}
