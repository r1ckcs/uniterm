// Default contents of the single highlight rule list (highlightRules.json):
// seeded on first run and by "Restore defaults". Nothing here is applied
// directly — once seeded, the user's file is the only source.
//
// Order matters: earlier rules win overlaps, so specific network phrases
// come first ("administratively down" before "down"), then the MobaXterm
// "Standard keywords + Shell syntax" profile for general output.
import type { UserHighlightRule } from './userHighlightRules'
import { mobaxtermStandardRules } from './mobaxtermStandard'

// Palette chosen to stay readable on dark themes; the terminal's
// minimumContrast setting still applies to override colors.
const C = {
  red: '#ff4d4f',
  orange: '#fa8c16',
  yellow: '#fadb14',
  green: '#52c41a',
  blue: '#4096ff',
  gray: '#8c8c8c',
  purple: '#b37feb',
  teal: '#36cfc9',
}

let seq = 0
function kw(pattern: string, color: string, extra: Partial<UserHighlightRule> = {}): UserHighlightRule {
  return { id: `p${++seq}`, pattern, kind: 'keyword', color, wholeWord: true, ...extra }
}
function re(pattern: string, color: string, extra: Partial<UserHighlightRule> = {}): UserHighlightRule {
  return { id: `p${++seq}`, pattern, kind: 'regex', color, ...extra }
}

// Optical power readings followed by a dBm unit ("-23.768(dbm)",
// "-27.1 dBm"). Ranges follow the NOC's GPON RX limits: -8..-25 good,
// -25..-27 attention, below -27 critical, above -8 saturated. Positive
// readings are transmit powers (normal) and stay uncolored.
const DBM = '(?=\\s?\\(?dbm\\)?)'
const opticalRules: UserHighlightRule[] = [
  re(`-[0-7](?:\\.\\d+)?${DBM}`, C.red),                         // saturated
  re(`-(?:[89]|1\\d|2[0-4])(?:\\.\\d+)?${DBM}`, C.green),         // -8 .. -24.99
  re(`-2[56](?:\\.\\d+)?${DBM}`, C.yellow),                        // -25 .. -26.99
  re(`-(?:2[7-9]|[3-9]\\d)(?:\\.\\d+)?${DBM}`, C.red),             // <= -27
]

/** Label resolver for default group names (translated at seed time). */
export type GroupLabel = (slug: string) => string

const off = { enabled: false }

/** Network rules, grouped. Words that would be noisy in every terminal
 * (bare up/down, connected) start disabled. */
function networkRules(): Array<[string, UserHighlightRule[]]> {
  return [
    ['optical', opticalRules],
    ['macVlan', [
      // MAC: aa:bb:cc:dd:ee:ff, aa-bb-…, and Cisco dotted aabb.ccdd.eeff
      re('(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}', C.teal, { wholeWord: true }),
      re('[0-9A-Fa-f]{4}\\.[0-9A-Fa-f]{4}\\.[0-9A-Fa-f]{4}', C.teal),
      // VLAN ids: vlan203, VLAN 10, Vlan-interface100, vlan-id=203, pvid=3026
      re('\\bvlan(?:-interface)?\\s?\\d+\\b', C.purple),
      re('\\b(?:vlan-id|pvid)=\\d+', C.purple),
    ]],
    ['zteOlt', [
      kw('LOS', C.red, { caseSensitive: true }),
      kw('LOSi', C.red, { caseSensitive: true }),
      kw('LOFi', C.red, { caseSensitive: true }),
      kw('DyingGasp', C.orange),
      kw('dying-gasp', C.orange),
      kw('PowerOff', C.orange),
      kw('offline', C.red),
      kw('working', C.green),
      kw('online', C.green),
      kw('Logging', C.yellow),
      kw('N/A', C.gray, { caseSensitive: true }),
      re('gpon-(?:onu|olt)_\\d+/\\d+/\\d+(?::\\d+)?', C.blue),
      re('\\d+/\\d+/\\d+:\\d+', C.blue, { wholeWord: true }),
    ]],
    ['mikrotik', [
      re('link down\\b', C.red),
      re('link up\\b', C.green),
      // "loop-protect=on" is plain config; only an actual detection is an alert.
      re('loop detected', C.orange),
      re('login failure', C.orange),
      re('logged (?:in|out)', C.blue),
      re('disabled=yes', C.gray),
      re('disabled=no', C.green),
      kw('running', C.green),
      // Interface names (RouterOS lets a description follow the base name).
      re('\\b(?:ether|sfp-sfpplus|sfp28-|qsfpplus|qsfp28-|sfp|combo|wlan|bridge|bonding|pppoe-(?:in|out)|l2tp-(?:in|out)|ovpn-(?:in|out)|eoip-tunnel|gre-tunnel)\\d+(?:-\\d+)?\\b', C.blue),
    ]],
    ['ciscoHuawei', [
      re('administratively down', C.gray),
      re('\\*down\\b', C.gray),
      re('\\^down\\b', C.yellow),
      kw('err-disabled', C.red),
      kw('notconnect', C.yellow),
      kw('connected', C.green, off),
      re('\\b(?:DOWN|UP) state\\b', C.yellow),
      kw('down', C.red, off),
      kw('up', C.green, off),
      // Syslog severities: Cisco %FAC-SEV-MNEMONIC, Huawei %%01FAC/SEV/MNEMONIC
      re('%[A-Z0-9_]+-[0-3]-[A-Z0-9_]+', C.red, { caseSensitive: true }),
      re('%[A-Z0-9_]+-4-[A-Z0-9_]+', C.orange, { caseSensitive: true }),
      re('%[A-Z0-9_]+-[5-7]-[A-Z0-9_]+', C.blue, { caseSensitive: true }),
      re('%%\\d*[A-Z0-9_]+/[0-3]/[A-Z0-9_]+', C.red, { caseSensitive: true }),
      re('%%\\d*[A-Z0-9_]+/4/[A-Z0-9_]+', C.orange, { caseSensitive: true }),
      re('%%\\d*[A-Z0-9_]+/[5-7]/[A-Z0-9_]+', C.blue, { caseSensitive: true }),
      re('\\b(?:(?:Ten|Forty|Hundred|Twenty[Ff]ive)?GigabitEthernet|FastEthernet|Ethernet|XGigabitEthernet|GE|XGE|Eth-Trunk|Port-channel|Gi|Te|Fa|Po)\\d+(?:/\\d+)*(?:\\.\\d+)?\\b', C.blue),
    ]],
  ]
}

/** Build the default list with group labels in the user's language. Rule ids
 * are stable across calls, so restoring defaults yields the same ids. */
export function buildDefaultRules(label: GroupLabel): UserHighlightRule[] {
  seq = 0
  // Network rules first (more specific), then the MobaXterm profile, which
  // colors general terminal output (errors, warnings, IPs, shell syntax…).
  const groups: Array<[string, UserHighlightRule[]]> = [...networkRules(), ['mobaStandard', mobaxtermStandardRules()]]
  const out: UserHighlightRule[] = []
  for (const [slug, rules] of groups) {
    for (const r of rules) {
      out.push({ enabled: true, caseSensitive: false, wholeWord: false, ...r, id: `d${out.length + 1}`, group: label(slug) })
    }
  }
  return out
}
