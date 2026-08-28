import { describe, expect, it } from 'vitest'
import {
  isImageGenerationModel,
  prepareAccountTestModels
} from '../accountTestModels'
import type { ClaudeModel } from '@/types'

function model(id: string): ClaudeModel {
  return { id, type: 'model', display_name: id, created_at: '' }
}

describe('isImageGenerationModel', () => {
  it.each([
    ['gpt-image-1', true],
    ['gemini-2.5-flash-image', true],
    ['gemini-3.1-flash-image', true],
    ['grok-4-image', true],
    ['grok-4-video', true],
    ['grok-imagine', true],
    ['cogview-3', true]
  ])('classifies %s as an image/video generation model', (id, expected) => {
    expect(isImageGenerationModel(id)).toBe(expected)
  })

  it.each([
    ['gpt-5.5', false],
    ['gemini-2.0-flash', false],
    ['grok-4.5', false],
    ['claude-sonnet-5', false]
  ])('classifies %s as a text model', (id, expected) => {
    expect(isImageGenerationModel(id)).toBe(expected)
  })
})

describe('prepareAccountTestModels', () => {
  it('filters image-generation models for any platform', () => {
    const models = [
      model('gemini-2.0-flash'),
      model('gemini-2.5-flash-image'),
      model('gemini-3.1-flash-image')
    ]

    const result = prepareAccountTestModels(models, 'gemini')

    expect(result.map((m) => m.id)).toEqual(['gemini-2.0-flash'])
  })

  it('sorts gemini models by priority', () => {
    const models = [
      model('gemini-3-pro-preview'),
      model('gemini-2.5-flash'),
      model('gemini-2.0-flash')
    ]

    const result = prepareAccountTestModels(models, 'gemini')

    expect(result.map((m) => m.id)).toEqual([
      'gemini-2.5-flash',
      'gemini-3-pro-preview',
      'gemini-2.0-flash'
    ])
  })

  it('keeps original order for non-gemini platforms', () => {
    const models = [model('gpt-5.5'), model('gpt-5.4')]

    const result = prepareAccountTestModels(models, 'openai')

    expect(result.map((m) => m.id)).toEqual(['gpt-5.5', 'gpt-5.4'])
  })
})
