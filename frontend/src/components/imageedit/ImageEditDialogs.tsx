import { useCallback, useEffect, useState } from "react";
import { EmptyNotice, InsetPillRow } from "@/components/primitives";
import { Badge, Button, Dialog, Icon, Input, Tooltip } from "@/components/ui";
import { cn } from "@/lib/cn";
import type { EditFileDTO, EditRecipeDTO } from "@/store";

export interface ImageEditRecipeDialogProps {
  open: boolean;
  recipes: EditRecipeDTO[];
  recipeErr?: string;
  onClose: () => void;
  onSaveRecipe: (name: string) => void;
  onApplyRecipe: (idx: number) => void;
  onDeleteRecipe: (idx: number) => void;
}

export function ImageEditRecipeDialog({
  open,
  recipes,
  recipeErr,
  onClose,
  onSaveRecipe,
  onApplyRecipe,
  onDeleteRecipe,
}: ImageEditRecipeDialogProps) {
  const [recipeName, setRecipeName] = useState("");

  useEffect(() => {
    if (!open) {
      setRecipeName("");
    }
  }, [open]);

  const handleSave = useCallback(() => {
    const trimmed = recipeName.trim();
    if (!trimmed) return;
    onSaveRecipe(trimmed);
    setRecipeName("");
  }, [recipeName, onSaveRecipe]);

  return (
    <Dialog
      data-testid="edit-recipe-dialog"
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
      title="保存与套用精修配方"
      width={500}
    >
      <div className="flex w-full min-w-0 flex-col gap-3">
        <div className="flex items-center gap-2">
          <Input
            size="small"
            aria-label="新配方名称"
            placeholder="输入新配方名称..."
            value={recipeName}
            onChange={(e) => setRecipeName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.nativeEvent.isComposing) handleSave();
            }}
            className="flex-1"
          />
          <Button
            text="保存当前"
            size="small"
            variant="primary"
            disabled={recipeName.trim() === ""}
            onClick={handleSave}
          />
        </div>

        {recipeErr ? (
          <div className="text-xs text-status-danger">{recipeErr}</div>
        ) : null}

        {recipes.length === 0 ? (
          <EmptyNotice
            icon="save"
            title="暂无精修配方"
            subtitle="配置好裁剪、调色或格式参数后，输入名称即可保存为可复用配方"
            className="py-5"
          />
        ) : (
          <div className="flex max-h-[280px] flex-col gap-1.5 overflow-y-auto pr-0.5">
            {recipes.map((recipe) => (
              <InsetPillRow
                key={recipe.index}
                className="flex-row items-center justify-between gap-2 py-2"
              >
                <span className="min-w-0 flex-1 truncate text-xs font-medium text-ink">
                  {recipe.name}
                </span>
                <div className="flex shrink-0 items-center gap-1">
                  <Button
                    text="套用"
                    size="xs"
                    variant="outline"
                    onClick={() => onApplyRecipe(recipe.index)}
                  />
                  <Button
                    text="删除"
                    size="xs"
                    variant="ghost"
                    onClick={() => onDeleteRecipe(recipe.index)}
                  />
                </div>
              </InsetPillRow>
            ))}
          </div>
        )}
      </div>
    </Dialog>
  );
}

export interface ImageEditResultDialogProps {
  open: boolean;
  okCount: number;
  failCount: number;
  outDir?: string;
  failedFiles: EditFileDTO[];
  running?: boolean;
  onClose: () => void;
  onRetryFailed: () => void;
  onPickFailed?: () => void;
  onOpenOutDir: () => void;
}

export function ImageEditResultDialog({
  open,
  okCount,
  failCount,
  outDir,
  failedFiles,
  running = false,
  onClose,
  onRetryFailed,
  onPickFailed,
  onOpenOutDir,
}: ImageEditResultDialogProps) {
  const displayOutDir = outDir?.trim() || "原图所在目录";

  return (
    <Dialog
      data-testid="edit-result-dialog"
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
      title="批量导出完成"
      description={`成功 ${okCount} 张 · 失败 ${failCount} 张`}
      width={500}
      footer={
        <>
          {failCount > 0 && onPickFailed ? (
            <Button
              text="选中失败图片"
              size="small"
              variant="outline"
              onClick={onPickFailed}
            />
          ) : null}
          {failCount > 0 ? (
            <Button
              text="重试失败"
              size="small"
              variant="outline"
              disabled={running}
              onClick={onRetryFailed}
            />
          ) : null}
          <Button
            text="打开输出目录"
            size="small"
            variant="outline"
            onClick={onOpenOutDir}
          />
          <Button
            text="完成"
            size="small"
            variant="primary"
            onClick={onClose}
          />
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="grid grid-cols-2 gap-2.5">
          <InsetPillRow className="gap-0.5 py-2.5">
            <span className="text-[11px] text-muted">导出成功</span>
            <span className="tnum text-lg font-semibold text-status-ok">
              {okCount}
            </span>
          </InsetPillRow>
          <InsetPillRow className="gap-0.5 py-2.5">
            <span className="text-[11px] text-muted">导出失败</span>
            <span
              className={cn(
                "tnum text-lg font-semibold",
                failCount > 0 ? "text-status-danger" : "text-muted",
              )}
            >
              {failCount}
            </span>
          </InsetPillRow>
        </div>

        <InsetPillRow className="flex-row items-center gap-2 py-2">
          <Icon name="folder-open" size={14} className="shrink-0 text-subtle" />
          <div className="min-w-0 flex-1">
            <div className="text-[10.5px] font-medium text-subtle">输出位置</div>
            <Tooltip content={displayOutDir} side="top">
              <div
                tabIndex={0}
                className="font-mono-console truncate rounded text-[11px] text-ink select-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
              >
                {displayOutDir}
              </div>
            </Tooltip>
          </div>
        </InsetPillRow>

        {failedFiles.length > 0 ? (
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between text-[11px]">
              <span className="font-medium text-ink">失败项目详情</span>
              <Badge variant="danger" text={`${failedFiles.length} 张`} />
            </div>
            <div className="flex max-h-[180px] flex-col gap-1.5 overflow-y-auto rounded-[12px] border border-hairline bg-sunken p-2.5">
              {failedFiles.map((f) => (
                <div
                  key={f.path || f.index}
                  className="flex items-start justify-between gap-2 rounded-[8px] border border-divider bg-card px-2.5 py-1.5 text-[11px]"
                >
                  <div className="flex min-w-0 items-center gap-1.5">
                    <span className="tnum shrink-0 text-[10px] text-subtle">
                      {`#${f.index + 1}`}
                    </span>
                    <Tooltip content={f.path || f.name} side="top">
                      <span
                        tabIndex={0}
                        className="truncate rounded font-medium text-ink focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                      >
                        {f.name}
                      </span>
                    </Tooltip>
                  </div>
                  <span className="max-w-[55%] text-right break-all text-status-danger">
                    {f.err || "处理失败"}
                  </span>
                </div>
              ))}
            </div>
          </div>
        ) : null}
      </div>
    </Dialog>
  );
}
