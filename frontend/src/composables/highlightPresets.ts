// Bundled network highlight presets. They are ordinary rule sets marked
// builtin (read-only in the UI; duplicate to customize). Whether a preset is
// global is user state stored in highlightRules.json (presetGlobals); the
// `global` value here is only the default. Non-global presets are picked per
// connection like any user set (ConnectionConfig.highlightSets).
//
// Rule order matters: on overlapping matches the first rule wins, so the
// more specific phrases ("administratively down", "*down") come before the
// bare words they contain.
import type { HighlightRuleSet, UserHighlightRule } from './userHighlightRules'

export const PRESET_PREFIX = 'preset:'

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

export const HIGHLIGHT_PRESETS: HighlightRuleSet[] = [
  {
    id: `${PRESET_PREFIX}generic`,
    name: 'MAC / VLAN',
    global: true,
    builtin: true,
    rules: [
      // MAC: aa:bb:cc:dd:ee:ff, aa-bb-…, and Cisco dotted aabb.ccdd.eeff
      re('(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}', C.teal, { wholeWord: true }),
      re('[0-9A-Fa-f]{4}\\.[0-9A-Fa-f]{4}\\.[0-9A-Fa-f]{4}', C.teal),
      // VLAN ids: vlan203, VLAN 10, Vlan-interface100, vlan-id=203, pvid=3026
      re('\\bvlan(?:-interface)?\\s?\\d+\\b', C.purple),
      re('\\b(?:vlan-id|pvid)=\\d+', C.purple),
    ],
  },
  {
    id: `${PRESET_PREFIX}zte-olt`,
    name: 'OLT ZTE',
    global: false,
    builtin: true,
    rules: [
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
      ...opticalRules,
      re('gpon-(?:onu|olt)_\\d+/\\d+/\\d+(?::\\d+)?', C.blue),
      re('\\d+/\\d+/\\d+:\\d+', C.blue, { wholeWord: true }),
    ],
  },
  {
    id: `${PRESET_PREFIX}mikrotik`,
    name: 'MikroTik',
    global: false,
    builtin: true,
    rules: [
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
      ...opticalRules,
    ],
  },
  {
    id: `${PRESET_PREFIX}cisco-huawei`,
    name: 'Cisco / Huawei',
    global: false,
    builtin: true,
    rules: [
      re('administratively down', C.gray),
      re('\\*down\\b', C.gray),
      re('\\^down\\b', C.yellow),
      kw('err-disabled', C.red),
      kw('notconnect', C.yellow),
      kw('connected', C.green),
      re('\\b(?:DOWN|UP) state\\b', C.yellow),
      kw('down', C.red),
      kw('up', C.green),
      // Syslog severities: Cisco %FAC-SEV-MNEMONIC, Huawei %%01FAC/SEV/MNEMONIC
      re('%[A-Z0-9_]+-[0-3]-[A-Z0-9_]+', C.red, { caseSensitive: true }),
      re('%[A-Z0-9_]+-4-[A-Z0-9_]+', C.orange, { caseSensitive: true }),
      re('%[A-Z0-9_]+-[5-7]-[A-Z0-9_]+', C.blue, { caseSensitive: true }),
      re('%%\\d*[A-Z0-9_]+/[0-3]/[A-Z0-9_]+', C.red, { caseSensitive: true }),
      re('%%\\d*[A-Z0-9_]+/4/[A-Z0-9_]+', C.orange, { caseSensitive: true }),
      re('%%\\d*[A-Z0-9_]+/[5-7]/[A-Z0-9_]+', C.blue, { caseSensitive: true }),
      re('\\b(?:(?:Ten|Forty|Hundred|Twenty[Ff]ive)?GigabitEthernet|FastEthernet|Ethernet|XGigabitEthernet|GE|XGE|Eth-Trunk|Port-channel|Gi|Te|Fa|Po)\\d+(?:/\\d+)*(?:\\.\\d+)?\\b', C.blue),
      ...opticalRules,
    ],
  },
]

/** Presets with the user's global/per-session choice applied. */
export function presetsWithState(presetGlobals: Record<string, boolean> = {}): HighlightRuleSet[] {
  return HIGHLIGHT_PRESETS.map(p =>
    p.id in presetGlobals ? { ...p, global: presetGlobals[p.id] } : p,
  )
}
