import { computed, ref } from 'vue'
import type { Language, Locale } from '../types/settings'
import { SUPPORTED_LOCALES } from '../types/settings'

import zhCN from './locales/zh-CN.json'
import zhTW from './locales/zh-TW.json'
import en from './locales/en.json'
import ja from './locales/ja.json'
import ko from './locales/ko.json'
import de from './locales/de.json'
import es from './locales/es.json'
import fr from './locales/fr.json'
import ru from './locales/ru.json'
import ptBR from './locales/pt-BR.json'

const messages: Record<Locale, Record<string, string>> = {
  'zh-CN': zhCN,
  'zh-TW': zhTW,
  en,
  ja,
  ko,
  de,
  es,
  fr,
  ru,
  'pt-BR': ptBR,
}

const currentLocale = ref<Locale>('en')

export const locale = computed(() => currentLocale.value)

const localePrefixMap: Record<string, Locale> = Object.fromEntries(
  SUPPORTED_LOCALES.filter((loc) => loc !== 'zh-CN' && loc !== 'zh-TW' && loc !== 'pt-BR').map((loc) => [loc, loc])
)

function resolveLocale(lang: Language): Locale {
  if (lang !== 'system') {
    if (SUPPORTED_LOCALES.includes(lang as Locale)) return lang as Locale
    return 'en'
  }

  if (typeof navigator === 'undefined' || !navigator.language) return 'en'

  const nav = navigator.language.toLowerCase()

  if (nav.startsWith('zh')) {
    if (nav.includes('tw') || nav.includes('hk') || nav.includes('mo')) return 'zh-TW'
    return 'zh-CN'
  }

  // Any Portuguese variant (pt, pt-BR, pt-PT) uses the Brazilian bundle.
  if (nav.startsWith('pt')) return 'pt-BR'

  for (const [prefix, loc] of Object.entries(localePrefixMap)) {
    if (nav.startsWith(prefix)) return loc
  }

  return 'en'
}

export function setLocale(lang: Language) {
  currentLocale.value = resolveLocale(lang)
}

// Plural forms live in "<key>.<category>" entries (Intl.PluralRules
// categories: one, other, ...), picked when params.count is a number. An
// optional "<key>.zero" wins for 0: CLDR files 0 under "one" in Portuguese,
// but "0 conexões" is what pt-BR reads. A
// bundle without them falls back to the plain key, so the locales that only
// carry "<key>" keep working.
function lookup(loc: Locale, key: string, count: unknown): string | undefined {
  const bundle = messages[loc]
  if (!bundle) return undefined
  if (typeof count === 'number') {
    const form = (count === 0 ? bundle[`${key}.zero`] : undefined)
      ?? bundle[`${key}.${new Intl.PluralRules(loc).select(count)}`] ?? bundle[`${key}.other`]
    if (form !== undefined) return form
  }
  return bundle[key]
}

function translate(loc: Locale, key: string, params?: Record<string, string | number>): string {
  const msg = lookup(loc, key, params?.count) ?? lookup('en', key, params?.count) ?? key
  if (!params) return msg
  return msg.replace(/\{(\w+)\}/g, (_, k) => String(params[k] ?? `{${k}}`))
}

export function useI18n() {
  function t(key: string, params?: Record<string, string | number>): string {
    return translate(currentLocale.value, key, params)
  }
  return { t, locale }
}

export function t(key: string, params?: Record<string, string | number>): string {
  return translate(currentLocale.value, key, params)
}
