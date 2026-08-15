import type {
  GrokVideoModelFamily,
  GrokVideoModelPrices,
  GrokVideoResolution,
} from "@/types";

export type GroupPricingSubmissionMode = "create" | "update";

export type GroupPricingValidationError =
  | "invalid_video_multiplier"
  | "invalid_video_price"
  | "invalid_video_model_price"
  | "invalid_grok_search_price"
  | "invalid_grok_audio_price"
  | "invalid_web_search_price";

export const GROK_VIDEO_MODEL_FAMILIES = [
  "grok-imagine-video",
  "grok-imagine-video-1.5",
] as const satisfies readonly GrokVideoModelFamily[];

export const GROK_VIDEO_RESOLUTIONS = [
  "480p",
  "720p",
  "1080p",
] as const satisfies readonly GrokVideoResolution[];

export type GrokVideoModelPricingForm = Record<
  GrokVideoModelFamily,
  Record<GrokVideoResolution, number | string | null>
>;

export interface GroupPricingFields {
  platform: string;
  rate_multiplier?: unknown;
  video_rate_independent?: unknown;
  video_rate_multiplier?: unknown;
  video_price_480p?: unknown;
  video_price_720p?: unknown;
  video_price_1080p?: unknown;
  video_model_prices?: unknown;
  web_search_price_per_call?: unknown;
  search_price_per_1k?: unknown;
  audio_realtime_price_per_min?: unknown;
  audio_tts_price_per_million_chars?: unknown;
  audio_stt_price_per_hour?: unknown;
}

export interface MutableGroupPricingForm {
  video_rate_independent: boolean;
  video_rate_multiplier: number | string;
  video_price_480p: number | string | null;
  video_price_720p: number | string | null;
  video_price_1080p: number | string | null;
  video_model_prices: GrokVideoModelPricingForm;
  web_search_price_per_call: number | string | null;
  search_price_per_1k: number | string | null;
  audio_realtime_price_per_min: number | string | null;
  audio_tts_price_per_million_chars: number | string | null;
  audio_stt_price_per_hour: number | string | null;
}

type NormalizedGroupPricingPayload<T> = Omit<T, "video_model_prices"> & {
  video_model_prices?: GrokVideoModelPrices | null;
};

type GroupPricingPayloadResult<T> =
  | { ok: true; payload: NormalizedGroupPricingPayload<T> }
  | { ok: false; error: GroupPricingValidationError };

const VIDEO_PRICE_FIELDS = [
  "video_price_480p",
  "video_price_720p",
  "video_price_1080p",
] as const;

const VIDEO_FIELDS = [
  "video_rate_independent",
  "video_rate_multiplier",
  ...VIDEO_PRICE_FIELDS,
  "video_model_prices",
] as const;

const GROK_EXPLICIT_PRICE_FIELDS = [
  "search_price_per_1k",
  "audio_realtime_price_per_min",
  "audio_tts_price_per_million_chars",
  "audio_stt_price_per_hour",
] as const;

const isBlank = (value: unknown): boolean =>
  value === null ||
  value === undefined ||
  (typeof value === "string" && value.trim() === "");

const parseFiniteNumber = (value: unknown): number | null => {
  if (isBlank(value)) return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
};

const parseOptionalNonNegativePrice = (
  value: unknown,
): { valid: true; value: number | null } | { valid: false } => {
  if (isBlank(value)) return { valid: true, value: null };
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed < 0) return { valid: false };
  return { valid: true, value: parsed };
};

export const createEmptyGrokVideoModelPricingForm = (
  prices?: GrokVideoModelPrices | null,
): GrokVideoModelPricingForm => ({
  "grok-imagine-video": {
    "480p": prices?.["grok-imagine-video"]?.["480p"] ?? null,
    "720p": prices?.["grok-imagine-video"]?.["720p"] ?? null,
    "1080p": prices?.["grok-imagine-video"]?.["1080p"] ?? null,
  },
  "grok-imagine-video-1.5": {
    "480p": prices?.["grok-imagine-video-1.5"]?.["480p"] ?? null,
    "720p": prices?.["grok-imagine-video-1.5"]?.["720p"] ?? null,
    "1080p": prices?.["grok-imagine-video-1.5"]?.["1080p"] ?? null,
  },
});

const normalizeVideoModelPrices = (
  value: unknown,
): { valid: true; value: GrokVideoModelPrices } | { valid: false } => {
  if (value === null || value === undefined) {
    return { valid: true, value: {} };
  }
  if (typeof value !== "object" || Array.isArray(value)) {
    return { valid: false };
  }

  const rawPrices = value as Record<string, unknown>;
  const normalized: GrokVideoModelPrices = {};
  for (const family of GROK_VIDEO_MODEL_FAMILIES) {
    const rawTiers = rawPrices[family];
    if (rawTiers !== undefined && (typeof rawTiers !== "object" || rawTiers === null || Array.isArray(rawTiers))) {
      return { valid: false };
    }

    const tiers = rawTiers as Record<string, unknown> | undefined;
    for (const resolution of GROK_VIDEO_RESOLUTIONS) {
      const price = parseOptionalNonNegativePrice(tiers?.[resolution]);
      if (!price.valid) return { valid: false };
      if (price.value === null) continue;
      if (!normalized[family]) normalized[family] = {};
      normalized[family]![resolution] = price.value;
    }
  }
  return { valid: true, value: normalized };
};

const setClearedVideoFields = (payload: Record<string, unknown>): void => {
  payload.video_rate_independent = false;
  payload.video_rate_multiplier = 1;
  for (const field of VIDEO_PRICE_FIELDS) payload[field] = -1;
  payload.video_model_prices = {};
};

const deleteVideoFields = (payload: Record<string, unknown>): void => {
  for (const field of VIDEO_FIELDS) delete payload[field];
};

const setClearedGrokExplicitPrices = (
  payload: Record<string, unknown>,
): void => {
  for (const field of GROK_EXPLICIT_PRICE_FIELDS) payload[field] = -1;
};

const deleteGrokExplicitPrices = (payload: Record<string, unknown>): void => {
  for (const field of GROK_EXPLICIT_PRICE_FIELDS) delete payload[field];
};

/**
 * Produces the API payload for platform-specific pricing fields.
 *
 * Create requests omit fields owned by another platform. Update requests use
 * the backend's -1 sentinel to remove stale prices left by a platform change.
 */
export function normalizeGroupPricingPayload<T extends GroupPricingFields>(
  input: T,
  mode: GroupPricingSubmissionMode,
): GroupPricingPayloadResult<T> {
  const payload = { ...input } as NormalizedGroupPricingPayload<T>;
  const writablePayload = payload as Record<string, unknown>;

  if (input.platform === "grok") {
    const independent = input.video_rate_independent === true;
    writablePayload.video_rate_independent = independent;

    if (independent) {
      const multiplier = parseFiniteNumber(input.video_rate_multiplier);
      if (multiplier === null || multiplier <= 0) {
        return { ok: false, error: "invalid_video_multiplier" };
      }
      writablePayload.video_rate_multiplier = multiplier;
    } else {
      writablePayload.video_rate_multiplier = 1;
    }

    for (const field of VIDEO_PRICE_FIELDS) {
      const price = parseOptionalNonNegativePrice(input[field]);
      if (!price.valid) {
        return { ok: false, error: "invalid_video_price" };
      }
      writablePayload[field] = price.value ?? (mode === "update" ? -1 : null);
    }

    const modelPrices = normalizeVideoModelPrices(input.video_model_prices);
    if (!modelPrices.valid) {
      return { ok: false, error: "invalid_video_model_price" };
    }
    writablePayload.video_model_prices = modelPrices.value;

    const searchPrice = parseOptionalNonNegativePrice(input.search_price_per_1k);
    if (!searchPrice.valid) {
      return { ok: false, error: "invalid_grok_search_price" };
    }
    writablePayload.search_price_per_1k =
      searchPrice.value ?? (mode === "update" ? -1 : null);

    for (const field of [
      "audio_realtime_price_per_min",
      "audio_tts_price_per_million_chars",
      "audio_stt_price_per_hour",
    ] as const) {
      const price = parseOptionalNonNegativePrice(input[field]);
      if (!price.valid) {
        return { ok: false, error: "invalid_grok_audio_price" };
      }
      writablePayload[field] = price.value ?? (mode === "update" ? -1 : null);
    }

    if (mode === "update") writablePayload.web_search_price_per_call = -1;
    else delete writablePayload.web_search_price_per_call;

    return { ok: true, payload };
  }

  if (mode === "update") setClearedVideoFields(writablePayload);
  else deleteVideoFields(writablePayload);

  if (mode === "update") setClearedGrokExplicitPrices(writablePayload);
  else deleteGrokExplicitPrices(writablePayload);

  if (input.platform === "openai") {
    const price = parseOptionalNonNegativePrice(
      input.web_search_price_per_call,
    );
    if (!price.valid) {
      return { ok: false, error: "invalid_web_search_price" };
    }
    writablePayload.web_search_price_per_call =
      price.value ?? (mode === "update" ? -1 : null);
  } else if (mode === "update") {
    writablePayload.web_search_price_per_call = -1;
  } else {
    delete writablePayload.web_search_price_per_call;
  }

  return { ok: true, payload };
}

/** Clears hidden pricing controls immediately when the selected platform changes. */
export function resetInactivePlatformPricing(
  form: MutableGroupPricingForm,
  platform: string,
): void {
  if (platform !== "grok") {
    form.video_rate_independent = false;
    form.video_rate_multiplier = 1;
    form.video_price_480p = null;
    form.video_price_720p = null;
    form.video_price_1080p = null;
    form.video_model_prices = createEmptyGrokVideoModelPricingForm();
    form.search_price_per_1k = null;
    form.audio_realtime_price_per_min = null;
    form.audio_tts_price_per_million_chars = null;
    form.audio_stt_price_per_hour = null;
  }
  if (platform !== "openai") form.web_search_price_per_call = null;
}

type VideoPricingPreviewForm = Pick<
  MutableGroupPricingForm,
  | "video_rate_independent"
  | "video_rate_multiplier"
  | "video_price_480p"
  | "video_price_720p"
  | "video_price_1080p"
> & { rate_multiplier: number | string };

const formatPrice = (value: number): string =>
  `$${value.toFixed(4).replace(/0+$/, "").replace(/\.$/, "")}`;

export function formatVideoPricePreview(
  form: VideoPricingPreviewForm,
  serverDefaultLabel: string,
): string {
  const selectedMultiplier = form.video_rate_independent
    ? parseFiniteNumber(form.video_rate_multiplier)
    : parseFiniteNumber(form.rate_multiplier);
  const multiplier =
    selectedMultiplier !== null && selectedMultiplier > 0
      ? selectedMultiplier
      : 1;

  return ([
    ["480p", form.video_price_480p],
    ["720p", form.video_price_720p],
    ["1080p", form.video_price_1080p],
  ] as const)
    .map(([resolution, rawPrice]) => {
      const price = parseOptionalNonNegativePrice(rawPrice);
      if (!price.valid || price.value === null) {
        return `${resolution} ${serverDefaultLabel}`;
      }
      return `${resolution} ${formatPrice(price.value * multiplier)}/s`;
    })
    .join(" · ");
}
