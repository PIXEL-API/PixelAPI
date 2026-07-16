import { describe, expect, it } from "vitest";

import {
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
  web_search_price_per_call: null as number | string | null,
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
    expect(grokResult.payload.web_search_price_per_call).toBe(-1);
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
      web_search_price_per_call: 0.01,
    };

    resetInactivePlatformPricing(form, "openai");
    expect(form).toMatchObject({
      video_rate_independent: false,
      video_rate_multiplier: 1,
      video_price_480p: null,
      video_price_720p: null,
      video_price_1080p: null,
      web_search_price_per_call: 0.01,
    });

    resetInactivePlatformPricing(form, "anthropic");
    expect(form.web_search_price_per_call).toBeNull();
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
