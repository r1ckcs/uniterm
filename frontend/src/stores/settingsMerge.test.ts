import { describe, expect, it } from 'vitest'
import { mergeSettings } from './settingsStore'
import { DEFAULT_SETTINGS, type AppSettings } from '../types/settings'

describe('mergeSettings', () => {
  it('keeps optional fields whose absence has a meaning', () => {
    const loaded = { ...DEFAULT_SETTINGS, listDensity: 'compact', connStatusCheck: false } as AppSettings
    const merged = mergeSettings(loaded)
    expect(merged.listDensity).toBe('compact')
    expect(merged.connStatusCheck).toBe(false)
  })

  it('leaves them absent when the file has none', () => {
    const merged = mergeSettings({ ...DEFAULT_SETTINGS } as AppSettings)
    expect(merged.listDensity).toBeUndefined()
    expect(merged.connStatusCheck).toBeUndefined()
  })
})
