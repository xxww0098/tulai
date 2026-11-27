export interface QwenQuotaLike {
  platform: string;
  disabled?: boolean;
  creditsSynced?: boolean;
  creditsRemain?: number;
  creditsTotal?: number;
  creditCost?: number;
  creditsResetAt?: number;
  creditsResetLabel?: string;
  creditsCycle?: string;
  creditsCycleLabel?: string;
  memberTier?: string;
  memberLabel?: string;
  concurrencyLimit?: number;
  concurrencyLabel?: string;
  watermarkFree?: boolean;
  usagePercent?: number;
  platformLimited?: boolean;
  limitTips?: string;
  resetCardCount?: number;
  creditsSyncedAt?: number;
  creditsSyncedLabel?: string;
  usagePct?: number;
}

export interface DoubaoPoolSummary {
  totalAccounts: number;
  enabledAccounts: number;
  syncedAccounts: number;
  vipCount: number;
  paidRemainSum: number;
  paidTotalSum: number;
  limitedCount: number;
  maxUsagePercent: number;
  resetCardSum: number;
  earliestResetAt: number;
  earliestResetLabel: string;
}

export interface QwenPoolSummary {
  totalAccounts: number;
  enabledAccounts: number;
  syncedAccounts: number;
  remainSum: number;
  totalSum: number;
  exhaustedCount: number;
  vipCount: number;
  costPerImage: number;
  estImages: number;
  earliestResetAt: number;
  earliestResetLabel: string;
}

export interface GeminiPoolSummary {
  totalAccounts: number;
  enabledAccounts: number;
  syncedAccounts: number;
  meteredAccounts: number;
  avgRemainPct: number;
  minRemainPct: number;
  exhaustedCount: number;
  paidCount: number;
  aiCreditsSum: number;
  earliestResetAt: number;
  earliestResetLabel: string;
}

export function isDoubaoAccount(item: Pick<QwenQuotaLike, "platform">): boolean {
  return item.platform === "豆包";
}

export function isQwenAccount(item: Pick<QwenQuotaLike, "platform">): boolean {
  return item.platform === "千问";
}

export function isGeminiAccount(item: Pick<QwenQuotaLike, "platform">): boolean {
  return item.platform === "Gemini";
}

export function isQuotaTrackedAccount(item: Pick<QwenQuotaLike, "platform">): boolean {
  return isQwenAccount(item) || isGeminiAccount(item) || isDoubaoAccount(item);
}

export function doubaoStatusTone(item: QwenQuotaLike): "danger" | "warn" | "ok" | "muted" {
  if (!item.creditsSynced) return "muted";
  const remain = item.creditsRemain ?? -1;
  if (item.platformLimited || remain === 0) return "danger";
  const usagePercent = item.usagePercent ?? -1;
  if (usagePercent >= 80 || (item.usagePct ?? 0) >= 80) return "warn";
  return "ok";
}

export function doubaoProgressTone(item: QwenQuotaLike): "danger" | "warn" | "accent" {
  const tone = doubaoStatusTone(item);
  const usagePercent = item.usagePercent ?? -1;
  if (tone === "danger" || (item.usagePct ?? 0) >= 95 || usagePercent >= 95) return "danger";
  if (tone === "warn" || (item.usagePct ?? 0) >= 80 || usagePercent >= 80) return "warn";
  return "accent";
}

export function formatDoubaoPrimary(item: QwenQuotaLike): string {
  if (!item.creditsSynced) return "状态未同步";
  if (item.platformLimited) return "已触发限额";
  const remain = item.creditsRemain ?? -1;
  const total = item.creditsTotal ?? 0;
  if (remain >= 0 && total > 0) {
    return remain === 0 ? `0 / ${total} 额度 · 已耗尽` : `余 ${remain} / ${total} 额度`;
  }
  if (remain >= 0) {
    return remain === 0 ? "0 额度 · 已耗尽" : `余 ${remain} 额度`;
  }
  const usagePercent = item.usagePercent ?? -1;
  if (usagePercent > 0) {
    return `窗口已用 ${usagePercent}%`;
  }
  return "额度正常";
}

export function formatDoubaoCompactPill(item: QwenQuotaLike): string {
  if (!item.creditsSynced) return "";
  if (item.platformLimited) return "限额";
  const remain = item.creditsRemain ?? -1;
  if (remain >= 0) return `${remain}额`;
  const usagePercent = item.usagePercent ?? -1;
  if (usagePercent > 0) return `已用${usagePercent}%`;
  if (item.memberLabel && isPaidMemberTier(item.memberTier)) return item.memberLabel;
  return "正常";
}

export function formatDoubaoMetaLine(item: QwenQuotaLike, todayCount?: number): string {
  const parts: string[] = [];
  if (item.creditsSynced) {
    const remain = item.creditsRemain ?? -1;
    const usagePercent = item.usagePercent ?? -1;
    if (item.platformLimited) {
      if (item.creditsResetLabel) {
        parts.push(item.creditsResetLabel);
      } else if (item.limitTips) {
        parts.push(item.limitTips);
      }
    } else {
      if (item.creditsResetLabel) {
        parts.push(item.creditsResetLabel);
      }
      if (usagePercent >= 0 && (remain >= 0 || usagePercent === 0)) {
        parts.push(`窗口 ${usagePercent}%`);
      }
    }
    if ((item.resetCardCount ?? 0) > 0) {
      parts.push(`重置卡 ${item.resetCardCount} 张`);
    }
    if (typeof todayCount === "number" && todayCount > 0) {
      parts.push(`今日 ${todayCount} 次`);
    }
    if (parts.length === 0) {
      parts.push(item.memberLabel || "免费体验");
    }
  } else {
    parts.push("免费动态限额");
    if (typeof todayCount === "number" && todayCount > 0) {
      parts.push(`今日 ${todayCount} 次`);
    }
  }
  return parts.join(" · ");
}

export function buildDoubaoStatusTooltip(
  item: QwenQuotaLike,
  extra?: {
    todayCount?: number;
    successToday?: number;
    failedToday?: number;
    cooldownAt?: string;
  },
): string {
  const lines: string[] = [];
  if (item.creditsSynced) {
    const memberLabel = item.memberLabel || (isPaidMemberTier(item.memberTier) ? "豆包订阅" : "免费体验");
    lines.push(`订阅方案：${memberLabel}`);
    const remain = item.creditsRemain ?? -1;
    const total = item.creditsTotal ?? 0;
    if (remain >= 0 && total > 0) {
      lines.push(`订阅额度：剩余 ${remain} / ${total}`);
    } else if (remain >= 0) {
      lines.push(`订阅额度：剩余 ${remain}`);
    } else {
      lines.push("额度模式：免费版动态限额（页面不提供固定每日次数）");
    }
    if (item.platformLimited) {
      lines.push(`限额状态：已触发限额${item.limitTips ? `（${item.limitTips}）` : ""}`);
    } else if ((item.usagePercent ?? -1) >= 0) {
      lines.push(`窗口占用：${item.usagePercent}%`);
    }
    if (item.creditsResetLabel) {
      lines.push(`恢复/到期：${item.creditsResetLabel}`);
    }
    lines.push(`额度重置卡：${item.resetCardCount ?? 0} 张`);
    if (item.creditsSyncedLabel) {
      lines.push(`状态同步：${item.creditsSyncedLabel}`);
    }
  } else {
    lines.push("豆包实时状态尚未同步，点击「同步」或完成一次出图后自动读取");
    lines.push("同步内容：订阅方案、付费剩余额度 / 免费窗口占用比、限额与频控状态、额度重置卡");
  }
  if (extra) {
    if (typeof extra.todayCount === "number") {
      if (typeof extra.successToday === "number") {
        lines.push(`今日出图：已派发 ${extra.todayCount} 次 · 成功 ${extra.successToday} 次`);
      } else {
        lines.push(`今日派发：${extra.todayCount} 次`);
      }
    }
    if (extra.cooldownAt) {
      lines.push(`冷却恢复：预计 ${extra.cooldownAt}`);
    }
  }
  return lines.join("\n");
}

export function summarizeDoubaoPool(items: QwenQuotaLike[]): DoubaoPoolSummary {
  const doubao = items.filter(isDoubaoAccount);
  const enabled = doubao.filter((item) => !item.disabled);
  const scope = enabled.length > 0 ? enabled : doubao;
  let syncedAccounts = 0;
  let vipCount = 0;
  let paidRemainSum = 0;
  let paidTotalSum = 0;
  let limitedCount = 0;
  let maxUsagePercent = -1;
  let resetCardSum = 0;
  let earliestResetAt = 0;
  let earliestResetLabel = "";

  for (const item of scope) {
    if (!item.creditsSynced) continue;
    syncedAccounts += 1;
    const remain = item.creditsRemain ?? -1;
    const total = item.creditsTotal ?? 0;
    if (isPaidMemberTier(item.memberTier) || remain >= 0) {
      vipCount += 1;
      if (remain >= 0) paidRemainSum += remain;
      if (total > 0) paidTotalSum += total;
    }
    if (item.platformLimited || remain === 0) {
      limitedCount += 1;
    }
    const usagePercent = item.usagePercent ?? -1;
    if (usagePercent >= 0 && usagePercent > maxUsagePercent) {
      maxUsagePercent = usagePercent;
    }
    if ((item.resetCardCount ?? 0) > 0) {
      resetCardSum += item.resetCardCount ?? 0;
    }
    const resetAt = item.creditsResetAt ?? 0;
    if (resetAt > 0 && (earliestResetAt === 0 || resetAt < earliestResetAt)) {
      earliestResetAt = resetAt;
      earliestResetLabel = item.creditsResetLabel || "";
    } else if (!earliestResetLabel && item.creditsResetLabel) {
      earliestResetLabel = item.creditsResetLabel;
    }
  }

  return {
    totalAccounts: doubao.length,
    enabledAccounts: enabled.length,
    syncedAccounts,
    vipCount,
    paidRemainSum,
    paidTotalSum,
    limitedCount,
    maxUsagePercent,
    resetCardSum,
    earliestResetAt,
    earliestResetLabel,
  };
}

export function isGeminiPercentQuota(item: Pick<QwenQuotaLike, "creditsCycle">): boolean {
  return item.creditsCycle === "5h" || item.creditsCycle === "weekly";
}

export function isPaidMemberTier(tier?: string): boolean {
  const t = String(tier || "").trim().toLowerCase();
  return t === "vip" || t === "pro" || t === "ultra" || t === "plus";
}

export function geminiQuotaTone(item: QwenQuotaLike): "danger" | "warn" | "ok" | "muted" {
  if (!item.creditsSynced) return "muted";
  const remain = item.creditsRemain ?? -1;
  if (remain < 0) return "muted";
  if (remain === 0) return "danger";
  if ((item.usagePct ?? 0) >= 80 || (isGeminiPercentQuota(item) && remain <= 20)) return "warn";
  return "ok";
}

export function geminiProgressTone(item: QwenQuotaLike): "danger" | "warn" | "accent" {
  const tone = geminiQuotaTone(item);
  if (tone === "danger" || (item.usagePct ?? 0) >= 95) return "danger";
  if (tone === "warn" || (item.usagePct ?? 0) >= 80) return "warn";
  return "accent";
}

export function formatGeminiQuotaPrimary(item: QwenQuotaLike): string {
  if (!item.creditsSynced) return "额度未同步";
  const remain = item.creditsRemain ?? -1;
  const total = item.creditsTotal ?? 0;
  if (isGeminiPercentQuota(item) && remain >= 0) {
    return remain === 0 ? "剩余 0% · 已耗尽" : `余 ${remain}%`;
  }
  if (item.creditsCycle === "ai_credits" && remain >= 0) {
    return remain === 0 ? "0 AI积分 · 已耗尽" : `余 ${remain} AI积分`;
  }
  if (remain >= 0 && total > 0) {
    return remain === 0 ? `0 / ${total} · 已耗尽` : `余 ${remain} / ${total}`;
  }
  return item.memberLabel ? `${item.memberLabel} · 已同步` : "已同步状态";
}

export function formatGeminiCompactPillQuota(item: QwenQuotaLike): string {
  if (!item.creditsSynced) return "";
  const remain = item.creditsRemain ?? -1;
  const total = item.creditsTotal ?? 0;
  if (isGeminiPercentQuota(item) && remain >= 0) {
    return `${remain}%`;
  }
  if (item.creditsCycle === "ai_credits" && remain >= 0) {
    return `${remain}分`;
  }
  if (remain >= 0 && total > 0) {
    return `${remain}/${total}`;
  }
  if (item.memberLabel && isPaidMemberTier(item.memberTier)) {
    return item.memberLabel;
  }
  return "";
}

export function formatGeminiMetaLine(item: QwenQuotaLike, todayCount?: number): string {
  const parts: string[] = [];
  if (item.creditsSynced) {
    if (item.creditsCycleLabel) {
      parts.push(item.creditsCycleLabel);
    }
    if (item.creditsResetLabel) {
      parts.push(item.creditsResetLabel);
    }
    if (typeof todayCount === "number" && todayCount > 0) {
      parts.push(`今日 ${todayCount} 次`);
    }
    if (parts.length === 0) {
      parts.push(item.memberLabel || "已同步套餐");
    }
  } else {
    parts.push("点击同步读取剩余额度与套餐");
    if (typeof todayCount === "number" && todayCount > 0) {
      parts.push(`今日 ${todayCount} 次`);
    }
  }
  return parts.join(" · ");
}

export function buildGeminiStatusTooltip(
  item: QwenQuotaLike,
  extra?: {
    todayCount?: number;
    successToday?: number;
    failedToday?: number;
    cooldownAt?: string;
  },
): string {
  const lines: string[] = [];
  if (item.creditsSynced) {
    const remain = item.creditsRemain ?? -1;
    const total = item.creditsTotal ?? 0;
    if (isGeminiPercentQuota(item) && remain >= 0) {
      const cyclePart = item.creditsCycleLabel ? `（${item.creditsCycleLabel}）` : "";
      lines.push(`剩余额度：${remain}%${cyclePart}`);
    } else if (item.creditsCycle === "ai_credits" && remain >= 0) {
      lines.push(`AI 积分：剩余 ${remain}`);
    } else if (remain >= 0 && total > 0) {
      lines.push(`剩余额度：${remain} / ${total}`);
    } else {
      lines.push("剩余额度：当前账号未返回实时用量百分比（已完成状态与套餐同步）");
    }
    if (item.creditsResetLabel) {
      lines.push(`刷新时间：${item.creditsResetLabel}`);
    }
    if (item.memberLabel) {
      lines.push(`订阅套餐：${item.memberLabel}`);
    }
    if (item.creditsSyncedLabel) {
      lines.push(`状态同步：${item.creditsSyncedLabel}`);
    }
  } else {
    lines.push("Gemini 额度尚未同步，点击「同步」或完成一次出图后自动读取");
    lines.push("同步内容：实时剩余额度百分比 / AI 积分、窗口刷新时间、订阅套餐与默认账号用户名");
  }
  if (extra) {
    if (typeof extra.todayCount === "number") {
      if (typeof extra.successToday === "number") {
        lines.push(`今日出图：已派发 ${extra.todayCount} 次 · 成功 ${extra.successToday} 次`);
      } else {
        lines.push(`今日派发：${extra.todayCount} 次`);
      }
    }
    if (extra.cooldownAt) {
      lines.push(`冷却恢复：预计 ${extra.cooldownAt}`);
    }
  }
  return lines.join("\n");
}

export function summarizeGeminiPool(items: QwenQuotaLike[]): GeminiPoolSummary {
  const gemini = items.filter(isGeminiAccount);
  const enabled = gemini.filter((item) => !item.disabled);
  const scope = enabled.length > 0 ? enabled : gemini;
  let syncedAccounts = 0;
  let meteredAccounts = 0;
  let remainPctSum = 0;
  let minRemainPct = 100;
  let exhaustedCount = 0;
  let paidCount = 0;
  let aiCreditsSum = 0;
  let earliestResetAt = 0;
  let earliestResetLabel = "";

  for (const item of scope) {
    if (!item.creditsSynced) continue;
    syncedAccounts += 1;
    if (isPaidMemberTier(item.memberTier)) {
      paidCount += 1;
    }
    const remain = item.creditsRemain ?? -1;
    if (isGeminiPercentQuota(item) && remain >= 0) {
      meteredAccounts += 1;
      remainPctSum += remain;
      if (remain < minRemainPct) minRemainPct = remain;
      if (remain === 0) exhaustedCount += 1;
    } else if (item.creditsCycle === "ai_credits" && remain >= 0) {
      aiCreditsSum += remain;
      if (remain === 0) exhaustedCount += 1;
    } else if (remain === 0) {
      exhaustedCount += 1;
    }
    const resetAt = item.creditsResetAt ?? 0;
    if (resetAt > 0 && (earliestResetAt === 0 || resetAt < earliestResetAt)) {
      earliestResetAt = resetAt;
      earliestResetLabel = item.creditsResetLabel || "";
    } else if (!earliestResetLabel && item.creditsResetLabel) {
      earliestResetLabel = item.creditsResetLabel;
    }
  }

  return {
    totalAccounts: gemini.length,
    enabledAccounts: enabled.length,
    syncedAccounts,
    meteredAccounts,
    avgRemainPct: meteredAccounts > 0 ? Math.round(remainPctSum / meteredAccounts) : -1,
    minRemainPct: meteredAccounts > 0 ? minRemainPct : -1,
    exhaustedCount,
    paidCount,
    aiCreditsSum,
    earliestResetAt,
    earliestResetLabel,
  };
}

export function qwenCreditTone(item: QwenQuotaLike): "danger" | "warn" | "ok" | "muted" {
  if (!item.creditsSynced) return "muted";
  const remain = item.creditsRemain ?? -1;
  if (remain === 0) return "danger";
  if ((item.usagePct ?? 0) >= 80 || (remain > 0 && remain <= 5)) return "warn";
  return "ok";
}

export function qwenProgressTone(item: QwenQuotaLike): "danger" | "warn" | "accent" {
  const tone = qwenCreditTone(item);
  if (tone === "danger" || (item.usagePct ?? 0) >= 95) return "danger";
  if (tone === "warn" || (item.usagePct ?? 0) >= 80) return "warn";
  return "accent";
}

export function formatQwenCreditsPrimary(item: QwenQuotaLike): string {
  if (!item.creditsSynced) return "积分未同步";
  const remain = item.creditsRemain ?? -1;
  const total = item.creditsTotal ?? 0;
  if (remain >= 0 && total > 0) {
    return remain === 0 ? `0 / ${total} 积分 · 已耗尽` : `余 ${remain} / ${total} 积分`;
  }
  if (remain >= 0) {
    return remain === 0 ? "0 积分 · 已耗尽" : `余 ${remain} 积分`;
  }
  return "积分未同步";
}

export function formatQwenCompactPillCredits(item: QwenQuotaLike): string {
  if (!item.creditsSynced) return "";
  const remain = item.creditsRemain ?? -1;
  if (remain < 0) return "";
  return `${remain}分`;
}

export function formatQwenMetaLine(item: QwenQuotaLike, todayCount?: number): string {
  const parts: string[] = [];
  if (item.creditsSynced) {
    if (item.creditsResetLabel) {
      parts.push(item.creditsResetLabel);
    } else if (item.creditsCycleLabel) {
      parts.push(item.creditsCycleLabel);
    }
    const cost = item.creditCost && item.creditCost > 0 ? item.creditCost : 1;
    parts.push(`${cost}分/张`);
    if (item.concurrencyLabel) {
      parts.push(item.concurrencyLabel);
    }
    if (item.watermarkFree) {
      parts.push("去水印");
    }
    if (typeof todayCount === "number" && todayCount > 0) {
      parts.push(`今日 ${todayCount} 次`);
    }
  } else {
    parts.push("免费约40分/周期");
    parts.push("1分/张");
    parts.push(item.concurrencyLabel || "免费突发≤6(建议2)");
    if (typeof todayCount === "number" && todayCount > 0) {
      parts.push(`今日 ${todayCount} 次`);
    }
  }
  return parts.join(" · ");
}

export function buildQwenStatusTooltip(
  item: QwenQuotaLike,
  extra?: {
    todayCount?: number;
    successToday?: number;
    failedToday?: number;
    cooldownAt?: string;
  },
): string {
  const lines: string[] = [];
  if (item.creditsSynced) {
    const cost = item.creditCost && item.creditCost > 0 ? item.creditCost : 1;
    const remain = item.creditsRemain ?? -1;
    const total = item.creditsTotal ?? 0;
    if (remain >= 0 && total > 0) {
      const est = Math.floor(remain / cost);
      lines.push(`积分：剩余 ${remain} / ${total}（约可生图 ${est} 张，${cost} 积分/张）`);
    } else if (remain >= 0) {
      lines.push(`积分：剩余 ${remain}（${cost} 积分/张）`);
    }
    const cyclePart = item.creditsCycleLabel ? `（${item.creditsCycleLabel}）` : "";
    if (item.creditsResetLabel) {
      lines.push(`刷新/到期：${item.creditsResetLabel}${cyclePart}`);
    } else if (item.creditsCycleLabel) {
      lines.push(`周期：${item.creditsCycleLabel}`);
    }
    if (item.memberLabel) {
      lines.push(`套餐：${item.memberLabel}${item.watermarkFree ? " · 支持去水印" : " · 默认带水印"}`);
    }
    if (item.concurrencyLimit !== undefined && item.concurrencyLimit < 0) {
      lines.push("并发上限：会员不限并发");
    } else {
      const cap = item.concurrencyLimit && item.concurrencyLimit > 0 ? item.concurrencyLimit : 6;
      lines.push(`并发上限：单号突发上限 ${cap}（建议单号 1–2 并发以防滑块风控）`);
    }
    if (item.creditsSyncedLabel) {
      lines.push(`状态同步：${item.creditsSyncedLabel}`);
    }
  } else {
    lines.push("千问积分尚未同步，点击「同步」或完成一次出图后自动读取");
    lines.push("参考规则：免费套餐每周期 40 积分（1 积分/张），突发并发上限 6（建议 1–2），会员不限并发");
  }
  if (extra) {
    if (typeof extra.todayCount === "number") {
      if (typeof extra.successToday === "number") {
        lines.push(`今日出图：已派发 ${extra.todayCount} 次 · 成功 ${extra.successToday} 次`);
      } else {
        lines.push(`今日派发：${extra.todayCount} 次`);
      }
    }
    if (extra.cooldownAt) {
      lines.push(`冷却恢复：预计 ${extra.cooldownAt}`);
    }
  }
  return lines.join("\n");
}

export function summarizeQwenPool(items: QwenQuotaLike[]): QwenPoolSummary {
  const qwen = items.filter(isQwenAccount);
  const enabled = qwen.filter((item) => !item.disabled);
  const scope = enabled.length > 0 ? enabled : qwen;
  let syncedAccounts = 0;
  let remainSum = 0;
  let totalSum = 0;
  let exhaustedCount = 0;
  let vipCount = 0;
  let costPerImage = 1;
  let earliestResetAt = 0;
  let earliestResetLabel = "";

  for (const item of scope) {
    if (!item.creditsSynced) continue;
    syncedAccounts += 1;
    if (item.memberTier === "vip" || (item.concurrencyLimit ?? 0) < 0) {
      vipCount += 1;
    }
    if (item.creditCost && item.creditCost > 0) {
      costPerImage = item.creditCost;
    }
    const remain = item.creditsRemain ?? -1;
    const total = item.creditsTotal ?? 0;
    if (remain >= 0) {
      remainSum += remain;
      if (remain === 0) exhaustedCount += 1;
    }
    if (total > 0) {
      totalSum += total;
    }
    const resetAt = item.creditsResetAt ?? 0;
    if (resetAt > 0 && (earliestResetAt === 0 || resetAt < earliestResetAt)) {
      earliestResetAt = resetAt;
      earliestResetLabel = item.creditsResetLabel || "";
    } else if (!earliestResetLabel && item.creditsResetLabel) {
      earliestResetLabel = item.creditsResetLabel;
    }
  }

  return {
    totalAccounts: qwen.length,
    enabledAccounts: enabled.length,
    syncedAccounts,
    remainSum,
    totalSum,
    exhaustedCount,
    vipCount,
    costPerImage,
    estImages: Math.floor(remainSum / Math.max(1, costPerImage)),
    earliestResetAt,
    earliestResetLabel,
  };
}
