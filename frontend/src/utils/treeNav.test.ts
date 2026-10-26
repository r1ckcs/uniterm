import { describe, expect, it } from 'vitest'
import { treeKeyAction, treeRowInfo, typeaheadKey, type TreeRow } from './treeNav'

const rows: TreeRow[] = [
  { key: 'g:fav', kind: 'group', label: 'Favoritos', level: 1, expanded: true },
  { key: 'f:1', kind: 'conn', label: 'MK-BORDA01', level: 2, parentKey: 'g:fav' },
  { key: 'g:rei', kind: 'group', label: 'POP REI', level: 1, expanded: true },
  { key: 'g:rei-core', kind: 'group', label: 'Núcleo', level: 2, parentKey: 'g:rei', expanded: false },
  { key: 'c:2', kind: 'conn', label: 'PE01-REI', level: 2, parentKey: 'g:rei' },
  { key: 'c:3', kind: 'conn', label: 'PILAR-AGG', level: 2, parentKey: 'g:rei' },
  { key: 'g:pilar', kind: 'group', label: 'POP Pilar', level: 1, expanded: false },
]
const key = (k: string, shiftKey = false) => ({ key: k, shiftKey })

describe('sidebar tree keyboard', () => {
  it('numbers siblings for aria-posinset/-setsize', () => {
    const info = treeRowInfo(rows)
    expect(info.get('g:rei')).toMatchObject({ posinset: 2, setsize: 3 })
    expect(info.get('c:3')).toMatchObject({ posinset: 3, setsize: 3 })
    expect(info.get('f:1')).toMatchObject({ posinset: 1, setsize: 1 })
  })

  it('↑↓ move, Home/End jump, without wrapping', () => {
    expect(treeKeyAction(rows, 'g:fav', key('ArrowDown'))).toEqual({ type: 'focus', key: 'f:1' })
    expect(treeKeyAction(rows, 'g:fav', key('ArrowUp'))).toEqual({ type: 'focus', key: 'g:fav' })
    expect(treeKeyAction(rows, 'g:pilar', key('ArrowDown'))).toEqual({ type: 'focus', key: 'g:pilar' })
    expect(treeKeyAction(rows, 'c:2', key('Home'))).toEqual({ type: 'focus', key: 'g:fav' })
    expect(treeKeyAction(rows, 'c:2', key('End'))).toEqual({ type: 'focus', key: 'g:pilar' })
  })

  it('→ opens a closed group, then enters it; ← closes, then climbs', () => {
    expect(treeKeyAction(rows, 'g:pilar', key('ArrowRight'))).toEqual({ type: 'expand', key: 'g:pilar' })
    expect(treeKeyAction(rows, 'g:rei', key('ArrowRight'))).toEqual({ type: 'focus', key: 'g:rei-core' })
    expect(treeKeyAction(rows, 'c:2', key('ArrowRight'))).toEqual({ type: 'none' })
    expect(treeKeyAction(rows, 'g:rei', key('ArrowLeft'))).toEqual({ type: 'collapse', key: 'g:rei' })
    expect(treeKeyAction(rows, 'c:3', key('ArrowLeft'))).toEqual({ type: 'focus', key: 'g:rei' })
    expect(treeKeyAction(rows, 'g:rei-core', key('ArrowLeft'))).toEqual({ type: 'focus', key: 'g:rei' })
  })

  it('Enter activates; Shift+F10 and the menu key open the menu', () => {
    expect(treeKeyAction(rows, 'c:2', key('Enter'))).toEqual({ type: 'activate', key: 'c:2' })
    expect(treeKeyAction(rows, 'c:2', key('F10', true))).toEqual({ type: 'menu', key: 'c:2' })
    expect(treeKeyAction(rows, 'c:2', key('F10'))).toEqual({ type: 'none' })
    expect(treeKeyAction(rows, 'g:rei', key('ContextMenu'))).toEqual({ type: 'menu', key: 'g:rei' })
  })

  it('typeahead jumps to the next name, accent- and case-insensitive', () => {
    expect(typeaheadKey(rows, 'g:fav', 'p')).toBe('g:rei')
    expect(typeaheadKey(rows, 'g:rei', 'p')).toBe('c:2')
    expect(typeaheadKey(rows, 'c:2', 'pp')).toBe('c:3')
    expect(typeaheadKey(rows, 'g:fav', 'pil')).toBe('c:3')
    expect(typeaheadKey(rows, 'g:fav', 'nu')).toBe('g:rei-core')
    expect(typeaheadKey(rows, 'g:fav', 'zz')).toBeNull()
  })
})

describe('keyboard and screen-reader markup', () => {
  const raw = import.meta.glob(
    ['../components/Sidebar.vue', '../components/GroupTreeItem.vue', '../components/TabsList.vue', '../components/TabItem.vue',
      '../components/StartTabContent.vue', '../components/ConnectionStylePicker.vue', '../components/TerminalStatusBar.vue'],
    { query: '?raw', import: 'default', eager: true },
  ) as Record<string, string>
  const src = (n: string) => raw[`../components/${n}.vue`]

  it('the sidebar is a tree with treeitems, groups and one tab stop', () => {
    expect(src('Sidebar')).toMatch(/role="tree"/)
    expect(src('Sidebar')).toMatch(/role: 'treeitem'/)
    expect(src('Sidebar')).toMatch(/tabindex: treeTabKey\.value === key \? 0 : -1/)
    expect(src('Sidebar')).toMatch(/'aria-expanded'/)
    expect(src('Sidebar')).toMatch(/'aria-level'/)
    expect(src('GroupTreeItem')).toMatch(/role="group"/)
    expect(src('GroupTreeItem')).toMatch(/v-bind="tree\.attrs\(/)
  })

  it('tabs are a tablist whose label carries the state', () => {
    expect(src('TabsList')).toMatch(/role="tablist"/)
    expect(src('TabItem')).toMatch(/role="tab"/)
    expect(src('TabItem')).toMatch(/:aria-selected="isActive"/)
    expect(src('TabItem')).toMatch(/:aria-label="tabAriaLabel"/)
    expect(src('TabItem')).toMatch(/tab\.a11yDown/)
    expect(src('TabItem')).toMatch(/tab\.a11yBroadcast/)
  })

  it('clickable spans became labelled buttons', () => {
    expect(src('TabsList')).toMatch(/<button\s+type="button"\s+class="tab-more-btn"[^>]*:aria-label=/)
    expect(src('TabsList')).not.toMatch(/<span class="tab-more-btn"/)
    expect(src('Sidebar')).toMatch(/<button type="button" class="filter-trigger"[^>]*:aria-label=/)
    expect(src('StartTabContent')).toMatch(/<button type="button" class="start-filter-btn"[^>]*:aria-label=/)
    expect(src('Sidebar')).toMatch(/class="sidebar-tab"[^>]*:aria-label=/)
  })

  it('color and icon swatches have a name; the footer speaks the latency', () => {
    expect(src('ConnectionStylePicker')).toMatch(/a11y\.colorOption/)
    expect(src('ConnectionStylePicker')).toMatch(/a11y\.iconOption/)
    expect(src('TerminalStatusBar')).toMatch(/:aria-label="latencySpoken"/)
  })
})
