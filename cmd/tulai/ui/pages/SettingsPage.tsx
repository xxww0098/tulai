import { Scroll, Text, View } from "nsgo";
import { Badge, Button, Checkbox, Input, Progress, Select, Switch, Tabs } from "nsgo/ui";
import {
  pickSettingDir,
  pickSettingFile,
  saveSettingsNow,
  setSettingField,
  updateAction,
} from "tulai/backend";
import { FormRow, PageToolbar, SectionCard } from "../components/primitives";
import { settings, shell, updateSettingField } from "../store";

const SETTINGS_TABS = [
  { value: "0", label: "常规与输出" },
  { value: "1", label: "账号与任务" },
  { value: "2", label: "网络与高级" },
  { value: "3", label: "关于与更新" },
];

const STRATEGY_OPTIONS = [
  { value: "round-robin", label: "轮询均衡" },
  { value: "first-healthy", label: "优先主力" },
  { value: "least-used", label: "最少使用优先" },
];

const CAPTCHA_BACKENDS = [
  { value: "manual", label: "弹窗人工处理" },
  { value: "custom", label: "自定义视觉 API" },
  { value: "ikunocr", label: "iKunOCR 服务" },
];

const UPDATE_MIRRORS = [
  { value: "", label: "GitHub 直连" },
  { value: "gh-proxy", label: "镜像加速 (gh-proxy)" },
];

export function SettingsPage() {
  const st = settings;
  const upd = () => shell().update;

  return (
    <View class="min-h-0 flex-1">
      <PageToolbar
        left={
          <Tabs
            testID="settings-tabs"
            variant="segmented"
            size="small"
            options={SETTINGS_TABS}
            value={String(st().tab)}
            onChange={({ value }) => updateSettingField("tab", value)}
          />
        }
        right={
          <View class="flex-row items-center gap-2">
            <Button
              testID="settings-save-btn"
              text="立即保存"
              size="small"
              icon="save"
              variant="primary"
              onPress={() => saveSettingsNow()}
            />
          </View>
        }
      />

      <Scroll class="min-h-0 flex-1" contentClass="items-center p-3 pb-6">
        <View class="w-full max-w-[780px] gap-3">
          {/* Tab 0: 常规与输出 */}
          <If when={st().tab === 0}>
            <View class="gap-3">
              <SectionCard compact title="输出目录与命名序号" subtitle={st().resolvedOutput}>
                <View class="gap-3">
                  <FormRow label="图片输出目录">
                    <View class="flex-row items-center gap-2">
                      <Input
                        value={st().outputPath || ""}
                        placeholder="留空则使用默认 输出结果 文件夹"
                        onChange={({ value }) => setSettingField("outputPath", value)}
                        class="flex-1"
                      />
                      <Button text="浏览" size="small" variant="outline" onPress={() => void pickSettingDir("outputPath")} />
                    </View>
                  </FormRow>

                  <FormRow label="自动编号开关">
                    <View class="flex-row flex-wrap items-center gap-4">
                      <Checkbox
                        text="自由生图自动编号"
                        checked={Boolean(st().genAutoNumber)}
                        onChange={({ checked }) =>
                          setSettingField("genAutoNumber", checked ? "true" : "false")
                        }
                      />
                      <Checkbox
                        text="批量洗图自动编号"
                        checked={Boolean(st().washAutoNumber)}
                        onChange={({ checked }) =>
                          setSettingField("washAutoNumber", checked ? "true" : "false")
                        }
                      />
                    </View>
                  </FormRow>

                  <FormRow label="编号格式">
                    <View class="flex-row flex-wrap items-center gap-2">
                      <Input
                        value={st().numPrefix || ""}
                        placeholder="前缀"
                        onChange={({ value }) => setSettingField("numPrefix", value)}
                        class="w-[150px]"
                      />
                      <Text class="text-xs text-slate-500">起始</Text>
                      <Input
                        value={String(st().numStart)}
                        onChange={({ value }) => setSettingField("numStart", value)}
                        class="w-[74px]"
                      />
                      <Text class="text-xs text-slate-500">位数</Text>
                      <Input
                        value={String(st().numWidth)}
                        onChange={({ value }) => setSettingField("numWidth", value)}
                        class="w-[74px]"
                      />
                    </View>
                  </FormRow>
                </View>
              </SectionCard>

              <SectionCard compact title="网页下载默认参数" subtitle={st().resolvedDlDir}>
                <View class="gap-3">
                  <FormRow label="下载保存目录">
                    <View class="flex-row items-center gap-2">
                      <Input
                        value={st().dlDir || ""}
                        onChange={({ value }) => setSettingField("dlDir", value)}
                        class="flex-1"
                      />
                      <Button text="浏览" size="small" variant="outline" onPress={() => void pickSettingDir("dlDir")} />
                    </View>
                  </FormRow>

                  <FormRow label="速率与重试">
                    <View class="flex-row flex-wrap items-center gap-2.5">
                      <Text class="text-xs text-slate-500">并发</Text>
                      <Input
                        value={String(st().downloadConc)}
                        onChange={({ value }) => setSettingField("downloadConc", value)}
                        class="w-[68px]"
                      />
                      <Text class="text-xs text-slate-500">重试</Text>
                      <Input
                        value={String(st().downloadRetries)}
                        onChange={({ value }) => setSettingField("downloadRetries", value)}
                        class="w-[68px]"
                      />
                      <Text class="text-xs text-slate-500">限速(KB/s)</Text>
                      <Input
                        value={String(st().downloadRateKB)}
                        onChange={({ value }) => setSettingField("downloadRateKB", value)}
                        class="w-[82px]"
                      />
                    </View>
                  </FormRow>
                </View>
              </SectionCard>
            </View>
          </If>

          {/* Tab 1: 账号与任务 */}
          <If when={st().tab === 1}>
            <View class="gap-3">
              <SectionCard compact title="队列并发与发送控制">
                <View class="gap-3">
                  <FormRow label="自动化开关">
                    <View class="flex-row flex-wrap items-center gap-4">
                      <View class="flex-row items-center gap-2">
                        <Switch
                          size="small"
                          checked={Boolean(st().concurrencyAuto)}
                          onChange={({ checked }) =>
                            setSettingField("concurrencyAuto", checked ? "true" : "false")
                          }
                        />
                        <Text class="text-xs text-slate-700 dark:text-slate-200">自动并发</Text>
                      </View>
                      <View class="flex-row items-center gap-2">
                        <Switch
                          size="small"
                          checked={Boolean(st().removeWatermark)}
                          onChange={({ checked }) =>
                            setSettingField("removeWatermark", checked ? "true" : "false")
                          }
                        />
                        <Text class="text-xs text-slate-700 dark:text-slate-200">去结果水印</Text>
                      </View>
                      <View class="flex-row items-center gap-2">
                        <Switch
                          size="small"
                          checked={Boolean(st().taskBrowserVisible)}
                          onChange={({ checked }) =>
                            setSettingField("taskBrowserVisible", checked ? "true" : "false")
                          }
                        />
                        <Text class="text-xs text-slate-700 dark:text-slate-200">显示任务浏览器</Text>
                      </View>
                    </View>
                  </FormRow>

                  <FormRow label="账号调度策略">
                    <View class="flex-row flex-wrap items-center gap-2.5">
                      <View class="w-[152px]">
                        <Select
                          options={STRATEGY_OPTIONS}
                          value={st().sendStrategy || "round-robin"}
                          onChange={({ value }) => setSettingField("sendStrategy", value)}
                        />
                      </View>
                      <Text class="text-xs text-slate-500">重试</Text>
                      <Input
                        value={String(st().retryCount)}
                        onChange={({ value }) => setSettingField("retryCount", value)}
                        class="w-[62px]"
                      />
                      <Text class="text-xs text-slate-500">间隔(秒)</Text>
                      <Input
                        value={String(st().sendWait)}
                        onChange={({ value }) => setSettingField("sendWait", value)}
                        class="w-[62px]"
                      />
                      <Text class="text-xs text-slate-500">超时(秒)</Text>
                      <Input
                        value={String(st().waitResultSeconds)}
                        onChange={({ value }) => setSettingField("waitResultSeconds", value)}
                        class="w-[70px]"
                      />
                    </View>
                  </FormRow>

                  <FormRow label="风险过滤词" hint="每行或逗号分隔">
                    <Input
                      value={st().riskyWords || ""}
                      onChange={({ value }) => setSettingField("riskyWords", value)}
                    />
                  </FormRow>
                </View>
              </SectionCard>

              <SectionCard compact title="平台每日出图上限与模型">
                <View class="gap-3">
                  <FormRow label="单日账号上限">
                    <View class="flex-row flex-wrap items-center gap-2.5">
                      <Text class="text-xs text-slate-500">豆包</Text>
                      <Input
                        value={String(st().dailyCapDoubao)}
                        onChange={({ value }) => setSettingField("dailyCapDoubao", value)}
                        class="w-[68px]"
                      />
                      <Text class="text-xs text-slate-500">千问</Text>
                      <Input
                        value={String(st().dailyCapQianwen)}
                        onChange={({ value }) => setSettingField("dailyCapQianwen", value)}
                        class="w-[68px]"
                      />
                      <Text class="text-xs text-slate-500">Antigravity</Text>
                      <Input
                        value={String(st().dailyCapAgy)}
                        onChange={({ value }) => setSettingField("dailyCapAgy", value)}
                        class="w-[68px]"
                      />
                      <Text class="text-xs text-slate-500">ChatGPT</Text>
                      <Input
                        value={String(st().dailyCapChatGPT)}
                        onChange={({ value }) => setSettingField("dailyCapChatGPT", value)}
                        class="w-[68px]"
                      />
                    </View>
                  </FormRow>

                  <FormRow label="默认生成模型">
                    <View class="flex-row flex-wrap gap-2">
                      <Input
                        value={st().chatGPTModel || ""}
                        placeholder="ChatGPT 模型（默认）"
                        onChange={({ value }) => setSettingField("chatGPTModel", value)}
                        class="min-w-[180px] flex-1"
                      />
                      <Input
                        value={st().agyModel || ""}
                        placeholder="Antigravity 模型（默认）"
                        onChange={({ value }) => setSettingField("agyModel", value)}
                        class="min-w-[180px] flex-1"
                      />
                    </View>
                  </FormRow>

                  <FormRow label="agy 可执行文件">
                    <View class="flex-row items-center gap-2">
                      <Input
                        value={st().agyBin || ""}
                        placeholder="自动探测或指定 agy 路径"
                        onChange={({ value }) => setSettingField("agyBin", value)}
                        class="flex-1"
                      />
                      <Button text="浏览" size="small" variant="outline" onPress={() => void pickSettingFile("agyBin")} />
                    </View>
                  </FormRow>

                  <FormRow label="agy 数据根目录">
                    <View class="flex-row items-center gap-2">
                      <Input
                        value={st().agyHomeRoot || ""}
                        onChange={({ value }) => setSettingField("agyHomeRoot", value)}
                        class="flex-1"
                      />
                      <Button text="浏览" size="small" variant="outline" onPress={() => void pickSettingDir("agyHomeRoot")} />
                    </View>
                  </FormRow>
                </View>
              </SectionCard>
            </View>
          </If>

          {/* Tab 2: 网络与高级 */}
          <If when={st().tab === 2}>
            <View class="gap-3">
              <SectionCard compact title="代理设置" subtitle="同时作用于托管浏览器与网页下载（默认关闭即直连）">
                <View class="gap-3">
                  <FormRow label="自定义代理">
                    <View class="flex-row items-center gap-3">
                      <Switch
                        size="small"
                        checked={Boolean(st().proxyCustom)}
                        onChange={({ checked }) =>
                          setSettingField("proxyCustom", checked ? "true" : "false")
                        }
                      />
                      <Text class="text-xs text-slate-700 dark:text-slate-200">启用自定义代理</Text>
                    </View>
                  </FormRow>
                  <FormRow label="代理地址">
                    <Input
                      value={st().proxyURL || ""}
                      placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:7891"
                      disabled={!st().proxyCustom}
                      onChange={({ value }) => setSettingField("proxyURL", value)}
                    />
                  </FormRow>
                </View>
              </SectionCard>

              <SectionCard compact title="验证码自动识别（高级）" subtitle="可选开启视觉模型辅助通过滑块/点选验证">
                <View class="gap-3">
                  <FormRow label="识别方式">
                    <View class="flex-row items-center gap-3">
                      <Switch
                        size="small"
                        checked={Boolean(st().captchaEnabled)}
                        onChange={({ checked }) =>
                          setSettingField("captchaEnabled", checked ? "true" : "false")
                        }
                      />
                      <Text class="text-xs text-slate-700 dark:text-slate-200">启用自动识别</Text>
                      <View class="w-[180px]">
                        <Select
                          options={CAPTCHA_BACKENDS}
                          value={st().captchaBackend || "manual"}
                          onChange={({ value }) => setSettingField("captchaBackend", value)}
                        />
                      </View>
                    </View>
                  </FormRow>

                  <If when={st().captchaBackend === "custom"}>
                    <FormRow label="自定义视觉接口">
                      <View class="gap-2">
                        <Input
                          value={st().customURL || ""}
                          placeholder="视觉 API Base URL"
                          onChange={({ value }) => setSettingField("customURL", value)}
                        />
                        <View class="flex-row gap-2">
                          <Input
                            value={st().customKey || ""}
                            placeholder="API Key"
                            masked
                            onChange={({ value }) => setSettingField("customKey", value)}
                            class="flex-1"
                          />
                          <Input
                            value={st().customModel || ""}
                            placeholder="模型名称"
                            onChange={({ value }) => setSettingField("customModel", value)}
                            class="w-[180px]"
                          />
                        </View>
                      </View>
                    </FormRow>
                  </If>

                  <If when={st().captchaBackend === "ikunocr"}>
                    <FormRow label="iKunOCR 密钥">
                      <Input
                        value={st().ikunocrKey || ""}
                        placeholder="iKunOCR Token"
                        masked
                        onChange={({ value }) => setSettingField("ikunocrKey", value)}
                      />
                    </FormRow>
                  </If>
                </View>
              </SectionCard>
            </View>
          </If>

          {/* Tab 3: 关于与更新 */}
          <If when={st().tab === 3}>
            <SectionCard
              compact
              title="关于与版本更新"
              subtitle={`当前版本 v${st().version || upd().current || "dev"} · GitHub Releases 自动校验安装`}
              right={
                <View class="flex-row items-center gap-2">
                  <Button
                    text={upd().checking ? "检查中..." : "检查更新"}
                    size="small"
                    icon="refresh-cw"
                    variant="outline"
                    busy={Boolean(upd().checking)}
                    onPress={() => updateAction("check")}
                  />
                  <If when={Boolean(upd().version)}>
                    <Button
                      text={upd().downloading ? "下载更新中..." : `更新到 v${upd().version}`}
                      size="small"
                      icon="download"
                      variant="primary"
                      busy={Boolean(upd().downloading)}
                      onPress={() => updateAction("install")}
                    />
                  </If>
                </View>
              }
            >
              <View class="gap-3">
                <FormRow label="更新策略">
                  <View class="flex-row flex-wrap items-center gap-4">
                    <Checkbox
                      text="启动时自动检查更新"
                      checked={Boolean(st().autoCheck)}
                      onChange={({ checked }) =>
                        setSettingField("autoCheck", checked ? "true" : "false")
                      }
                    />
                    <View class="w-[210px]">
                      <Select
                        options={UPDATE_MIRRORS}
                        value={st().updateMirror || ""}
                        onChange={({ value }) => setSettingField("updateMirror", value)}
                      />
                    </View>
                  </View>
                </FormRow>

                <If when={Boolean(upd().downloading)}>
                  <View class="gap-1">
                    <Text class="text-xs text-blue-600 dark:text-blue-400">
                      {upd().progressText || "正在下载更新包并验证校验和..."}
                    </Text>
                    <Progress value={Math.min(1, Math.max(0, upd().fraction))} />
                  </View>
                </If>

                <If when={Boolean(st().updateNotice || upd().installed)}>
                  <Badge text={st().updateNotice || `已准备更新 ${upd().installed}`} variant="up" />
                </If>
                <If when={Boolean(st().updateErr || upd().err)}>
                  <Text class="text-xs text-rose-600 dark:text-rose-400">
                    {st().updateErr || upd().err}
                  </Text>
                </If>
                <If when={Boolean(upd().notes)}>
                  <View class="rounded-lg bg-slate-50 p-3 dark:bg-[#1A2232]">
                    <Text class="text-xs text-slate-700 dark:text-slate-200">{upd().notes}</Text>
                  </View>
                </If>
              </View>
            </SectionCard>
          </If>
        </View>
      </Scroll>
    </View>
  );
}
