import { describe, expect, it } from 'vitest'
import { splitMiddle } from './middleEllipsis'

describe('middle ellipsis', () => {
  it('keeps the distinguishing end of similar names', () => {
    expect(splitMiddle('OLT-ZTE-CORDOVIL')).toEqual({ head: 'OLT-ZTE-', tail: 'CORDOVIL' })
    expect(splitMiddle('OLT-ZTE-PILAR')).toEqual({ head: 'OLT-ZTE-', tail: 'PILAR' })
    expect(splitMiddle('sw01.pop-rei.netflex.local')).toEqual({ head: 'sw01.pop-rei.netflex.', tail: 'local' })
  })

  it('short names stay whole; no separator falls back to the last 8 chars', () => {
    expect(splitMiddle('PE01-REI')).toEqual({ head: 'PE01-REI', tail: '' })
    expect(splitMiddle('ABCDEFGHIJKLMNOPQRST')).toEqual({ head: 'ABCDEFGHIJKL', tail: 'MNOPQRST' })
    expect(splitMiddle('')).toEqual({ head: '', tail: '' })
  })

  it('a too-short last segment takes the one before; a too-long one falls back to 8 chars', () => {
    expect(splitMiddle('HW-AGGREGATIONSWITCHCORE')).toEqual({ head: 'HW-AGGREGATIONSW', tail: 'ITCHCORE' })
    expect(splitMiddle('OLT-CORDOVIL-01')).toEqual({ head: 'OLT-', tail: 'CORDOVIL-01' })
  })

  it('head + tail is always the full name', () => {
    for (const n of ['OLT-ZTE-CORDOVIL', 'x', 'MK-BORDA01', 'RDN-PJT22-SW-ACCESS-0001']) {
      const { head, tail } = splitMiddle(n)
      expect(head + tail).toBe(n)
    }
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
