import { describe, expect, it } from 'vitest'
import { groupKey, groupLabels, stripGroupToken } from './groupLabel'

describe('names without the group (item 50)', () => {
  it('drops "POP " and case from the group name', () => {
    expect(groupKey('POP Cordovil')).toBe('cordovil')
    expect(groupKey('Núcleo')).toBe('nucleo')
  })

  it('takes the group token out with its separator, anywhere as a whole segment', () => {
    const k = groupKey('POP Cordovil')
    expect(stripGroupToken('OLT-ZTE-C600-CORDOVIL', k)).toBe('OLT-ZTE-C600')
    expect(stripGroupToken('OLT-ZTE-C600-CORDOVIL-02', k)).toBe('OLT-ZTE-C600-02')
    expect(stripGroupToken('CORDOVIL-AGG-01', k)).toBe('AGG-01')
    expect(stripGroupToken('sw01.pop-cordovil.netflex.local', k)).toBe('sw01.pop.netflex.local')
  })

  it('keeps the name when the token is only part of a word or the whole name', () => {
    const k = groupKey('POP Cordovil')
    expect(stripGroupToken('CORDOVILNORTE-01', k)).toBe('CORDOVILNORTE-01')
    expect(stripGroupToken('Cordovil', k)).toBe('Cordovil')
    expect(stripGroupToken('PE01-REI', k)).toBe('PE01-REI')
  })

  it('accented names keep their other characters', () => {
    expect(stripGroupToken('ÓLT-SÃO-GONÇALO-01', groupKey('POP São Gonçalo'))).toBe('ÓLT-01')
  })

  it('multi-word groups match any separator', () => {
    expect(stripGroupToken('OLT-RIO-DAS-OSTRAS', groupKey('POP Rio das Ostras'))).toBe('OLT')
  })

  it('names that would collide after stripping keep their full name', () => {
    const m = groupLabels([
      { id: 'a', name: 'OLT-CORDOVIL' }, { id: 'b', name: 'OLT' }, { id: 'c', name: 'PE01-CORDOVIL' }, { id: 'd', name: 'Cordovil' },
    ], 'POP Cordovil')
    expect(m.get('a')).toBe('OLT-CORDOVIL')
    expect(m.get('b')).toBe('OLT')
    expect(m.get('c')).toBe('PE01')
    expect(m.get('d')).toBe('Cordovil')
  })
})

describe('item 50 markup', () => {
  const raw = import.meta.glob(['../components/GroupTreeItem.vue', '../components/StartTabContent.vue', '../components/MiddleEllipsis.vue', '../style.css'],
    { query: '?raw', import: 'default', eager: true }) as Record<string, string>

  it('the group tree and the Sites cards show the label, the full name stays in title / screen-reader text', () => {
    expect(raw['../components/GroupTreeItem.vue']).toMatch(/<MiddleEllipsis :text="labels\.get\(conn\.id\) \?\? conn\.name" :full="conn\.name" \/>/)
    expect(raw['../components/StartTabContent.vue']).toMatch(/<MiddleEllipsis :text="site\.labels\.get\(config\.id\) \?\? config\.name" :full="config\.name" \/>/)
    expect(raw['../components/MiddleEllipsis.vue']).toMatch(/:title="noTitle \? undefined : \(full \|\| text\)"/)
    expect(raw['../components/MiddleEllipsis.vue']).toMatch(/<span class="me-sr">\{\{ full \|\| text \}\}<\/span>/)
  })

  it('Sites rows keep a fixed column for the session badge', () => {
    expect(raw['../components/StartTabContent.vue']).toMatch(/<span class="site-row-session"><SessionStateBadge :info=/)
    expect(raw['../components/StartTabContent.vue']).toMatch(/\.site-row-session \{[^}]*flex: 0 0 1\.25rem;/)
  })

  it('the compact list drops a host that cannot keep ~8 characters', () => {
    expect(raw['../style.css']).toMatch(/\.density-compact \.conn-details \{[^}]*flex-wrap: wrap;[^}]*max-height: 1\.125rem;\s*overflow: hidden;/)
    expect(raw['../style.css']).toMatch(/\.density-compact \.conn-details \.conn-meta \{\s*flex: 1 0 8ch;\s*min-width: 8ch;/)
  })
})
