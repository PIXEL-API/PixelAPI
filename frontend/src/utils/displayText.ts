/** Repair UTF-8 text that was accidentally decoded as Latin-1. */
const mojibakeMarkers = /[ÃÂÆÇÐÑØÙÝÞßàáâãäåæçèéêëìíîïðñòóôõö÷øùúûüýþÿ锟�]/g

const markerScore = (value: string): number =>
  (value.match(mojibakeMarkers)?.length ?? 0) + (value.match(/�/g)?.length ?? 0) * 3

export function displayText(value: string | null | undefined): string {
  if (!value || markerScore(value) === 0) return value ?? ''
  try {
    const bytes = Uint8Array.from(value, (character) => {
      const code = character.codePointAt(0) ?? 0
      return code <= 0xff ? code : 0x3f
    })
    const decoded = new TextDecoder('utf-8', { fatal: true }).decode(bytes)
    return markerScore(decoded) < markerScore(value) ? decoded : value
  } catch {
    return value
  }
}
