package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestWorkspaceProviderPickerMatchesStart(t *testing.T) {
	for _, page := range []int{pageHome, pageWash} {
		for _, size := range [][2]int{{1280, 840}, {1024, 700}, {760, 900}} {
			t.Run(fmt.Sprintf("%s-%dx%d", pageLabel(page), size[0], size[1]), func(t *testing.T) {
				width, height := float32(size[0]), float32(size[1])
				a := &App{
					tab: page, statePage: page, stagePicked: true, platformAuto: true,
					prompts: "画一只猫", outputPath: t.TempDir(),
					browserRows: []store.Browser{
						loggedLane(1, "豆包账号", "豆包"),
						loggedLane(2, "千问账号", "千问"),
						loggedLane(3, "Antigravity账号", agy.Platform),
					},
				}
				if page == pageWash {
					a.sources = []sourceImage{{Path: "原图.png"}}
				}
				tt := ui.NewTester(a.view, size[0], size[1])
				tt.Advance(300 * time.Millisecond)
				picker := assertVisible(t, tt, "服务商选择", width, height)
				input := "提示词内容"
				start := "开始出图"
				if page == pageWash {
					input, start = "洗图指令", "开始洗图"
				}
				inputRect := assertVisible(t, tt, input, width, height)
				if picker.Y+picker.H > inputRect.Y {
					t.Fatalf("服务商选择应在输入区之前: picker=%+v input=%+v", picker, inputRect)
				}
				if page == pageHome {
					assertVisible(t, tt, "拖入或点击添加图片", width, height)
				}
				assertVisible(t, tt, start, width, height)
				for _, platform := range []string{"豆包", "千问", agy.Platform} {
					if err := tt.Click(platform); err != nil {
						t.Fatal(err)
					}
					tt.Frame()
					assertVisible(t, tt, "本次发送至："+platform, width, height)
					plan := a.workbenchPlan()
					if plan.Action != StartGenerate || len(plan.Platforms) != 1 || plan.Platforms[0] != platform {
						t.Fatalf("选择 %s 后的任务计划: %+v", platform, plan)
					}
					if !tt.HasText("当前服务商：" + platform) {
						t.Fatalf("结果区没有标出当前服务商 %s", platform)
					}
				}
			})
		}
	}
}

func TestWorkspaceProviderPickerCanBrowseWhileRunning(t *testing.T) {
	a := &App{
		platformAuto: true, running: true, runPage: pageHome,
		browserRows: []store.Browser{loggedLane(1, "千问账号", "千问")},
		queue:       []tasks.Task{{Index: 1, Platform: "千问", BrowserID: 1, Status: tasks.StatusRunning}},
	}
	tt := ui.NewTester(a.view, 1280, 840)
	tt.Advance(300 * time.Millisecond)
	assertVisible(t, tt, "当前查看：千问", 1280, 840)
	if err := tt.Click("豆包"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	assertVisible(t, tt, "当前查看：豆包", 1280, 840)
	if !a.running || len(a.queue) != 1 || a.queue[0].Platform != "千问" || tt.HasText("本次发送至：豆包") {
		t.Fatalf("切换查看不应改变运行任务: %+v", a.queue)
	}
}
