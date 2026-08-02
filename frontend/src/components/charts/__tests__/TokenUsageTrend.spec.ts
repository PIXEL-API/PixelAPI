import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import TokenUsageTrend from '../TokenUsageTrend.vue'
import type { TrendDataPoint } from '@/types'

const messages: Record<string, string> = {
  'admin.dashboard.tokenUsageTrend': 'Token Usage Trend',
  'admin.dashboard.noDataAvailable': 'No data available',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

vi.mock('vue-chartjs', () => ({
  Line: {
    props: ['data', 'options'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

function makePoint(overrides: Partial<TrendDataPoint>): TrendDataPoint {
  const point: TrendDataPoint = {
    date: '2026-08-02',
    requests: 1,
    input_tokens: 0,
    output_tokens: 0,
    cache_creation_tokens: 0,
    cache_read_tokens: 0,
    total_tokens: 0,
    cost: 0,
    actual_cost: 0,
    ...overrides,
  }
  point.total_tokens =
    point.input_tokens +
    point.output_tokens +
    point.cache_creation_tokens +
    point.cache_read_tokens
  return point
}

function hitRateSeries(trendData: TrendDataPoint[]): number[] {
  const wrapper = mount(TokenUsageTrend, {
    props: { trendData },
    global: { stubs: { LoadingSpinner: true } },
  })
  const chartData = JSON.parse(wrapper.find('.chart-data').text())
  const dataset = chartData.datasets.find((ds: any) => ds.label === 'Cache Hit Rate')
  expect(dataset).toBeTruthy()
  return dataset.data
}

describe('TokenUsageTrend cache hit rate', () => {
  it('OpenAI 口径（cache_creation 恒为 0）不再恒显示 100%', () => {
    // usage_logs 里 OpenAI 的 input_tokens 已扣掉 cached_tokens，两桶互斥。
    // 命中率 = 1500 / (500 + 1500 + 0) = 75%
    const data = hitRateSeries([
      makePoint({ input_tokens: 500, output_tokens: 100, cache_read_tokens: 1500 }),
    ])
    expect(data[0]).toBe(75)
    expect(data[0]).not.toBe(100)
  })

  it('Anthropic 口径把 cache_creation 计入分母', () => {
    // 命中率 = 500 / (200 + 500 + 300) = 50%
    const data = hitRateSeries([
      makePoint({
        input_tokens: 200,
        output_tokens: 50,
        cache_creation_tokens: 300,
        cache_read_tokens: 500,
      }),
    ])
    expect(data[0]).toBe(50)
  })

  it('纯缓存写入（无读取）时命中率为 0', () => {
    const data = hitRateSeries([
      makePoint({ input_tokens: 100, output_tokens: 20, cache_creation_tokens: 900 }),
    ])
    expect(data[0]).toBe(0)
  })

  it('全部输入侧 token 为 0 时返回 0 而非 NaN', () => {
    const data = hitRateSeries([makePoint({ output_tokens: 10 })])
    expect(data[0]).toBe(0)
  })

  it('输入侧 token 全部来自缓存读取时才是 100%', () => {
    const data = hitRateSeries([makePoint({ output_tokens: 10, cache_read_tokens: 800 })])
    expect(data[0]).toBe(100)
  })

  it('负数/缺失字段被夹到 0，不产生负命中率', () => {
    const point = makePoint({ input_tokens: 100, cache_read_tokens: 100 })
    // 模拟后端异常回传
    ;(point as unknown as Record<string, unknown>).cache_creation_tokens = -50
    const data = hitRateSeries([point])
    expect(data[0]).toBe(50)
  })

  it('逐点计算，不做跨点汇总', () => {
    const data = hitRateSeries([
      makePoint({ input_tokens: 100, cache_read_tokens: 300 }),
      makePoint({ input_tokens: 300, cache_read_tokens: 100 }),
    ])
    expect(data).toEqual([75, 25])
  })
})
