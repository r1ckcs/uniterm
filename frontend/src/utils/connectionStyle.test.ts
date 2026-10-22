import { describe, expect, it } from 'vitest'
import { colorCss, connectionStyle, effectiveStyle, iconComponent, normalizeColor, normalizeIcon } from './connectionStyle'
import type { ConnectionConfig, ConnectionGroup } from '../types/session'

const g = (id: string, extra: Partial<ConnectionGroup> = {}) => ({ id, name: id, ...extra }) as ConnectionGroup
const c = (extra: Partial<ConnectionConfig>) => ({ id: 'c', name: 'c', type: 'ssh', host: 'h', port: 22, ...extra }) as ConnectionConfig

describe('connection color/icon validation (same rules as the backend)', () => {
  it('accepts only palette keys, none and #rrggbb', () => {
    expect(normalizeColor('orange')).toBe('orange')
    expect(normalizeColor('none')).toBe('none')
    expect(normalizeColor('#A1B2C3')).toBe('#a1b2c3')
    for (const bad of ['#fff', 'rgb(1,2,3)', 'var(--x)', 'url(x)', 'Orange', 'red;background:url(x)', '#12345g', '', undefined, 42]) {
      expect(normalizeColor(bad), String(bad)).toBe('')
    }
    expect(normalizeIcon('olt')).toBe('olt')
    for (const bad of ['OLT', '../x.svg', 'https://x/y.svg', '<svg>', 'router ']) expect(normalizeIcon(bad), bad).toBe('')
  })

  it('never builds CSS from a free value', () => {
    expect(colorCss('blue')).toBe('var(--conn-blue)')
    expect(colorCss('#a1b2c3')).toBe('#a1b2c3')
    expect(colorCss('url(x)')).toBeUndefined()
    expect(colorCss('var(--accent)')).toBeUndefined()
    expect(iconComponent('router')).toBeTruthy()
    expect(iconComponent('../evil.svg')).toBeUndefined()
  })
})

describe('inheritance (session.EffectiveStyle)', () => {
  // POP PJT is orange + router; its subgroup OLTs has the olt icon.
  const groups = [g('pjt', { color: 'orange', icon: 'router' }), g('olts', { parentId: 'pjt', icon: 'olt' })]

  it('resolves color and icon each on its own, nearest group first', () => {
    expect(connectionStyle(c({ groupId: 'olts' }), groups)).toEqual({ color: 'orange', icon: 'olt' })
    expect(connectionStyle(c({ groupId: 'olts', color: 'blue' }), groups)).toEqual({ color: 'blue', icon: 'olt' })
  })

  it('"none" stops the walk and shows nothing', () => {
    expect(connectionStyle(c({ groupId: 'olts', color: 'none', icon: 'none' }), groups)).toEqual({ color: '', icon: '' })
    const withNone = [g('pjt', { color: 'orange' }), g('mid', { parentId: 'pjt', color: 'none' })]
    expect(connectionStyle(c({ groupId: 'mid' }), withNone).color).toBe('')
  })

  it('a cycle or a missing group ends the walk', () => {
    const cycle = [g('a', { parentId: 'b' }), g('b', { parentId: 'a' })]
    expect(connectionStyle(c({ groupId: 'a' }), cycle)).toEqual({ color: '', icon: '' })
    expect(connectionStyle(c({ groupId: 'gone' }), groups)).toEqual({ color: '', icon: '' })
  })

  it('stops after 32 groups', () => {
    const chain: ConnectionGroup[] = []
    for (let i = 0; i < 40; i++) chain.push(g('g' + i, { parentId: 'g' + (i + 1) }))
    chain[33] = g('g33', { parentId: 'g34', color: 'red' })
    expect(connectionStyle(c({ groupId: 'g0' }), chain).color).toBe('')
    chain[31] = g('g31', { parentId: 'g32', color: 'green' })
    expect(connectionStyle(c({ groupId: 'g0' }), chain).color).toBe('green')
  })

  it('groups inherit from their parents too', () => {
    expect(effectiveStyle({ parentId: 'pjt' }, groups)).toEqual({ color: 'orange', icon: 'router' })
  })
})
