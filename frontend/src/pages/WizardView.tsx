import { BentoCard, InsetPillRow, PillToggle } from "@/components/primitives";
import { Badge, Button, Dialog, Progress } from "@/components/ui";
import { normalizeInstallProgress } from "@/lib/browserRuntime";
import { cn } from "@/lib/cn";
import { closeWizardModal, runWizardAction, useWizard } from "@/store";

const PLATFORMS = ["豆包", "千问", "千问国际", "Gemini", "即梦"];

export function WizardView() {
  const st = useWizard();
  const dlProgress = normalizeInstallProgress(st.runtimePercent);

  return (
    <Dialog
      open={Boolean(st.on)}
      onOpenChange={(open) => {
        if (!open) void closeWizardModal();
      }}
      title="快速配置向导"
      description="三步完成平台选择、内置浏览器准备与账号登录"
      width={640}
      closeOnEscape
      data-testid="wizard-dialog-surface"
      bodyClassName="p-3 bg-paper"
    >
      <div
        data-testid="wizard-dialog"
        className="flex w-full min-w-0 flex-col gap-3"
      >
        {/* Stepper pill bar */}
        <div className="flex flex-wrap items-center gap-1.5 rounded-[16px] border border-hairline bg-card p-2.5">
          {(st.steps ?? []).map((step, idx) => {
            const state = step.state;
            return (
              <div
                key={step.id || step.title || idx}
                className={cn(
                  "tnum inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs transition-colors duration-200",
                  state === "current"
                    ? "bg-accent font-semibold text-on-accent"
                    : state === "done"
                      ? "bg-[var(--status-ok-bg)] font-medium text-status-ok"
                      : "bg-sunken font-medium text-muted",
                )}
              >
                {`${idx + 1}. ${step.title || step.id}`}
              </div>
            );
          })}
        </div>

        <div key={st.step || "platform"} className="animate-tulai-panel-in flex flex-col">
        {/* Step: platform */}
        {st.step === "platform" || !st.step ? (
          <BentoCard
            compact
            icon="sparkles"
            title="选择主要出图平台"
            subtitle="后续仍可随时在工作台侧边栏切换"
          >
            <div className="flex flex-col gap-3">
              <InsetPillRow className="flex-row flex-wrap gap-2">
                {PLATFORMS.map((plat) => (
                  <PillToggle
                    key={plat}
                    label={plat}
                    active={st.choice === plat}
                    onClick={() => void runWizardAction("choose", plat)}
                  />
                ))}
              </InsetPillRow>
              <div className="flex justify-end gap-2">
                <Button
                  text="跳过配置"
                  size="small"
                  variant="ghost"
                  onClick={() => void runWizardAction("skip", "")}
                />
                <Button
                  text="下一步"
                  size="small"
                  variant="primary"
                  onClick={() => void runWizardAction("nextPlatform", "")}
                />
              </div>
            </div>
          </BentoCard>
        ) : null}

        {/* Step: browser / install */}
        {st.step === "browser" ? (
          <BentoCard
            compact
            icon="globe"
            title={st.planTitle || "准备内置浏览器"}
            subtitle={st.planDetail || undefined}
          >
            <div className="flex flex-col gap-3">
              <InsetPillRow className="gap-2.5">
                <div className="flex items-center gap-2">
                  <Badge text={st.planBackend || "Fingerprint"} variant="neutral" />
                </div>
                {st.planNeedsDl && !st.runtimeBusy ? (
                  <div className="flex">
                    <Button
                      text="下载内置浏览器"
                      size="small"
                      icon="download"
                      variant="primary"
                      onClick={() => void runWizardAction("installBrowser", "")}
                    />
                  </div>
                ) : null}
                {st.runtimeBusy ? (
                  <div className="flex flex-col gap-1">
                    <span className="tnum text-xs text-accent">
                      {dlProgress.indeterminate
                        ? st.runtimeStatus || "正在下载"
                        : `${st.runtimeStatus || "正在下载"} · ${dlProgress.labelPct}%`}
                    </span>
                    <Progress
                      value={dlProgress.fraction}
                      indeterminate={dlProgress.indeterminate}
                    />
                  </div>
                ) : null}
                {st.planBlocked || st.err ? (
                  <span className="text-xs text-status-warn">
                    {st.planBlocked || st.err}
                  </span>
                ) : null}
              </InsetPillRow>

              <div className="flex items-center justify-between">
                <Button
                  text="上一步"
                  size="small"
                  variant="ghost"
                  onClick={() => void runWizardAction("back", "")}
                />
                <div className="flex items-center gap-2">
                  <Button
                    text="跳过"
                    size="small"
                    variant="ghost"
                    onClick={() => void runWizardAction("skip", "")}
                  />
                  <Button
                    text="下一步"
                    size="small"
                    variant="primary"
                    disabled={!st.planCanNext}
                    onClick={() => void runWizardAction("nextBrowser", "")}
                  />
                </div>
              </div>
            </div>
          </BentoCard>
        ) : null}

        {/* Step: account */}
        {st.step === "account" ? (
          <BentoCard
            compact
            icon="key"
            title={`登录 ${st.choice || ""} 账号${st.accountName ? ` (${st.accountName})` : ""}`}
            subtitle={st.err || st.note || undefined}
          >
            <div className="flex items-center justify-between">
              <Button
                text="上一步"
                size="small"
                variant="ghost"
                onClick={() => void runWizardAction("back", "")}
              />
              <div className="flex items-center gap-2">
                <Button
                  text="刷新登录状态"
                  size="small"
                  variant="outline"
                  onClick={() => void runWizardAction("recheck", "")}
                />
                <Button
                  text="完成配置"
                  size="small"
                  variant="primary"
                  onClick={() => void closeWizardModal()}
                />
              </div>
            </div>
          </BentoCard>
        ) : null}

        {/* Step: done */}
        {st.step === "done" ? (
          <BentoCard compact icon="circle-check" title="配置完成">
            <div className="flex flex-col gap-3">
              <InsetPillRow className="gap-2">
                <div className="flex">
                  <Badge text="配置已完成" variant="success" />
                </div>
                <span className="text-xs text-ink">
                  {st.note || "已就绪，可以开始批量洗图、自由生图与裂变了。"}
                </span>
              </InsetPillRow>
              <div className="flex justify-end gap-2">
                <Button
                  text="继续添加账号"
                  size="small"
                  variant="outline"
                  onClick={() => void runWizardAction("addMore", "")}
                />
                <Button
                  text="进入工作台"
                  size="small"
                  variant="primary"
                  onClick={() => void closeWizardModal()}
                />
              </div>
            </div>
          </BentoCard>
        ) : null}
        </div>
      </div>
    </Dialog>
  );
}
