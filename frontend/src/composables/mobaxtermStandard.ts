// MobaXterm 26.5 syntax profile "Custom: Standard keywords + Shell syntax",
// converted for JavaScript. MobaXterm conventions kept via rule options:
// - "¨" marks line start/end: rules match marker+line+marker (lineMarker).
// - "guard(word)guard" patterns color capture group 1 (colorGroup1).
// - Case-insensitive, like MobaXterm.
// Only two regex dialect fixes were needed: "[^\]" (not-a-backslash in
// MobaXterm) became "[^\\]", and a "]" opening a character class ("[]…]",
// "[^]]") is escaped. Underline has no equivalent (text color only), so the
// URL rule uses blue; Blinking is empty in this profile.
import type { UserHighlightRule } from './userHighlightRules'

export const MOBAXTERM_LINE_MARKER = '¨'

/** [MobaXterm color, JS pattern, uniTerm color] in the profile's order. */
export const MOBAXTERM_STANDARD: Array<[string, string, string]> = [
  ["Underline", "[^A-Za-z_&-](http(s)?://[A-Za-z0-9_.&?=%~#{}()@+-]+:?[A-Za-z0-9_./&?=%~#{}()@+-]+)[^A-Za-z0-9_-]", "theme:blue"],
  ["Red", "[^A-Za-z_&-](rejected|session closed|session disconnected|does not match|does not exist|(bad|wrong|incorrect|improper|invalid|unsupported|bad)( file| memory)? (descriptor|alloc(ation)?|addr(ess)?|owner(ship)?|arg(ument)?|param(eter)?|setting|length|filename)|not properly|improperly|(operation |connection |authentication |access |permission )?(denied|disallowed|not allowed|refused|problem|failed|failure|not permitted)|no [A-Za-z]+( [A-Za-z]+)? found|invalid|unsupported|not supported|seg(mentation )?fault|corruption|corrupted|corrupt|overflow|underrun|not ok|unimplemented|unsuccessfull|not implemented|permerrors?|fehlers?|errore|errors?|erreurs?|fejl|virhe|greka|erro|crash|crashed|core dump|fel|\\(ee\\)|\\(ni\\))[^A-Za-z_-]|[^\\\\](\\$\\([^(\\))¨]+\\)|`[^ `¨][^`¨]+`)[^¨]|[=>\"':.,;({\\[][ ]*(false|no valid signature found|no|ko)[ ]*[\\]=>\"':.,;)} ]| false    | no    | ko    | down    | inactive    |[^A-Za-z_&-]administratively down[^A-Za-z_&-]", "theme:red"],
  ["Green", "([^A-Za-z_&-](session opened|accepted|allowed|enabled|connected|erfolgreich|exitoso|successo|sucedido|framgångsrik|successfully|successful|succeeded|success)[^A-Za-z_-]|[=>\"':.,;({\\[][ ]*(true|yes|ok)[ ]*[\\]=>\"':.,;)} ])|(¨( *)?#[^¨]+)+|[^A-Za-z_&-](true|yes|ok|up|active)    |(¨![^¨]+)+", "theme:green"],
  ["Yellow", "([^A-Za-z_&-](\\[\\-w[A-Za-z-]+\\]|unassigned|shutdown|discarded|discarding|warn|caught signal [0-9]+|cannot|(connection (to (remote host|[a-z0-9.]+) )?)?(closed|terminated|stopped|not responding)|exited|no more [A-Za-z] available|unexpected|(command |binary |file )?not found|ooo?o?o?ps|out of (space|memory)|low (memory|disk)|(user )?unknown( user)?|disabled|disconnected|deprecated|refused|disconnect(ion)?|advertencia|avvertimento|attention|warnings?|achtung|exclamation|alerts?|warnungs?|advarsel|pedwarn|aviso|varoitus|upozorenje|peringatan|uyari|varning|avertissement|\\(ww\\)|\\(\\?\\?\\)|could not|unable to)[^A-Za-z_-])|([^\\\\](\\$[\\?@\\$][^¨]|\\$\\(\\([^(\\))]+\\)\\)[^¨]|\\$\\{[^\\}]+\\}[^¨]|\\$\\[[^\\]]+\\][^¨]|\\$[A-Za-z_0-9]+[^A-Za-z_0-9]))", "theme:yellow"],
  ["Blue", "[^\\\\](\\\\(033|e)(\\[(1;)?(0|30|31|32|33|34|35|36)?m|\\((0|b))|\\[[0-9.:,]+\\]|/dev/null|\\|\\||\\&\\&)[^¨]", "theme:blue"],
  ["Magenta", "([^0-9A-Za-z_&-]([0-9a-f][0-9a-f](:|-)[0-9a-f][0-9a-f](:|-)[0-9a-f][0-9a-f](:|-)[0-9a-f][0-9a-f](:|-)[0-9a-f][0-9a-f](:|-)[0-9a-f][0-9a-f]|localhost|([1-9]|[1-9][0-9]|1[0-9][0-9]|2[0-4][0-9]|25[0-4])\\.[0-9]+\\.[0-9]+\\.[0-9]+|null|none)[^0-9A-Za-z_-])|([;¨|°¤\\$\\>][ \\(]*(for(each)?|while|done|if|then|else|elif|fi|case|esac|endif|exit|eval|shift|read|continue|return)[ ;¨\\)])|[^A-Za-z_&-](interface )?(Fa[0-9/]+|Gi[0-9/]+|GigabitEthernet[0-9/]+|FastEthernet[0-9/]+|vlan[0-9]+|Dot11Radio[0-9\\\\.]+|dot[0-9][A-Za-z0-9\\\\.]+|bvi[0-9]+)[^A-Za-z_&-]", "theme:magenta"],
  ["Cyan", "(( |\\(|\"|'|\\[|\\])-(-)?[a-zA-Z0-9_-]+( |=|,|\\.|\\)|\"|'|\\[|\\]))|([^A-Za-z_&-](last (failed )?login:|launching|checking|loading|creating|building|important|booting|starting|notice|informational|informationen|informazioni|informação|oplysninger|informations?|info|información|informasi|note|\\(ii\\)|\\(\\!\\!\\))[^A-Za-z_-])|([ ;¨](builtin )?(setenv|export|unset|builtin|shopt|unalias|echo|printf|alias|function|bindkey|setopt|unsetopt|user access verification|switchport|logging event|no ip address|service-policy|vlan-range|spanning-tree|access-list|description|running-config|startup-config|radius-server|class-map|policy-map|media-type|ip address)[ ;¨])", "theme:cyan"],
]

export function mobaxtermStandardRules(): UserHighlightRule[] {
  return MOBAXTERM_STANDARD.map(([, pattern, color], i) => ({
    id: `moba${i + 1}`,
    pattern,
    kind: 'regex',
    color,
    caseSensitive: false,
    lineMarker: MOBAXTERM_LINE_MARKER,
    colorGroup1: true,
  }))
}
