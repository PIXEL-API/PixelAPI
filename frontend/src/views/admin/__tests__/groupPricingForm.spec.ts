import { describe, expect, it } from "vitest";

import {
  createEmptyGrokVideoModelPricingForm,
  formatVideoPricePreview,
  normalizeGroupPricingPayload,
  resetInactivePlatformPricing,
} from "../groupPricingForm";

const basePricingForm = () => ({
  platform: "grok",
  rate_multiplier: 2,
  video_rate_independent: false,
  video_rate_multiplier: 1 as number | string,
  video_price_480p: null as number | string | null,
  video_price_720p: null as number | string | null,
  video_price_1080p: null as number | string | null,
  video_model_prices: createEmptyGrokVideoModelPricingForm(),
  web_search_price_per_call: null as number | string | null,
  search_price_per_1k: null as number | string | null,
  audio_realtime_price_per_min: null as number | string | null,
  audio_tts_price_per_million_chars: null as number | string | null,
  audio_stt_price_per_hour: null as number | string | null,
});

describe("normalizeGroupPricingPayload", () => {
  it("rejects an empty or non-positive independent Grok multiplier", () => {
    for (const multiplier of ["", 0, -1, Number.POSITIVE_INFINITY]) {
      const result = normalizeGroupPricingPayload(
        {
          ...basePricingForm(),
          video_rate_independent: true,
          video_rate_multiplier: multiplier,
        },
        "create",
      );

      expect(result).toEqual({
        ok: false,
        error: "invalid_video_multiplier",
      });
    }
  });

  it("uses a safe multiplier and omits OpenAI pricing for a Grok create", () => {
    const result = normalizeGroupPricingPayload(
      {
        ...basePricingForm(),
        video_rate_multiplier: "",
        web_search_price_per_call: 0.01,
      },
      "create",
    );

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.payload.video_rate_multiplier).toBe(1);
    expect(result.payload.video_price_480p).toBeNull();
    expect(result.payload.video_model_prices).toEqual({});
    expect(result.payload.search_price_per_1k).toBeNull();
    expect("web_search_price_per_call" in result.payload).toBe(false);
  });

  it("omits hidden Grok fields and preserves an explicit free search price for an OpenAI create", () => {
    const result = normalizeGroupPricingPayload(
      {
        ...basePricingForm(),
        platform: "openai",
        video_rate_independent: true,
        video_rate_multiplier: 3,
        video_price_480p: 0.5,
        web_search_price_per_call: 0,
      },
      "create",
    );

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.payload.web_search_price_per_call).toBe(0);
    expect("video_rate_independent" in result.payload).toBe(false);
    expect("video_price_480p" in result.payload).toBe(false);
    expect("video_model_prices" in result.payload).toBe(false);
    expect("search_price_per_1k" in result.payload).toBe(false);
    expect("audio_realtime_price_per_min" in result.payload).toBe(false);
  });

  it("clears stale cross-platform pricing on updates", () => {
    const openAIResult = normalizeGroupPricingPayload(
      {
        ...basePricingForm(),
        platform: "openai",
        web_search_price_per_call: "",
      },
      "update",
    );
    expect(openAIResult.ok).toBe(true);
    if (!openAIResult.ok) return;
    expect(openAIResult.payload).toMatchObject({
      video_rate_independent: false,
      video_rate_multiplier: 1,
      video_price_480p: -1,
      video_price_720p: -1,
      video_price_1080p: -1,
      video_model_prices: {},
      search_price_per_1k: -1,
      audio_realtime_price_per_min: -1,
      audio_tts_price_per_million_chars: -1,
      audio_stt_price_per_hour: -1,
      web_search_price_per_call: -1,
    });

    const grokResult = normalizeGroupPricingPayload(
      {
        ...basePricingForm(),
        video_rate_independent: true,
        video_rate_multiplier: 1.5,
        video_price_720p: 0,
        web_search_price_per_call: 0.02,
      },
      "update",
    );
    expect(grokResult.ok).toBe(true);
    if (!grokResult.ok) return;
    expect(grokResult.payload.video_rate_multiplier).toBe(1.5);
    expect(grokResult.payload.video_price_720p).toBe(0);
    expect(grokResult.payload.search_price_per_1k).toBe(-1);
    expect(grokResult.payload.web_search_price_per_call).toBe(-1);
  });

  it("normalizes Grok model, search, and audio prices while preserving explicit free tiers", () => {
    const form = basePricingForm();
    form.video_model_prices["grok-imagine-video"]["480p"] = 0;
    form.video_model_prices["grok-imagine-video-1.5"]["1080p"] = "0.25";

    const result = normalizeGroupPricingPayload(
      {
        ...form,
        search_price_per_1k: 0,
        audio_realtime_price_per_min: "0.5",
        audio_tts_price_per_million_chars: 0,
        audio_stt_price_per_hour: "",
      },
      "update",
    );

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.payload.video_model_prices).toEqual({
      "grok-imagine-video": { "480p": 0 },
      "grok-imagine-video-1.5": { "1080p": 0.25 },
    });
    expect(result.payload.search_price_per_1k).toBe(0);
    expect(result.payload.audio_realtime_price_per_min).toBe(0.5);
    expect(result.payload.audio_tts_price_per_million_chars).toBe(0);
    expect(result.payload.audio_stt_price_per_hour).toBe(-1);
  });

  it("rejects negative, NaN, and infinite Grok capability prices", () => {
    const invalidValues = [-1, Number.NaN, Number.POSITIVE_INFINITY];

    for (const value of invalidValues) {
      const searchResult = normalizeGroupPricingPayload(
        { ...basePricingForm(), search_price_per_1k: value },
        "create",
      );
      expect(searchResult).toEqual({
        ok: false,
        error: "invalid_grok_search_price",
      });

      const audioResult = normalizeGroupPricingPayload(
        { ...basePricingForm(), audio_realtime_price_per_min: value },
        "create",
      );
      expect(audioResult).toEqual({
        ok: false,
        error: "invalid_grok_audio_price",
      });

      const modelForm = basePricingForm();
      modelForm.video_model_prices["grok-imagine-video"]["720p"] = value;
      const modelResult = normalizeGroupPricingPayload(modelForm, "create");
      expect(modelResult).toEqual({
        ok: false,
        error: "invalid_video_model_price",
      });
    }
  });
});

describe("resetInactivePlatformPricing", () => {
  it("clears controls owned by inactive platforms", () => {
    const form = {
      ...basePricingForm(),
      video_rate_independent: true,
      video_rate_multiplier: 2,
      video_price_480p: 0.1,
      video_price_720p: 0.2,
      video_price_1080p: 0.3,
      video_model_prices: createEmptyGrokVideoModelPricingForm({
        "grok-imagine-video": { "480p": 0.05 },
      }),
      web_search_price_per_call: 0.01,
      search_price_per_1k: 5,
      audio_realtime_price_per_min: 0.5,
      audio_tts_price_per_million_chars: 1,
      audio_stt_price_per_hour: 2,
    };

    resetInactivePlatformPricing(form, "openai");
    expect(form).toMatchObject({
      video_rate_independent: false,
      video_rate_multiplier: 1,
      video_price_480p: null,
      video_price_720p: null,
      video_price_1080p: null,
      video_model_prices: createEmptyGrokVideoModelPricingForm(),
      search_price_per_1k: null,
      audio_realtime_price_per_min: null,
      audio_tts_price_per_million_chars: null,
      audio_stt_price_per_hour: null,
      web_search_price_per_call: 0.01,
    });

    resetInactivePlatformPricing(form, "anthropic");
    expect(form.web_search_price_per_call).toBeNull();
  });
});

describe("createEmptyGrokVideoModelPricingForm", () => {
  it("hydrates supported model tiers and keeps every control writable", () => {
    expect(
      createEmptyGrokVideoModelPricingForm({
        "grok-imagine-video": { "720p": 0 },
        "grok-imagine-video-1.5": { "1080p": 0.25 },
      }),
    ).toEqual({
      "grok-imagine-video": {
        "480p": null,
        "720p": 0,
        "1080p": null,
      },
      "grok-imagine-video-1.5": {
        "480p": null,
        "720p": null,
        "1080p": 0.25,
      },
    });
  });
});

describe("formatVideoPricePreview", () => {
  it("labels blank prices as server model defaults instead of inventing fixed values", () => {
    const preview = formatVideoPricePreview(
      {
        ...basePricingForm(),
        video_price_720p: 0.07,
      },
      "使用服务端模型默认价",
    );

    expect(preview).toBe(
      "480p 使用服务端模型默认价 · 720p $0.14/s · 1080p 使用服务端模型默认价",
    );
    expect(preview).not.toContain("480p $0.05");
    expect(preview).not.toContain("1080p $0.25");
  });
});
