import type { AccountPlatform, ClaudeModel } from '@/types'

export const DEFAULT_OPENAI_TEST_MODEL = 'gpt-5.5'
export const DEFAULT_GROK_TEST_MODEL = 'grok-4.5'
export const DEFAULT_OPENCODE_TEST_MODEL = 'kimi-k2.6'

const PRIORITIZED_GEMINI_MODELS = [
  'gemini-2.5-flash',
  'gemini-2.5-pro',
  'gemini-3-flash-preview',
  'gemini-3-pro-preview',
  'gemini-2.0-flash'
]

export function isImageGenerationModel(modelId: string): boolean {
  const normalizedModelId = modelId.toLowerCase()
  return (
    normalizedModelId.startsWith('gpt-image-') ||
    (normalizedModelId.startsWith('gemini-') && normalizedModelId.includes('-image')) ||
    (normalizedModelId.startsWith('grok-') &&
      (normalizedModelId.includes('-image') || normalizedModelId.includes('-video'))) ||
    normalizedModelId === 'grok-imagine' ||
    normalizedModelId.startsWith('cogview')
  )
}

export function prepareAccountTestModels(
  models: ClaudeModel[],
  platform: AccountPlatform
): ClaudeModel[] {
  const textModels = models.filter((model) => !isImageGenerationModel(model.id))
  if (platform !== 'gemini' && platform !== 'antigravity') return textModels

  const priorityMap = new Map(
    PRIORITIZED_GEMINI_MODELS.map((modelId, index) => [modelId, index])
  )
  return [...textModels].sort((left, right) => {
    const leftPriority = priorityMap.get(left.id) ?? Number.MAX_SAFE_INTEGER
    const rightPriority = priorityMap.get(right.id) ?? Number.MAX_SAFE_INTEGER
    return leftPriority - rightPriority
  })
}

export function selectDefaultAccountTestModel(
  models: ClaudeModel[],
  platform: AccountPlatform
): string {
  if (models.length === 0) return ''
  if (platform === 'openai') {
    return models.find((model) => model.id === DEFAULT_OPENAI_TEST_MODEL)?.id ?? models[0].id
  }
  if (platform === 'grok') {
    return models.find((model) => model.id === DEFAULT_GROK_TEST_MODEL)?.id ?? models[0].id
  }
  if (platform === 'opencode') {
    return models.find((model) => model.id === DEFAULT_OPENCODE_TEST_MODEL)?.id ?? models[0].id
  }
  if (platform === 'gemini' || platform === 'antigravity') return models[0].id
  return models.find((model) => model.id.includes('sonnet'))?.id ?? models[0].id
}
