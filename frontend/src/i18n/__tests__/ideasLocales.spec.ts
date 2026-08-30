import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

function messagePaths(value: unknown, prefix = ''): string[] {
  if (value == null || typeof value !== 'object') return [prefix]
  return Object.entries(value as Record<string, unknown>)
    .flatMap(([key, child]) => messagePaths(child, prefix ? `${prefix}.${key}` : key))
    .sort()
}

describe('ideas locale messages', () => {
  it('keeps the complete Ideas message structure aligned in Chinese and English', () => {
    expect(messagePaths(en.ideas)).toEqual(messagePaths(zh.ideas))
  })

  it('provides localized labels for the deployment-critical actions', () => {
    expect(zh.ideas.editor.submitRevision).toBe('提交修订审核')
    expect(en.ideas.editor.submitRevision).toBe('Submit Revision for Review')
    expect(zh.ideas.header.backToDashboard).toBe('返回控制台')
    expect(en.ideas.header.backToDashboard).toBe('Back to Dashboard')
  })
})
