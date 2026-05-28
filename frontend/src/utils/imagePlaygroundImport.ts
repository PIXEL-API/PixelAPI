export const IMAGE_PLAYGROUND_IMPORT_URL =
  (import.meta.env.VITE_IMAGE_PLAYGROUND_URL as string | undefined)?.trim().replace(/\/+$/, '') ||
  'https://image.ai-pixel.online'

export interface ImagePlaygroundImportPayload {
  version: 1
  source: 'pixel-api'
  sourceName: string
  apiBaseUrl: string
  apiKey: string
  keyId: number
  keyName: string
  provider: 'pixel'
  model: 'gpt-image-2'
  issuedAt: number
}

export interface BuildImagePlaygroundImportUrlInput {
  apiBaseUrl: string
  apiKey: string
  keyId: number
  keyName: string
  sourceName?: string
}

function encodeBase64UrlUtf8(value: string): string {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }

  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}

function normalizeApiBaseUrl(value: string): string {
  return value.trim().replace(/\/+$/, '')
}

export function buildImagePlaygroundImportUrl(input: BuildImagePlaygroundImportUrlInput): string {
  const apiBaseUrl = normalizeApiBaseUrl(input.apiBaseUrl)
  if (!apiBaseUrl) {
    throw new Error('API base URL is required')
  }
  if (!input.apiKey.trim()) {
    throw new Error('API key is required')
  }

  const payload: ImagePlaygroundImportPayload = {
    version: 1,
    source: 'pixel-api',
    sourceName: input.sourceName?.trim() || 'Pixel API',
    apiBaseUrl,
    apiKey: input.apiKey.trim(),
    keyId: input.keyId,
    keyName: input.keyName.trim() || `API Key ${input.keyId}`,
    provider: 'pixel',
    model: 'gpt-image-2',
    issuedAt: Date.now()
  }

  const encodedPayload = encodeBase64UrlUtf8(JSON.stringify(payload))
  return `${IMAGE_PLAYGROUND_IMPORT_URL}/#/import/pixel?payload=${encodedPayload}`
}
