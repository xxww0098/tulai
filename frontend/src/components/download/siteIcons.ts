const siteIconModules = import.meta.glob<string>(
  "@/assets/download-sites/*.{png,svg}",
  { eager: true, import: "default" },
);

const SITE_ICON_MAP: Record<string, string> = Object.fromEntries(
  Object.entries(siteIconModules).flatMap(([path, url]) => {
    const file = path.split("/").pop() ?? "";
    const domain = file.replace(/\.(png|svg)$/i, "").toLowerCase();
    return domain ? [[domain, url]] : [];
  }),
);

/**
 * Extracts the primary normalized domain key from a guide entry's `icons` field.
 */
export function getSiteDomain(icons?: string): string {
  const first = (icons ?? "").trim().split(/\s+/)[0] ?? "";
  return first.toLowerCase();
}

/**
 * Resolves the bundled favicon asset URL for a supported download site domain.
 */
export function getSiteIconUrl(icons?: string): string | undefined {
  const domain = getSiteDomain(icons);
  if (!domain) return undefined;
  return SITE_ICON_MAP[domain];
}
