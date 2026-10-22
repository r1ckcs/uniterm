// Schema-drift guard: settings are persisted through the Go structs in
// backend/store/settings_store.go. A frontend settings key without a
// matching Go json tag is silently dropped by the JSON round-trip, so its
// setting resets on every restart (this bit showTabShortcutHints and
// hostListMenuStyle). These tests fail the moment a new DEFAULT_SETTINGS
// key is not mirrored on the Go side.
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { DEFAULT_SETTINGS } from './settings'

const here = dirname(fileURLToPath(import.meta.url))
const goStructsSrc = readFileSync(resolve(here, '../../../backend/store/settings_store.go'), 'utf-8')
const settingsStoreSrc = readFileSync(resolve(here, '../stores/settingsStore.ts'), 'utf-8')

// Keys persisted as opaque maps on the Go side (map[string]... transparently
// round-trips every child key): only the top-level key needs a Go tag.
const OPAQUE_MAP_KEYS = new Set(['keyboard', 'sidebarTabs'])

function collectLeafKeys(obj: object, prefix = '', out = new Set<string>()): Set<string> {
  for (const [key, value] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (OPAQUE_MAP_KEYS.has(path)) {
      out.add(path)
    } else if (value && typeof value === 'object' && !Array.isArray(value)) {
      collectLeafKeys(value, path, out)
    } else {
      out.add(path)
    }
  }
  return out
}

// Extract the json tag names from a Go struct block (brace-matched).
function goStructTags(src: string, structName: string): Set<string> {
  const marker = `type ${structName} struct {`
  const start = src.indexOf(marker)
  if (start === -1) throw new Error(`struct ${structName} not found in settings_store.go`)
  const open = src.indexOf('{', start)
  let depth = 0
  let end = open
  for (let i = open; i < src.length; i++) {
    if (src[i] === '{') depth++
    else if (src[i] === '}') {
      depth--
      if (depth === 0) {
        end = i
        break
      }
    }
  }
  const tags = new Set<string>()
  for (const m of src.slice(open, end).matchAll(/json:"([^"]+)"/g)) {
    tags.add(m[1].split(',')[0])
  }
  return tags
}

// Go struct that stores each nested settings object, by its key path.
// Everything else lives directly on AppSettings. When a nested settings
// struct is added on the Go side, register it here (an unregistered nested
// object fails the test instead of being checked against the wrong struct).
const NESTED_STRUCTS: Record<string, string> = {
  terminal: 'TerminalSettings',
  ai: 'AISettings',
  sftpBookmarks: 'SFTPBookmarks',
  mcp: 'MCPSettings',
  'mcp.tools': 'MCPToolToggles',
}

const tagsByStruct: Record<string, Set<string>> = {}
for (const s of new Set(['AppSettings', ...Object.values(NESTED_STRUCTS)])) {
  tagsByStruct[s] = goStructTags(goStructsSrc, s)
}

// Why a frontend leaf key would be dropped by the Go round-trip, or null if
// it survives: every segment of the path must be a json tag of the struct
// that holds it, walking AppSettings → nested structs.
function goTagProblem(path: string): string | null {
  const parts = path.split('.')
  let struct = 'AppSettings'
  for (let i = 0; i < parts.length; i++) {
    if (!tagsByStruct[struct].has(parts[i])) return `${path} (no "${parts[i]}" tag in ${struct})`
    if (i < parts.length - 1) {
      const prefix = parts.slice(0, i + 1).join('.')
      const next = NESTED_STRUCTS[prefix]
      if (!next) return `${path} (nested "${prefix}" has no struct in NESTED_STRUCTS)`
      struct = next
    }
  }
  return null
}

describe('settings schema parity (frontend ↔ backend/store/settings_store.go)', () => {
  it('every leaf key in DEFAULT_SETTINGS has a Go json tag', () => {
    const missing = [...collectLeafKeys(DEFAULT_SETTINGS)].map(goTagProblem).filter(Boolean)
    expect(
      missing,
      `settings keys dropped by the Go JSON round-trip — add them to backend/store/settings_store.go: ${missing.join(', ')}`
    ).toEqual([])
  })

  it('mergeSettings in settingsStore.ts re-maps every top-level key', () => {
    const start = settingsStoreSrc.indexOf('function mergeSettings')
    const body = settingsStoreSrc.slice(start, settingsStoreSrc.indexOf('\n}', start))
    const missing = Object.keys(DEFAULT_SETTINGS).filter(k => !new RegExp(`\\b${k}\\s*:`).test(body))
    expect(
      missing,
      `keys missing from mergeSettings (they reset to defaults on load): ${missing.join(', ')}`
    ).toEqual([])
  })
})
