import { describe, expect, it } from 'vitest'
import en from './locales/en.json'
import ptBR from './locales/pt-BR.json'
import zhCN from './locales/zh-CN.json'

// en is the source of truth; pt-BR and zh-CN are maintained in this fork and
// must stay complete. The other bundles fall back to en key by key.
const BUNDLES: Record<string, Record<string, string>> = { 'pt-BR': ptBR, 'zh-CN': zhCN }
const EN = en as Record<string, string>

const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort()

describe('i18n bundles', () => {
  for (const [loc, bundle] of Object.entries(BUNDLES)) {
    it(`${loc} has exactly the en keys`, () => {
      const missing = Object.keys(EN).filter((k) => !(k in bundle))
      const extra = Object.keys(bundle).filter((k) => !(k in EN))
      expect(missing, 'missing').toEqual([])
      expect(extra, 'extra').toEqual([])
    })

    it(`${loc} keeps the en placeholders`, () => {
      const broken = Object.keys(EN).filter(
        (k) => k in bundle && placeholders(EN[k]).join() !== placeholders(bundle[k]).join()
      )
      expect(broken).toEqual([])
    })

    it(`${loc} has no empty values`, () => {
      expect(Object.keys(bundle).filter((k) => !bundle[k].trim())).toEqual([])
    })
  }

  it('pt-BR has no Chinese text left over', () => {
    const cjk = /[぀-ヿ一-鿿]/
    expect(Object.keys(ptBR).filter((k) => cjk.test((ptBR as Record<string, string>)[k]))).toEqual([])
  })
})

describe('plural forms', () => {
  it('pick the singular and plural by count, per locale', async () => {
    const { t, setLocale } = await import('./index')
    setLocale('pt-BR')
    expect(t('startTab.connectionsCount', { count: 1 })).toBe('1 conexão')
    expect(t('startTab.connectionsCount', { count: 3 })).toBe('3 conexões')
    expect(t('startTab.connectionsCount', { count: 0 })).toBe('0 conexões')
    setLocale('en')
    expect(t('startTab.connectionsCount', { count: 1 })).toBe('1 connection')
    expect(t('startTab.connectionsCount', { count: 2 })).toBe('2 connections')
    setLocale('zh-CN')
    expect(t('startTab.connectionsCount', { count: 5 })).toBe('5 个连接')
    // A locale without the plural entries falls back to its plain key.
    setLocale('de')
    expect(t('startTab.connectionsCount', { count: 1 })).not.toContain('startTab')
  })
})
