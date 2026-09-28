import { describe, expect, it } from 'vitest'
import { matchTextSpans } from './highlightRules'
import { compileRules, validateRule } from './userHighlightRules'
import { buildDefaultRules } from './highlightDefaults'
import en from '../i18n/locales/en.json'
import ptBR from '../i18n/locales/pt-BR.json'
import zhCN from '../i18n/locales/zh-CN.json'

const RED = '#ff4d4f'
const ORANGE = '#fa8c16'
const YELLOW = '#fadb14'
const GREEN = '#52c41a'
const BLUE = '#4096ff'
const GRAY = '#8c8c8c'
const PURPLE = '#b37feb'
const TEAL = '#36cfc9'
const MAGENTA = 'theme:magenta'

const SLUG: Record<string, string> = {
  ipv6: 'ipv6', juniper: 'juniper',
  generic: 'macVlan', 'zte-olt': 'zteOlt', mikrotik: 'mikrotik', 'cisco-huawei': 'ciscoHuawei',
}
const DEFAULTS = buildDefaultRules(slug => slug)

/** Colored substrings of a line under the given default groups (plus the
 * optical group, shared by the network groups), in order. Disabled rules
 * are switched on so every rule is exercised. */
function paint(line: string, ...ids: string[]): Array<[string, string]> {
  const wanted = new Set(['optical', ...ids.map(i => SLUG[i])])
  const rules = compileRules(DEFAULTS.filter(r => wanted.has(r.group!)).map(r => ({ ...r, enabled: true })))
  return matchTextSpans(line, rules).spans.map(s => [line.slice(s.start, s.end), s.color!])
}

/** Color of one exact substring (first occurrence of that painted text). */
function colorOf(line: string, text: string, ...ids: string[]): string | undefined {
  return paint(line, ...ids).find(([t]) => t === text)?.[1]
}

describe('default list', () => {
  it('every default rule passes validation', () => {
    for (const r of DEFAULTS) {
      expect(validateRule(r), `${r.group} ${r.pattern}`).toEqual({ ok: true })
    }
  })

  it('has stable ids and translated group labels', () => {
    const a = buildDefaultRules(s => `L:${s}`)
    const b = buildDefaultRules(s => `L:${s}`)
    expect(a.map(r => r.id)).toEqual(b.map(r => r.id))
    expect(new Set(a.map(r => r.id)).size).toBe(a.length)
    expect(a.every(r => r.group!.startsWith('L:'))).toBe(true)
  })

  it('every default group has a label in en, pt-BR and zh-CN', () => {
    const slugs = [...new Set(DEFAULTS.map(r => r.group!))]
    for (const bundle of [en, ptBR, zhCN] as Array<Record<string, string>>) {
      for (const slug of slugs) expect(bundle[`hl.group.${slug}`], slug).toBeTruthy()
    }
  })

  it('keeps words that are noisy in every terminal disabled', () => {
    const off = DEFAULTS.filter(r => r.enabled === false).map(r => r.pattern).sort()
    expect(off).toEqual(['connected', 'down', 'up'])
  })

  it('network rules come before the MobaXterm profile', () => {
    const firstMoba = DEFAULTS.findIndex(r => r.group === 'mobaStandard')
    expect(firstMoba).toBeGreaterThan(0)
    expect(DEFAULTS.slice(firstMoba).every(r => r.group === 'mobaStandard')).toBe(true)
    expect(DEFAULTS.filter(r => r.group === 'mobaStandard')).toHaveLength(7)
  })
})

describe('MobaXterm Standard + Shell profile', () => {
  const moba = compileRules(DEFAULTS.filter(r => r.group === 'mobaStandard'))
  const color = (line: string, text: string) =>
    matchTextSpans(line, moba).spans.find(s => line.slice(s.start, s.end).trim().replace(/[:,]$/, '') === text)?.color

  it('colors errors, warnings, success and info with theme colors', () => {
    expect(color('ssh: connect to host x port 22: Connection refused', 'Connection refused')).toBe('theme:red')
    expect(color('Permission denied (publickey).', 'Permission denied')).toBe('theme:red')
    expect(color('warning: unable to resolve host', 'warning')).toBe('theme:yellow')
    expect(color('Starting daemon... success', 'success')).toBe('theme:green')
    expect(color('Last login: Mon Sep 28 17:52:21 2026', 'Last login')).toBe('theme:cyan')
  })

  it('uses the line marker for start-of-line constructs and never colors it', () => {
    const line = '# this is a comment'
    const [span] = matchTextSpans(line, moba).spans
    expect(line.slice(span.start, span.end)).toBe(line)
    expect(span.color).toBe('theme:green')
    expect(matchTextSpans('x # not a comment', moba).spans.find(s => s.color === 'theme:green')).toBeUndefined()
  })

  it('colors shell syntax and network tokens', () => {
    expect(color('echo $(whoami) done', '$(whoami)')).toBe('theme:red')
    expect(color('interface GigabitEthernet0/1', 'interface GigabitEthernet0/1')).toBe('theme:magenta')
    expect(color('from 10.0.0.1 now', '10.0.0.1')).toBe('theme:magenta')
    expect(color('see https://example.com/x', 'https://example.com/x')).toBe('theme:blue')
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

describe('IPv6', () => {
  it.each([
    ['inet6 2001:db8::1/64 scope global', '2001:db8::1/64'],
    ['fe80::1a2b:3c4d%eth0', 'fe80::1a2b:3c4d'],
    ['addr 2001:0db8:0000:0000:0000:ff00:0042:8329 ok', '2001:0db8:0000:0000:0000:ff00:0042:8329'],
    ['route ::/0 via fe80::1', '::/0'],
    ['nexthop ::1', '::1'],
  ])('colors %s', (line, addr) => {
    expect(colorOf(line, addr, 'ipv6')).toBe(MAGENTA)
  })

  it.each([
    'mac 48:a9:8a:12:34:56 dynamic',
    'at 17:52:21 today',
    'ratio 1:2:3',
    'std::vector<int>',
    'x :: y',
  ])('leaves %s alone', (line) => {
    expect(paint(line, 'ipv6')).toEqual([])
  })
})

describe('Juniper', () => {
  it('colors physical interfaces with units and channels', () => {
    const line = 'ge-0/0/0.0              up    up   inet     10.0.0.1/30'
    expect(colorOf(line, 'ge-0/0/0.0', 'juniper')).toBe(BLUE)
    expect(colorOf('xe-1/2/3:1   up    down', 'xe-1/2/3:1', 'juniper')).toBe(BLUE)
    expect(colorOf('et-0/0/49 up up', 'et-0/0/49', 'juniper')).toBe(BLUE)
  })

  it('colors logical interfaces', () => {
    for (const ifname of ['ae0', 'ae12.100', 'lo0.0', 'irb.100', 'vlan.10', 'fxp0', 'em0', 'reth1', 'st0.1']) {
      expect(colorOf(`${ifname}   up    up`, ifname, 'juniper'), ifname).toBe(BLUE)
    }
  })

  it('does not match inside words', () => {
    expect(paint('hello0 system1 fe-male stem0', 'juniper')).toEqual([])
  })
})
