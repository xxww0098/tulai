import { openUrl, signal, Text, View } from "nsgo";
import { Badge, Button, Dialog, Input, Progress, Tabs } from "nsgo/ui";
import { copyText } from "tulai/backend";
import { PillToggle } from "../components/primitives";
import { closeWizardModal, runWizardAction, showToast, wizard } from "../store";

const PLATFORMS = ["豆包", "千问", "ChatGPT", "Antigravity"];

export function WizardView() {
  const st = wizard;
  const agyCode = signal("");

  const wizardAction = runWizardAction;
  const finishWizard = closeWizardModal;

  const stepTone = (s?: string): "up" | "info" | "neutral" =>
    s === "done" ? "up" : s === "current" ? "info" : "neutral";

  return (
    <Dialog
      open={Boolean(st().on)}
      title="快速配置向导"
      description="几步完成出图平台选择、托管环境准备与首个账号登录"
      style={{ width: 620 }}
      testID="wizard-dialog-surface"
      onClose={() => closeWizardModal()}
      closeOnEscape
    >
      <View class="w-full min-w-0 gap-4" testID="wizard-dialog">
        {/* 步骤条 */}
        <View class="flex-row flex-wrap items-center gap-2 rounded-lg bg-slate-50 p-2.5 dark:bg-[#1A2232]">
          <Each of={st().steps ?? []} key={(s) => s.id || s.title || ""}>
            {(step, idx) => (
              <View class="flex-row items-center gap-1.5">
                <Badge
                  text={`${idx() + 1}. ${step().title || step().id}`}
                  variant={stepTone(step().state)}
                />
              </View>
            )}
          </Each>
        </View>

        {/* Step: platform */}
        <If when={st().step === "platform" || !st().step}>
          <View class="gap-3">
            <Text class="text-xs font-medium text-slate-700 dark:text-slate-200">
              选择您主要使用的出图平台：
            </Text>
            <View class="flex-row flex-wrap gap-2">
              <Each of={PLATFORMS} key={(p) => p}>
                {(plat) => (
                  <PillToggle
                    label={plat()}
                    active={st().choice === plat()}
                    onPress={() => void wizardAction("choose", plat())}
                  />
                )}
              </Each>
            </View>
            <View class="flex-row justify-end gap-2 pt-2">
              <Button text="跳过配置" size="small" variant="ghost" onPress={() => void wizardAction("skip", "")} />
              <Button
                text="下一步"
                size="small"
                variant="primary"
                onPress={() => void wizardAction("nextPlatform", "")}
              />
            </View>
          </View>
        </If>

        {/* Step: browser / install */}
        <If when={st().step === "browser"}>
          <View class="gap-3">
            <Text class="text-xs font-medium text-slate-900 dark:text-slate-100">
              {st().planTitle || "准备项目管控的浏览器环境"}
            </Text>
            <If when={Boolean(st().planDetail)}>
              <Text class="text-xs text-slate-500 dark:text-slate-400">{st().planDetail}</Text>
            </If>
            <Tabs
              variant="segmented"
              size="small"
              options={[
                { value: "Camoufox", label: "Camoufox (默认)" },
                { value: "Chrome CDP", label: "Chrome CDP (专用)" },
              ]}
              value={st().planBackend || "Camoufox"}
              onChange={({ value }) => void wizardAction("selectEngine", value)}
            />
            <If when={Boolean(st().planNeedsDL && !st().camBusy)}>
              <Button
                text="立即下载托管环境"
                size="small"
                icon="download"
                variant="primary"
                onPress={() => void wizardAction("installBrowser", "")}
              />
            </If>
            <If when={Boolean(st().camBusy)}>
              <View class="gap-1">
                <Text class="text-xs text-blue-600 dark:text-blue-400">
                  {`${st().camStatus || "正在下载"} · ${Math.round(st().camPercent)}%`}
                </Text>
                <Progress value={Math.min(1, st().camPercent / 100)} />
              </View>
            </If>
            <If when={Boolean(st().planBlocked || st().err)}>
              <Text class="text-xs text-amber-600 dark:text-amber-400">
                {st().planBlocked || st().err}
              </Text>
            </If>
            <View class="flex-row justify-between pt-2">
              <Button text="上一步" size="small" variant="ghost" onPress={() => void wizardAction("back", "")} />
              <View class="flex-row gap-2">
                <Button text="跳过" size="small" variant="ghost" onPress={() => void wizardAction("skip", "")} />
                <Button
                  text="下一步"
                  size="small"
                  variant="primary"
                  disabled={!st().planCanNext}
                  onPress={() => void wizardAction("nextBrowser", "")}
                />
              </View>
            </View>
          </View>
        </If>

        {/* Step: install (Antigravity agy CLI) */}
        <If when={st().step === "install"}>
          <View class="gap-3">
            <Text class="text-xs font-medium text-slate-900 dark:text-slate-100">
              {st().agyFound ? `已检测到 agy：${st().agyPath}` : "检测或安装 Antigravity (agy)"}
            </Text>
            <If when={Boolean(st().agyCmd)}>
              <View class="gap-1.5 rounded-lg bg-slate-50 p-2.5 dark:bg-[#1A2232]">
                <Text class="text-xs text-slate-600 dark:text-slate-300">{st().agyCmd}</Text>
                <View class="flex-row gap-2">
                  <Button
                    text={st().agyCopied ? "已复制命令" : "复制安装命令"}
                    size="small"
                    variant="outline"
                    onPress={() => {
                      void wizardAction("copyAgyCmd", "");
                      if (st().agyCmd) copyText(st().agyCmd!);
                    }}
                  />
                  <If when={Boolean(st().agyPageURL)}>
                    <Button
                      text="安装说明"
                      size="small"
                      variant="ghost"
                      onPress={() => void openUrl(st().agyPageURL!)}
                    />
                  </If>
                </View>
              </View>
            </If>
            <View class="flex-row flex-wrap gap-2">
              <Button text="重新检测" size="small" variant="outline" onPress={() => void wizardAction("checkAgy", "")} />
              <Button text="手动选择路径" size="small" variant="outline" onPress={() => void wizardAction("pickAgyBin", "")} />
            </View>
            <View class="flex-row justify-between pt-2">
              <Button text="上一步" size="small" variant="ghost" onPress={() => void wizardAction("back", "")} />
              <Button
                text="下一步：添加账号"
                size="small"
                variant="primary"
                disabled={!st().agyFound}
                onPress={() => void wizardAction("enterAgyAccount", "")}
              />
            </View>
          </View>
        </If>

        {/* Step: account */}
        <If when={st().step === "account"}>
          <View class="gap-3">
            <Text class="text-xs font-medium text-slate-900 dark:text-slate-100">
              {`登录 ${st().choice || ""} 账号${st().accountName ? ` (${st().accountName})` : ""}`}
            </Text>
            <If when={Boolean(st().note || st().err)}>
              <Text class="text-xs text-slate-600 dark:text-slate-300">{st().err || st().note}</Text>
            </If>

            <If when={Boolean(st().agyLoginOn)}>
              <View class="gap-2 rounded-lg bg-slate-50 p-3 dark:bg-[#1A2232]">
                <View class="flex-row gap-2">
                  <Button
                    text="在 Camoufox 打开授权"
                    size="small"
                    variant="outline"
                    onPress={() => void wizardAction("recheck", "")}
                  />
                  <If when={Boolean(st().agyLogin.url)}>
                    <Button
                      text="复制链接"
                      size="small"
                      variant="ghost"
                      onPress={() => {
                        copyText(st().agyLogin.url!);
                        showToast("已复制授权链接", "success");
                      }}
                    />
                  </If>
                </View>
                <Input
                  value={agyCode()}
                  placeholder="粘贴授权回调或验证码..."
                  onChange={({ value }) => agyCode.set(value)}
                />
              </View>
            </If>

            <View class="flex-row justify-between pt-2">
              <Button text="上一步" size="small" variant="ghost" onPress={() => void wizardAction("back", "")} />
              <View class="flex-row gap-2">
                <Button text="刷新登录状态" size="small" variant="outline" onPress={() => void wizardAction("recheck", "")} />
                <Button text="完成配置" size="small" variant="primary" onPress={() => finishWizard()} />
              </View>
            </View>
          </View>
        </If>

        {/* Step: done */}
        <If when={st().step === "done"}>
          <View class="gap-3">
            <Badge text="配置已完成" variant="up" />
            <Text class="text-xs text-slate-700 dark:text-slate-200">
              {st().note || "您现在可以开始使用图来批量生图、洗图与裂变工作台。"}
            </Text>
            <View class="flex-row justify-end gap-2">
              <Button text="继续添加账号" size="small" variant="outline" onPress={() => void wizardAction("addMore", "")} />
              <Button text="进入工作台" size="small" variant="primary" onPress={() => finishWizard()} />
            </View>
          </View>
        </If>
      </View>
    </Dialog>
  );
}
