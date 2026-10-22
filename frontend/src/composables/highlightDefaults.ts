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
  theme: (key: string) => `theme:${key}`,
  red: '#ff4d4f',
  orange: '#fa8c16',
  yellow: '#fadb14',
  green: '#52c41a',
  blue: '#4096ff',
  gray: '#8c8c8c',
  purple: '#b37feb',
  teal: '#36cfc9',
}

// Every default rule has a fixed id ("d" + number). Saved files are merged
// with this list by id on load (stores/highlightRuleStore.ts), so an id must
// never change or be reused: a new rule takes the next unused number
// (NEXT_DEFAULT_ID below), wherever it sits in the list, and a removed rule's
// number is retired. Bump DEFAULTS_VERSION whenever the list changes.
const id = (n: number) => `d${n}`
function kw(n: number, pattern: string, color: string, extra: Partial<UserHighlightRule> = {}): UserHighlightRule {
  return { id: id(n), pattern, kind: 'keyword', color, wholeWord: true, ...extra }
}
function re(n: number, pattern: string, color: string, extra: Partial<UserHighlightRule> = {}): UserHighlightRule {
  return { id: id(n), pattern, kind: 'regex', color, ...extra }
}

// Optical power readings, in two layouts:
// - value followed by a dBm unit ("-23.768(dbm)", "Rx Power: -27.1dBm");
// - Huawei transceiver table, unit in the label ("RxPower(dBm)  -21.74
//   -26.02  1.00"): only the first value after the label (the current
//   reading) is colored, never the alarm thresholds.
// Ranges follow the NOC's GPON RX limits: -8..-25 good, -25..-27 attention,
// below -27 critical, above -8 saturated. They describe received power, so
// transmit readings stay uncolored: positive ones naturally, and negative
// SFP TX values ("Tx Power: -5.94dBm", "TxPower(dBm) -5.94") are claimed
// first in the terminal's own text color. No lookbehind (old WebKit), so the
// table form paints capture group 1.
const DBM = '(?=\\s?\\(?dbm\\)?)'
const optical = (value: string) => `${value}${DBM}|\\brx[ _-]?power\\s*\\(dbm\\)[\\s:]*(${value})(?![\\d.])`
const opticalRules: UserHighlightRule[] = [
  re(1, '\\btx[ _-]?power\\s*(?:\\(dbm\\))?\\s*:?\\s*(-?\\d+(?:\\.\\d+)?)', C.theme('foreground'), { colorGroup1: true }),
  re(2, optical('-[0-7](?:\\.\\d+)?'), C.red, { colorGroup1: true }),                 // saturated
  re(3, optical('-(?:[89]|1\\d|2[0-4])(?:\\.\\d+)?'), C.green, { colorGroup1: true }), // -8 .. -24.99
  re(4, optical('-2[56](?:\\.\\d+)?'), C.yellow, { colorGroup1: true }),                // -25 .. -26.99
  re(5, optical('-(?:2[7-9]|[3-9]\\d)(?:\\.\\d+)?'), C.red, { colorGroup1: true }),     // <= -27
]

// "display interface brief" (Huawei) and "show ip interface brief" (Cisco):
// the PHY/Protocol status columns right after an interface name (a token
// with a digit), so bare up/down elsewhere stays uncolored. One rule per
// column (anchored at line start, group 1 painted). Huawei's "*down"/
// "^down" and Cisco's "administratively down" have their own rules;
// "up(s)" is a spoofing loopback.
const IF_PREFIX = '^[a-z][\\w./:-]*\\d[\\w./:-]*\\s+(?:(?:unassigned|[\\d.]+)\\s+(?:yes|no)\\s+\\S+\\s+)?'
const STATUS = (word: string) => `(${word}(?:\\([a-z]+\\))?)(?=\\s|$)`
const statusCols = ([a, b]: [number, number], word: string, color: string): UserHighlightRule[] => [
  re(a, `${IF_PREFIX}${STATUS(word)}`, color, { colorGroup1: true }),
  re(b, `${IF_PREFIX}(?:administratively\\s+)?[*^]?(?:up|down)(?:\\([a-z]+\\))?\\s+${STATUS(word)}`, color, { colorGroup1: true }),
]

/** Label resolver for default group names (translated at seed time). */
export type GroupLabel = (slug: string) => string

const off = { enabled: false }

/** Network rules, grouped. Words that would be noisy in every terminal
 * (bare up/down, connected) start disabled. */
function networkRules(): Array<[string, UserHighlightRule[]]> {
  return [
    ['optical', opticalRules],
    ['ipv6', [
      // Full and ::-compressed forms, optional prefix length. Group 1 is a
      // consumed left guard (trimLead); MAC addresses and clock times never
      // contain "::" and have fewer than 8 groups, so they don't match.
      re(6, '(^|[^0-9a-z_&:.-])((?:[a-f0-9]{1,4}:){7}[a-f0-9]{1,4}|(?:[a-f0-9]{1,4}:){1,7}:|(?:[a-f0-9]{1,4}:){1,6}:[a-f0-9]{1,4}|(?:[a-f0-9]{1,4}:){1,5}(?::[a-f0-9]{1,4}){1,2}|(?:[a-f0-9]{1,4}:){1,4}(?::[a-f0-9]{1,4}){1,3}|(?:[a-f0-9]{1,4}:){1,3}(?::[a-f0-9]{1,4}){1,4}|(?:[a-f0-9]{1,4}:){1,2}(?::[a-f0-9]{1,4}){1,5}|[a-f0-9]{1,4}:(?::[a-f0-9]{1,4}){1,6}|:(?::[a-f0-9]{1,4}){1,7}|::(?=/\\d))(?:/\\d{1,3})?(?![0-9a-z_:-])', C.theme('magenta'), { trimLead: true }),
    ]],
    ['juniper', [
      // Physical: ge-0/0/0, xe-1/2/3:1 (channelized), with optional unit .0
      re(7, '\\b(?:ge|xe|et|fe|so|gr|lt|ip|vt|ms|sp|mt|pe|pd|ps|vcp|lsi|fti)-\\d+/\\d+/\\d+(?::\\d+)?(?:\\.\\d+)?\\b', C.blue),
      // Logical: ae0, reth0, lo0.0, irb.100, vlan.10, fxp0, em0, me0, st0.1, pp0
      re(8, '\\b(?:ae|reth|lo|fxp|em|me|vme|st|pp|bme|jsrv|dsc)\\d+(?:\\.\\d+)?\\b', C.blue),
      re(9, '\\b(?:irb|vlan)\\.\\d+\\b', C.blue),
    ]],
    ['macVlan', [
      // MAC: aa:bb:cc:dd:ee:ff, aa-bb-…, and Cisco dotted aabb.ccdd.eeff
      re(10, '(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}', C.teal, { wholeWord: true }),
      re(11, '[0-9A-Fa-f]{4}\\.[0-9A-Fa-f]{4}\\.[0-9A-Fa-f]{4}', C.teal),
      // VLAN ids: vlan203, VLAN 10, Vlan-interface100, vlan-id=203, pvid=3026
      re(12, '\\bvlan(?:-interface)?\\s?\\d+\\b', C.purple),
      re(13, '\\b(?:vlan-id|pvid)=\\d+', C.purple),
    ]],
    ['zteOlt', [
      kw(14, 'LOS', C.red, { caseSensitive: true }),
      kw(15, 'LOSi', C.red, { caseSensitive: true }),
      kw(16, 'LOFi', C.red, { caseSensitive: true }),
      kw(17, 'DyingGasp', C.orange),
      kw(18, 'dying-gasp', C.orange),
      kw(19, 'PowerOff', C.orange),
      kw(20, 'offline', C.red),
      kw(21, 'working', C.green),
      kw(22, 'online', C.green),
      kw(23, 'Logging', C.yellow),
      kw(24, 'N/A', C.gray, { caseSensitive: true }),
      re(25, 'gpon-(?:onu|olt)_\\d+/\\d+/\\d+(?::\\d+)?', C.blue),
      re(26, '\\d+/\\d+/\\d+:\\d+', C.blue, { wholeWord: true }),
    ]],
    ['mikrotik', [
      re(27, 'link down\\b', C.red),
      re(28, 'link up\\b', C.green),
      // "loop-protect=on" is plain config; only an actual detection is an alert.
      re(29, 'loop detected', C.orange),
      re(30, 'login failure', C.orange),
      re(31, 'logged (?:in|out)', C.blue),
      re(32, 'disabled=yes', C.gray),
      re(33, 'disabled=no', C.green),
      kw(34, 'running', C.green),
      // Interface names (RouterOS lets a description follow the base name).
      re(35, '\\b(?:ether|sfp-sfpplus|sfp28-|qsfpplus|qsfp28-|sfp|combo|wlan|bridge|bonding|pppoe-(?:in|out)|l2tp-(?:in|out)|ovpn-(?:in|out)|eoip-tunnel|gre-tunnel)\\d+(?:-\\d+)?\\b', C.blue),
    ]],
    ['ciscoHuawei', [
      re(36, 'administratively down', C.gray),
      re(37, '\\*down\\b', C.gray),
      re(38, '\\^down\\b', C.yellow),
      kw(39, 'err-disabled', C.red),
      kw(40, 'notconnect', C.yellow),
      kw(41, 'connected', C.green, off),
      re(42, '\\b(?:DOWN|UP) state\\b', C.yellow),
      ...statusCols([43, 44], 'down', C.red),
      ...statusCols([45, 46], 'up', C.green),
      kw(47, 'down', C.red, off),
      kw(48, 'up', C.green, off),
      // Syslog severities: Cisco %FAC-SEV-MNEMONIC, Huawei %%01FAC/SEV/MNEMONIC
      re(49, '%[A-Z0-9_]+-[0-3]-[A-Z0-9_]+', C.red, { caseSensitive: true }),
      re(50, '%[A-Z0-9_]+-4-[A-Z0-9_]+', C.orange, { caseSensitive: true }),
      re(51, '%[A-Z0-9_]+-[5-7]-[A-Z0-9_]+', C.blue, { caseSensitive: true }),
      re(52, '%%\\d*[A-Z0-9_]+/[0-3]/[A-Z0-9_]+', C.red, { caseSensitive: true }),
      re(53, '%%\\d*[A-Z0-9_]+/4/[A-Z0-9_]+', C.orange, { caseSensitive: true }),
      re(54, '%%\\d*[A-Z0-9_]+/[5-7]/[A-Z0-9_]+', C.blue, { caseSensitive: true }),
      // Cisco and Huawei interface names, including Huawei's 10GE/25GE/40GE/
      // 100GE ports, MEth, Vlanif, LoopBack and NULL; "GigabitEthernet 0/0/21"
      // (with a space, as typed in commands) only in slot/port form.
      re(55, '\\b(?:(?:Ten|Forty|Hundred|Twenty[Ff]ive)?GigabitEthernet|FastEthernet|Ethernet|XGigabitEthernet|\\d+GE|MultiGE|GE|XGE|Eth-Trunk|Port-channel|MEth|Vlanif|Vbdif|LoopBack|NULL|Tunnel|Gi|Te|Fa|Po)(?: ?\\d+(?:/\\d+)+|\\d+)(?:\\.\\d+)?\\b', C.blue),
    ]],
  ]
}

// The MobaXterm profile's rules, in profile order.
const MOBA_IDS = [56, 57, 58, 59, 60, 61, 62]

/** Version of the default list. Saved files record the version they were
 * last merged with; bump it on any change to buildDefaultRules' output
 * (a test pins the list's signature to it). */
export const DEFAULTS_VERSION = 1
/** Next unused default rule number (see the note on ids above). */
export const NEXT_DEFAULT_ID = 63

/** Build the default list with group labels in the user's language. Rule ids
 * are fixed, so restoring defaults yields the same ids. */
export function buildDefaultRules(label: GroupLabel): UserHighlightRule[] {
  // Network rules first (more specific), then the MobaXterm profile, which
  // colors general terminal output (errors, warnings, IPs, shell syntax…).
  const groups: Array<[string, UserHighlightRule[]]> = [...networkRules(), ['mobaStandard', mobaxtermStandardRules(MOBA_IDS)]]
  const out: UserHighlightRule[] = []
  for (const [slug, rules] of groups) {
    for (const r of rules) {
      out.push({ enabled: true, caseSensitive: false, wholeWord: false, ...r, group: label(slug) })
    }
  }
  return out
}
