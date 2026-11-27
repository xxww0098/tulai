import { useEffect, useState } from "react";
import { Button, Dialog, Input, Select } from "@/components/ui";
import { Browser, formatCallError } from "@/lib/ipc";
import type { BrowserFingerprintDTO } from "@/mygo";

type Props = { id: number; name: string; onClose: () => void };

const appearanceOptions = [
  { value: "default", label: "默认" },
  { value: "system", label: "跟随系统" },
  { value: "light", label: "浅色" },
  { value: "dark", label: "深色" },
];

export function FingerprintDialog({ id, name, onClose }: Props) {
  const [value, setValue] = useState<BrowserFingerprintDTO | null>(null);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let active = true;
    Browser.getAccountFingerprint(id)
      .then((data) => { if (active) setValue(data); })
      .catch((err) => { if (active) setError(formatCallError(err)); });
    return () => { active = false; };
  }, [id]);

  async function save() {
    if (!value || saving) return;
    setSaving(true);
    setError("");
    try {
      await Browser.saveAccountFingerprint(id, value);
      onClose();
    } catch (err) {
      setError(formatCallError(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      open
      title="指纹设置"
      description={name + " · 下次打开浏览器时生效。留空使用默认值。"}
      width={440}
      closeOnBackdrop={!saving}
      closeOnEscape={!saving}
      onOpenChange={(open) => { if (!open && !saving) onClose(); }}
      footer={
        <>
          <Button text="取消" size="small" variant="ghost" disabled={saving} onClick={onClose} />
          <Button text="保存" size="small" variant="primary" disabled={!value} busy={saving} onClick={save} />
        </>
      }
    >
      {value && (
        <fieldset disabled={saving} className="grid grid-cols-2 gap-3 text-xs text-ink">
          <label className="grid gap-1">
            语言
            <Input placeholder="zh-CN" value={value.lang}
              onChange={(e) => setValue({ ...value, lang: e.target.value })} />
          </label>
          <label className="grid gap-1">
            时区
            <Input placeholder="Asia/Shanghai" value={value.timezone}
              onChange={(e) => setValue({ ...value, timezone: e.target.value })} />
          </label>
          {(["windowWidth", "windowHeight"] as const).map((key) => (
            <label key={key} className="grid gap-1">
              {key === "windowWidth" ? "窗口宽度（像素）" : "窗口高度（像素）"}
              <Input type="number" min={1} max={32768} placeholder="默认" value={value[key] || ""}
                onChange={(e) => setValue({ ...value, [key]: Number(e.target.value) })} />
            </label>
          ))}
          <div className="col-span-2 grid gap-1">
            <span>网页外观</span>
            <Select
              aria-label="网页外观"
              value={value.colorScheme || "default"}
              options={appearanceOptions}
              onValueChange={(scheme) => setValue({ ...value, colorScheme: scheme === "default" ? "" : scheme })}
            />
          </div>
        </fieldset>
      )}
      {!value && !error && <span role="status">正在读取…</span>}
      {error && <p role="alert" className="mt-2 text-xs text-status-danger">{error}</p>}
    </Dialog>
  );
}
