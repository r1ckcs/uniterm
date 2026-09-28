import { describe, expect, it } from 'vitest'
import { matchTextSpans } from './highlightRules'
import { compileRuleSets, validateRule } from './userHighlightRules'
import { HIGHLIGHT_PRESETS, presetsWithState } from './highlightPresets'

const RED = '#ff4d4f'
const ORANGE = '#fa8c16'
const YELLOW = '#fadb14'
const GREEN = '#52c41a'
const BLUE = '#4096ff'
const GRAY = '#8c8c8c'
const PURPLE = '#b37feb'
const TEAL = '#36cfc9'

const preset = (id: string) => HIGHLIGHT_PRESETS.find(p => p.id === `preset:${id}`)!

/** Colored substrings of a line under the given presets, in order. */
function paint(line: string, ...ids: string[]): Array<[string, string]> {
  const rules = compileRuleSets(ids.map(preset))
  return matchTextSpans(line, rules).spans.map(s => [line.slice(s.start, s.end), s.color!])
}

/** Color of one exact substring (first occurrence of that painted text). */
function colorOf(line: string, text: string, ...ids: string[]): string | undefined {
  return paint(line, ...ids).find(([t]) => t === text)?.[1]
}

describe('presets are valid', () => {
  for (const p of HIGHLIGHT_PRESETS) {
    it(`${p.name}: every rule passes validation`, () => {
      for (const r of p.rules) {
        expect(validateRule(r), `${p.id} ${r.pattern}`).toEqual({ ok: true })
      }
      expect(compileRuleSets([p])).toHaveLength(p.rules.length)
    })
  }

  it('only MAC/VLAN is global by default; state overrides apply', () => {
    expect(HIGHLIGHT_PRESETS.filter(p => p.global).map(p => p.id)).toEqual(['preset:generic'])
    const s = presetsWithState({ 'preset:generic': false, 'preset:zte-olt': true })
    expect(s.find(p => p.id === 'preset:generic')!.global).toBe(false)
    expect(s.find(p => p.id === 'preset:zte-olt')!.global).toBe(true)
  })
})

describe('OLT ZTE', () => {
  it('colors ONU states', () => {
    const lines: Array<[string, string, string]> = [
      ['1/7/3:27    enable       disable      LOS          1(GPON)', 'LOS', RED],
      ['1/7/3:28    enable       disable      DyingGasp    1(GPON)', 'DyingGasp', ORANGE],
      ['1/7/3:29    enable       enable       working      1(GPON)', 'working', GREEN],
      ['1/7/3:30    enable       disable      OffLine      1(GPON)', 'OffLine', RED],
      ['1/7/3:31    enable       enable       Logging      1(GPON)', 'Logging', YELLOW],
      ['ONU 1/2/1:4 is online', 'online', GREEN],
    ]
    for (const [line, word, color] of lines) {
      expect(colorOf(line, word, 'zte-olt'), line).toBe(color)
    }
    expect(colorOf('1/7/3:27    enable       disable      LOS', '1/7/3:27', 'zte-olt')).toBe(BLUE)
  })

  it('does not match LOS inside other words or lower-case los', () => {
    expect(paint('CLOSED los LOSS', 'zte-olt')).toEqual([])
  })

  it('colors optical power by range', () => {
    const cases: Array<[string, string | undefined]> = [
      ['-5.100', RED],     // saturated
      ['-8.000', GREEN],
      ['-23.768', GREEN],
      ['-24.999', GREEN],
      ['-25.000', YELLOW],
      ['-26.500', YELLOW],
      ['-27.000', RED],
      ['-31.200', RED],
    ]
    for (const [v, color] of cases) {
      expect(colorOf(`gpon-onu_1/2/5:1   ${v}(dbm)`, v, 'zte-olt'), v).toBe(color)
    }
    // Unit variants and positive TX values
    expect(colorOf('Rx power: -26.1 dBm', '-26.1', 'zte-olt')).toBe(YELLOW)
    expect(colorOf('OLT Tx: 3.52(dbm)', '3.52', 'zte-olt')).toBeUndefined()
    // Not a power reading without the unit
    expect(colorOf('distance -27.5 km', '-27.5', 'zte-olt')).toBeUndefined()
    expect(colorOf('gpon-onu_1/2/5:3   N/A', 'N/A', 'zte-olt')).toBe(GRAY)
    expect(colorOf('gpon-onu_1/2/5:3   N/A', 'gpon-onu_1/2/5:3', 'zte-olt')).toBe(BLUE)
  })
})

describe('MikroTik', () => {
  it('colors link events and interfaces', () => {
    const log = 'sep/28 18:38:35 interface,info sfp-sfpplus2 - SITE-A<>SITE-B link down'
    expect(colorOf(log, 'link down', 'mikrotik')).toBe(RED)
    expect(colorOf(log, 'sfp-sfpplus2', 'mikrotik')).toBe(BLUE)
    expect(colorOf('ether1 link up (speed 1G, full duplex)', 'link up', 'mikrotik')).toBe(GREEN)
    expect(colorOf('ether5 loop detected, disabling port', 'loop detected', 'mikrotik')).toBe(ORANGE)
    expect(colorOf('login failure for user admin from 10.0.0.9 via ssh', 'login failure', 'mikrotik')).toBe(ORANGE)
  })

  it('does not flag loop-protect config as an alert', () => {
    expect(colorOf('set [ find default-name=sfp-sfpplus2 ] loop-protect=on', 'loop-protect', 'mikrotik')).toBeUndefined()
  })

  it('colors export flags', () => {
    const line = 'add address=10.0.100.1/24 disabled=yes interface="ether1 - teste" network=10.0.100.0'
    expect(colorOf(line, 'disabled=yes', 'mikrotik')).toBe(GRAY)
    expect(colorOf(line, 'ether1', 'mikrotik')).toBe(BLUE)
  })
})

describe('Cisco / Huawei', () => {
  it('prefers specific phrases over bare up/down', () => {
    const line = 'GigabitEthernet0/2   unassigned  YES unset  administratively down down'
    const painted = paint(line, 'cisco-huawei')
    expect(painted).toContainEqual(['administratively down', GRAY])
    expect(painted).toContainEqual(['down', RED])
    expect(painted).toContainEqual(['GigabitEthernet0/2', BLUE])
  })

  it('colors interface states', () => {
    expect(colorOf('Gi0/1   connected    10   a-full a-1000', 'connected', 'cisco-huawei')).toBe(GREEN)
    expect(colorOf('Gi0/3   err-disabled 10   auto   auto', 'err-disabled', 'cisco-huawei')).toBe(RED)
    expect(colorOf('Gi0/4   notconnect   10   auto   auto', 'notconnect', 'cisco-huawei')).toBe(YELLOW)
    expect(colorOf('GigabitEthernet0/0/1   *down  down  0%  0%', '*down', 'cisco-huawei')).toBe(GRAY)
    expect(colorOf('GigabitEthernet0/0/2   up     up    1%  2%', 'up', 'cisco-huawei')).toBe(GREEN)
  })

  it('colors syslog severities', () => {
    expect(colorOf('%LINK-3-UPDOWN: Interface Gi0/1, changed state to down', '%LINK-3-UPDOWN', 'cisco-huawei')).toBe(RED)
    expect(colorOf('%SYS-4-CONFIG_RESOLVE_FAILURE: x', '%SYS-4-CONFIG_RESOLVE_FAILURE', 'cisco-huawei')).toBe(ORANGE)
    expect(colorOf('%LINEPROTO-5-UPDOWN: Line protocol on Interface Gi0/1, changed state to up', '%LINEPROTO-5-UPDOWN', 'cisco-huawei')).toBe(BLUE)
    expect(colorOf('%%01IFNET/4/LINK_STATE(l)[0]:The line protocol', '%%01IFNET/4/LINK_STATE', 'cisco-huawei')).toBe(ORANGE)
    expect(colorOf('has entered the DOWN state.', 'DOWN state', 'cisco-huawei')).toBe(YELLOW)
  })
})

describe('MAC / VLAN', () => {
  it('colors MAC formats', () => {
    expect(colorOf('48:A9:8A:12:34:56 dynamic', '48:A9:8A:12:34:56', 'generic')).toBe(TEAL)
    expect(colorOf('mac 48-a9-8a-12-34-56', '48-a9-8a-12-34-56', 'generic')).toBe(TEAL)
    expect(colorOf('  10    48a9.8a12.3456    DYNAMIC     Gi0/1', '48a9.8a12.3456', 'generic')).toBe(TEAL)
  })

  it('does not treat times or IPv6 as MACs', () => {
    expect(paint('2026-09-28 17:52:21 fe80::1a2b:3c4d', 'generic')).toEqual([])
  })

  it('colors VLAN ids', () => {
    expect(colorOf('add interface=bridge1 name=vlan203 vlan-id=203', 'vlan203', 'generic')).toBe(PURPLE)
    expect(colorOf('add interface=bridge1 name=vlan203 vlan-id=203', 'vlan-id=203', 'generic')).toBe(PURPLE)
    expect(colorOf('add bridge=bridge1 pvid=3026', 'pvid=3026', 'generic')).toBe(PURPLE)
    expect(colorOf('interface Vlan-interface100', 'Vlan-interface100', 'generic')).toBe(PURPLE)
    expect(colorOf('switchport access vlan 10', 'vlan 10', 'generic')).toBe(PURPLE)
  })
})
