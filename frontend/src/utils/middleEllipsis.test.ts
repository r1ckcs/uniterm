import { describe, expect, it } from 'vitest'
import { fitMiddle, splitMiddle } from './middleEllipsis'

describe('middle ellipsis', () => {
  it('keeps the distinguishing end, separator included', () => {
    expect(splitMiddle('OLT-ZTE-CORDOVIL')).toEqual({ head: 'OLT-ZTE', tail: '-CORDOVIL' })
    expect(splitMiddle('SW-HUAWEI-S5732-PILAR')).toEqual({ head: 'SW-HUAWEI-S5732', tail: '-PILAR' })
    expect(splitMiddle('OLT-CORDOVIL-01')).toEqual({ head: 'OLT', tail: '-CORDOVIL-01' })
  })

  it('a domain keeps the segment before the last dot', () => {
    expect(splitMiddle('sw01.pop-cordovil.netflex.local')).toEqual({ head: 'sw01.pop-cordovil', tail: '.netflex.local' })
  })

  it('short names stay whole; no usable separator falls back to the last 8 chars', () => {
    expect(splitMiddle('PE01-REI')).toEqual({ head: 'PE01-REI', tail: '' })
    expect(splitMiddle('ABCDEFGHIJKLMNOPQRST')).toEqual({ head: 'ABCDEFGHIJKL', tail: 'MNOPQRST' })
    expect(splitMiddle('HW-AGGREGATIONSWITCHCORE')).toEqual({ head: 'HW-AGGREGATIONSW', tail: 'ITCHCORE' })
    expect(splitMiddle('')).toEqual({ head: '', tail: '' })
  })

  it('head + tail is always the full name', () => {
    for (const n of ['OLT-ZTE-CORDOVIL', 'x', 'MK-BORDA01', 'RDN-PJT22-SW-ACCESS-0001', 'sw02.pop-pilar.netflex.local']) {
      const { head, tail } = splitMiddle(n)
      expect(head + tail).toBe(n)
    }
  })
})

describe('fitting the cut to the box (1 px per character)', () => {
  const m = (s: string) => s.length

  it('whole when it fits; else head + "…" + tail with no gap and the separator kept', () => {
    expect(fitMiddle('SW-HUAWEI-S5732-PILAR', 21, m)).toBe('SW-HUAWEI-S5732-PILAR')
    expect(fitMiddle('SW-HUAWEI-S5732-PILAR', 19, m)).toBe('SW-HUAWEI-S5…-PILAR')
    expect(fitMiddle('sw01.pop-cordovil.netflex.local', 22, m)).toBe('sw01.pop…netflex.local')
  })

  it('the head never drops below 4 characters + "…"', () => {
    expect(fitMiddle('OLT-ZTE-C600-CORDOVIL-02', 17, m)).toBe('OLT-…-CORDOVIL-02')
    expect(fitMiddle('OLT-ZTE-C600-CORDOVIL-02', 20, m)).toBe('OLT-ZTE…-CORDOVIL-02')
  })

  it('when even that does not fit, the tail gives way from its start', () => {
    expect(fitMiddle('OLT-ZTE-C600-CORDOVIL-02', 13, m)).toBe('OLT-…DOVIL-02')
    expect(fitMiddle('OLT-ZTE-C600-CORDOVIL-02', 8, m)).toBe('OLT-…-02')
    expect(fitMiddle('OLT-ZTE-C600-CORDOVIL-02', 13, m).split('…').length).toBe(2)
  })
})

describe('item 46/47 markup', () => {
  const raw = import.meta.glob(
    ['../components/Sidebar.vue', '../components/GroupTreeItem.vue', '../components/TabItem.vue', '../components/StartTabContent.vue',
      '../components/SessionPicker.vue', '../components/Menu.vue', '../components/MenuItem.vue'],
    { query: '?raw', import: 'default', eager: true },
  ) as Record<string, string>
  const src = (n: string) => raw[`../components/${n}.vue`]

  it('connection names are cut in the middle in the sidebar, tabs, Sites and ⌘K', () => {
    for (const n of ['Sidebar', 'GroupTreeItem', 'TabItem', 'StartTabContent', 'SessionPicker']) {
      expect(src(n), n).toMatch(/<MiddleEllipsis :text=/)
    }
  })

  it('the sidebar edge is a keyboard separator with its value', () => {
    const s = src('Sidebar')
    expect(s).toMatch(/class="resize-handle"\s+role="separator"\s+aria-orientation="vertical"\s+tabindex="0"/)
    expect(s).toMatch(/:aria-valuenow="sidebarWidth"/)
    expect(s).toMatch(/@keydown="onResizeKeydown"/)
    expect(s).toMatch(/sidebarWidth: sidebarWidth\.value === SIDEBAR_DEFAULT \? 0 : sidebarWidth\.value/)
  })

  it('Sites opens on the alerts, with "All" one click away', () => {
    const s = src('StartTabContent')
    expect(s).toMatch(/const siteFilter = ref<SiteFilter>\(settingsStore\.settings\.connStatusCheck === false \? 'all' : 'alert'\)/)
    expect(s).toMatch(/class="start-empty-all" @click="siteFilter = 'all'"/)
  })

  it('menus are menu/menuitem and move with the keyboard', () => {
    expect(src('Menu')).toMatch(/role="menu"/)
    expect(src('Menu')).toMatch(/@keydown="onMenuKeydown"/)
    for (const k of ['ArrowDown', 'ArrowUp', 'Home', 'End', 'Escape', 'ArrowRight', 'ArrowLeft']) expect(src('Menu')).toContain(`'${k}'`)
    expect(src('MenuItem')).toMatch(/role="menuitem" tabindex="-1"/)
  })
})
