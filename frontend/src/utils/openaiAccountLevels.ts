import type { OpenAIAccountLevelConfig, SelectOption } from "@/types";

export const DEFAULT_OPENAI_ACCOUNT_LEVELS: OpenAIAccountLevelConfig[] = [
  { key: "free", label: "Free", aliases: ["free", "chatgptfree"], enabled: true, requires_proxy_login: false, sort_order: 10 },
  { key: "plus", label: "Plus", aliases: ["plus", "plus*", "chatgptplus"], enabled: true, requires_proxy_login: false, sort_order: 20 },
  { key: "pro", label: "Pro", aliases: ["pro", "pro*", "chatgptpro", "chatgptpro*"], enabled: true, requires_proxy_login: true, sort_order: 30 },
  { key: "team", label: "Team", aliases: ["team", "team*", "chatgptteam"], enabled: true, requires_proxy_login: false, sort_order: 40 },
  { key: "k12", label: "K12", aliases: ["k12", "chatgptk12", "chatgpt-k12"], enabled: true, requires_proxy_login: false, sort_order: 50 },
];

export function normalizeOpenAIAccountLevelKey(value: unknown): string {
  if (typeof value !== "string") return "";
  return value
    .trim()
    .toLowerCase()
    .replace(/[\s_]+/g, "-")
    .replace(/[^a-z0-9-]/g, "")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
}

export function normalizeOpenAIAccountLevelConfigs(
  levels?: OpenAIAccountLevelConfig[] | null,
): OpenAIAccountLevelConfig[] {
  const source = Array.isArray(levels) && levels.length > 0 ? levels : DEFAULT_OPENAI_ACCOUNT_LEVELS;
  const seen = new Set<string>();
  const normalized: OpenAIAccountLevelConfig[] = [];
  source.forEach((level, index) => {
    const key = normalizeOpenAIAccountLevelKey(level.key);
    if (!key || key === "unknown" || seen.has(key)) return;
    seen.add(key);
    normalized.push({
      key,
      label: String(level.label || key).trim() || key,
      aliases: Array.isArray(level.aliases) ? level.aliases.map(String).filter(Boolean) : [],
      enabled: level.enabled !== false,
      requires_proxy_login: level.requires_proxy_login === true,
      sort_order: Number.isFinite(Number(level.sort_order)) ? Number(level.sort_order) : (index + 1) * 10,
    });
  });
  return normalized.sort((a, b) => a.sort_order - b.sort_order || a.key.localeCompare(b.key));
}

export function selectableOpenAIAccountLevels(
  levels: OpenAIAccountLevelConfig[] | null | undefined,
): OpenAIAccountLevelConfig[] {
  return normalizeOpenAIAccountLevelConfigs(levels).filter((level) => level.enabled);
}

export function openAIAccountLevelOptions(
  levels: OpenAIAccountLevelConfig[] | null | undefined,
  options: { includeUnknown?: boolean; includeEmpty?: boolean; emptyLabel?: string; unknownLabel?: string } = {},
): SelectOption[] {
  const items: SelectOption[] = [];
  if (options.includeEmpty) {
    items.push({ value: "", label: options.emptyLabel ?? "不限制" });
  }
  if (options.includeUnknown) {
    items.push({ value: "unknown", label: options.unknownLabel ?? "Unknown" });
  }
  for (const level of selectableOpenAIAccountLevels(levels)) {
    items.push({ value: level.key, label: level.label });
  }
  return items;
}

export function openAIAccountLevelLabel(
  value: unknown,
  levels: OpenAIAccountLevelConfig[] | null | undefined,
): string {
  const key = normalizeOpenAIAccountLevelKey(value);
  if (!key) return "";
  if (key === "unknown") return "Unknown";
  return normalizeOpenAIAccountLevelConfigs(levels).find((level) => level.key === key)?.label ?? key;
}
