import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

import { buildModelMappingObject, getModelsByPlatform, getPresetMappingsByPlatform } from '../useModelWhitelist'

describe('useModelWhitelist', () => {
  it('openai 模型列表包含 GPT-5.4 官方快照', () => {
    const models = getModelsByPlatform('openai')

    expect(models).toContain('gpt-5.4')
    expect(models).toContain('gpt-5.4-mini')
    expect(models).toContain('gpt-5.4-2026-03-05')
  })

  it('openai 模型列表不再暴露已下线的 ChatGPT 登录 Codex 模型', () => {
    const models = getModelsByPlatform('openai')

    expect(models).not.toContain('gpt-5')
    expect(models).not.toContain('gpt-5.1')
    expect(models).not.toContain('gpt-5.1-codex')
    expect(models).not.toContain('gpt-5.1-codex-max')
    expect(models).not.toContain('gpt-5.1-codex-mini')
    expect(models).not.toContain('gpt-5.2-codex')
  })

  it('antigravity 模型列表包含图片模型兼容项', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models).toContain('gemini-2.5-flash-image')
    expect(models).toContain('gemini-3.1-flash-image')
    expect(models).toContain('gemini-3-pro-image')
  })

  it('xAI 模型列表包含 Grok 4.5 官方模型和别名', () => {
	const models = getModelsByPlatform('grok')

	expect(models).toContain('grok-4.6')
	expect(models).toContain('grok-4.6-latest')
	expect(models).toContain('grok-4.5')
    expect(models).toContain('grok-4.5-latest')
    expect(models).toContain('grok-build-latest')
  })

  it('Grok 4.5 官方别名预设指向最新模型', () => {
    const mappings = getPresetMappingsByPlatform('grok')

    expect(mappings).toEqual(expect.arrayContaining([
      expect.objectContaining({ from: 'grok-latest', to: 'grok-4.5' }),
      expect.objectContaining({ from: 'grok-4.5-latest', to: 'grok-4.5' }),
      expect.objectContaining({ from: 'grok-build-latest', to: 'grok-4.5' })
    ]))
  })

  it('gemini 模型列表包含原生生图模型', () => {
    const models = getModelsByPlatform('gemini')

    expect(models).toContain('gemini-2.5-flash-image')
    expect(models).toContain('gemini-3.1-flash-image')
  })

  it('grok 模型列表包含官方订阅和媒体模型别名', () => {
    const models = getModelsByPlatform('grok')

    expect(models).toContain('grok-4.3')
    expect(models).toContain('grok-3-mini-fast')
    expect(models).toContain('grok-4.20-multi-agent-latest')
    expect(models).toContain('grok-imagine-image-quality')
    expect(models).toContain('grok-imagine-edit')
    expect(models).toContain('grok-imagine-video-1.5')
    expect(models).toContain('grok-imagine-video-1.5-preview')
    expect(getPresetMappingsByPlatform('grok')).toContainEqual(
      expect.objectContaining({ from: 'grok-imagine-edit', to: 'grok-imagine-image-quality' })
    )
  })

  it('xai 平台沿用 grok 模型列表与预设映射', () => {
    expect(getModelsByPlatform('xai')).toEqual(getModelsByPlatform('grok'))
    expect(getPresetMappingsByPlatform('xai')).toEqual(getPresetMappingsByPlatform('grok'))
  })

  it('antigravity 模型列表会把新的 Gemini 图片模型排在前面', () => {
    const models = getModelsByPlatform('antigravity')

    expect(models.indexOf('gemini-3.1-flash-image')).toBeLessThan(
      models.indexOf('gemini-2.5-flash-image')
    )
  })

  it('whitelist 模式会忽略通配符条目', () => {
    const mapping = buildModelMappingObject('whitelist', ['claude-*', 'gemini-2.5-flash'], [])
    expect(mapping).toEqual({
      'gemini-2.5-flash': 'gemini-2.5-flash'
    })
  })

  it('whitelist 模式会保留 GPT-5.4 官方快照的精确映射', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-2026-03-05'], [])

    expect(mapping).toEqual({
      'gpt-5.4-2026-03-05': 'gpt-5.4-2026-03-05'
    })
  })

  it('whitelist keeps GPT-5.4 mini exact mappings', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-mini'], [])

    expect(mapping).toEqual({
      'gpt-5.4-mini': 'gpt-5.4-mini'
    })
  })
})
