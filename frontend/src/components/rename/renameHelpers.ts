import { formatCallError, isMyGoRuntime } from "@/lib/ipc";
import { showToast, type RenameStateDTO } from "@/store";

export interface RenameModeDef {
  value: number;
  label: string;
  icon: string;
}

// Exact uTools "批量重命名" tab order mapped to Go backend mode IDs
export const RENAME_MODES: RenameModeDef[] = [
  { value: 0, label: "查找替换", icon: "replace-loop" },
  { value: 4, label: "智能重命名", icon: "wand-sparkles" },
  { value: 2, label: "插入内容", icon: "insert-square" },
  { value: 3, label: "自动编号", icon: "list-ordered" },
  { value: 1, label: "手动编辑", icon: "pencil" },
];

export const INSERT_CONTENT_TABS = [
  { value: "文本", label: "文本" },
  { value: "序号", label: "序号" },
  { value: "文件属性", label: "文件信息" },
] as const;

export const INSERT_ATTR_OPTIONS = [
  { value: "修改时间", label: "修改时间" },
  { value: "创建时间", label: "创建时间" },
  { value: "大小", label: "文件大小 (字节)" },
  { value: "图片宽度", label: "图片宽度 (px)" },
  { value: "图片高度", label: "图片高度 (px)" },
  { value: "照片拍摄时间", label: "照片拍摄时间 (EXIF)" },
];

export const INSERT_POSITION_OPTIONS = [
  { value: "开头", label: "开头" },
  { value: "指定位置", label: "指定位置" },
  { value: "末尾", label: "末尾" },
] as const;

export const NUMBER_FORMAT_OPTIONS = [
  { value: "阿拉伯数字", label: "阿拉伯数字" },
  { value: "中文小写", label: "中文小写" },
  { value: "中文大写", label: "中文大写" },
  { value: "英文大写", label: "英文大写" },
  { value: "英文小写", label: "英文小写" },
] as const;

export const SORT_OPTIONS = [
  { value: "名称升序", label: "按名称升序" },
  { value: "名称降序", label: "按名称降序" },
  { value: "修改时间升序", label: "按修改时间升序" },
  { value: "修改时间降序", label: "按修改时间降序" },
  { value: "创建时间升序", label: "按创建时间升序" },
  { value: "创建时间降序", label: "按创建时间降序" },
  { value: "大小升序", label: "按大小升序" },
  { value: "大小降序", label: "按大小降序" },
];

export const DEFAULT_TEMPLATE_NAMES = [
  "普通查找",
  "前 N 位",
  "后 N 位",
  "从第 X 位开始的 N 位",
  "某字符之后的内容",
  "某字符之前的内容",
  "某字符之后的 N 位",
  "某字符之前的 N 位",
];

export const EMPTY_RENAME_STATE: RenameStateDTO = {
  mode: 0,
  busy: false,
  includeExt: false,
  insertKind: "文本",
  position: "末尾",
  index: 1,
  text: "",
  prefix: "",
  suffix: "",
  numFormat: "阿拉伯数字",
  numStart: 1,
  numWidth: 0,
  numPrefix: "",
  numSuffix: "",
  sortBy: "名称升序",
  aiPrompt: "",
  aiSettings: false,
  aiUrl: "",
  aiKey: "",
  aiModel: "",
  rules: [{ index: 0, find: "", with: "", template: 0, count: 1, offset: 0, text: "" }],
  rows: [],
  changedCount: 0,
  canUndo: false,
  planErr: "",
  note: "",
  err: "",
  templateNames: DEFAULT_TEMPLATE_NAMES,
};

export function runIpc(fn: () => unknown): void {
  try {
    void Promise.resolve(fn()).catch((err) => {
      if (isMyGoRuntime()) {
        showToast(formatCallError(err), "error");
      }
    });
  } catch (err) {
    if (isMyGoRuntime()) {
      showToast(formatCallError(err), "error");
    }
  }
}

export function resolveInsertTab(insertKind: string): "文本" | "序号" | "文件属性" {
  if (insertKind === "序号") return "序号";
  if (INSERT_ATTR_OPTIONS.some((o) => o.value === insertKind)) return "文件属性";
  return "文本";
}

export function resolvePositionValue(position: string): "开头" | "指定位置" | "末尾" {
  if (position === "开头" || position === "指定位置" || position === "末尾") {
    return position;
  }
  return "末尾";
}

const REGEX_LITERAL_RE = /^\/(.*)\/([gimuy]*)$/;

export function isRegexFind(value: string): boolean {
  return value.length >= 2 && REGEX_LITERAL_RE.test(value);
}

export function toggleRegexFind(value: string): string {
  const match = value.match(REGEX_LITERAL_RE);
  if (match) {
    return match[1];
  }
  return `/${value}/g`;
}

export function formatNumberSample(
  format: string,
  start: number,
  width: number,
  prefix: string,
  suffix: string,
): string {
  const n = Math.max(0, start);
  let core = String(n);
  if (format === "中文小写" || format === "中文大写") {
    const digits =
      format === "中文大写"
        ? ["零", "壹", "贰", "叁", "肆", "伍", "陆", "柒", "捌", "玖"]
        : ["零", "一", "二", "三", "四", "五", "六", "七", "八", "九"];
    if (n < 10) {
      core = digits[n] ?? String(n);
    } else if (n < 20 && format === "中文小写") {
      core = `十${n % 10 === 0 ? "" : digits[n % 10]}`;
    } else {
      core = n < 100 ? `${digits[Math.floor(n / 10)]}${format === "中文大写" ? "拾" : "十"}${n % 10 === 0 ? "" : digits[n % 10]}` : String(n);
    }
  } else if (format === "英文大写" || format === "英文小写") {
    let val = "";
    let cur = Math.max(1, n);
    while (cur > 0) {
      cur -= 1;
      val = String.fromCharCode(65 + (cur % 26)) + val;
      cur = Math.floor(cur / 26);
    }
    core = format === "英文小写" ? val.toLowerCase() : val;
  } else if (width > core.length) {
    core = "0".repeat(width - core.length) + core;
  }
  return `${prefix}${core}${suffix}`;
}
