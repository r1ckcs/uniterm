// Color and icon of connections and groups (CONTRATO-cor-icone v1.1).
// Values come from the vault too (written by another device), so only closed
// values are ever turned into styling: a palette key maps to a theme token,
// a #rrggbb that passed the same check as the backend is used as is, an icon
// key maps to a bundled component. Nothing else reaches CSS or a URL.
import type { Component } from 'vue'
import { BrickWall, Box, Network, RadioTower, Router, Server, Split } from '@lucide/vue'
import type { ConnectionColor, ConnectionConfig, ConnectionGroup, ConnectionIcon } from '../types/session'
import { brandTile } from '../components/icons/brandTile'

export const PALETTE_KEYS = ['red', 'orange', 'amber', 'green', 'cyan', 'blue', 'purple', 'gray'] as const
export type PaletteKey = typeof PALETTE_KEYS[number]

export const ICON_KEYS = ['olt', 'mikrotik', 'huawei', 'juniper', 'switch', 'router', 'server', 'firewall', 'radio', 'generic'] as const
export type IconKey = typeof ICON_KEYS[number]

const ICONS: Record<IconKey, Component> = {
  olt: Split,
  mikrotik: brandTile('M', 'IconMikrotik'),
  huawei: brandTile('H', 'IconHuawei'),
  juniper: brandTile('J', 'IconJuniper'),
  switch: Network,
  router: Router,
  server: Server,
  firewall: BrickWall,
  radio: RadioTower,
  generic: Box,
}

const HEX = /^#[0-9a-fA-F]{6}$/

// Same rules as session.NormalizeColor: palette key, "none" or #rrggbb
// (lowercased); anything else is "" (inherit).
export function normalizeColor(c: unknown): string {
  if (typeof c !== 'string') return ''
  if (c === 'none' || (PALETTE_KEYS as readonly string[]).includes(c)) return c
  return HEX.test(c) ? c.toLowerCase() : ''
}

// Same rules as session.NormalizeIcon.
export function normalizeIcon(i: unknown): string {
  if (typeof i !== 'string') return ''
  return i === 'none' || (ICON_KEYS as readonly string[]).includes(i) ? i : ''
}

export function isPaletteKey(c: string): c is PaletteKey {
  return (PALETTE_KEYS as readonly string[]).includes(c)
}

const MAX_GROUP_DEPTH = 32

export interface EffectiveStyle {
  color: string // palette key, #rrggbb, or '' for none
  icon: string // icon key, or '' for the type icon
}

// Mirrors session.EffectiveStyle: own value, else the nearest ancestor
// group's, each of color and icon on its own; "none" stops the walk and
// shows nothing; at most 32 groups are visited, a missing group ends it, so
// a cycle from the vault cannot loop.
export function effectiveStyle(
  item: { color?: ConnectionColor | string; icon?: ConnectionIcon | string; groupId?: string; parentId?: string },
  groups: ConnectionGroup[],
): EffectiveStyle {
  const byId = new Map(groups.map(g => [g.id, g]))
  let color = normalizeColor(item.color)
  let icon = normalizeIcon(item.icon)
  let next = item.groupId ?? item.parentId
  for (let depth = 0; next && (color === '' || icon === '') && depth < MAX_GROUP_DEPTH; depth++) {
    const g = byId.get(next)
    if (!g) break
    if (color === '') color = normalizeColor(g.color)
    if (icon === '') icon = normalizeIcon(g.icon)
    next = g.parentId
  }
  return { color: color === 'none' ? '' : color, icon: icon === 'none' ? '' : icon }
}

export function connectionStyle(c: ConnectionConfig, groups: ConnectionGroup[]): EffectiveStyle {
  return effectiveStyle({ color: c.color, icon: c.icon, groupId: c.groupId }, groups)
}

// A group's own style for display: its value, else its ancestors'.
export function groupStyle(g: ConnectionGroup, groups: ConnectionGroup[]): EffectiveStyle {
  return effectiveStyle({ color: g.color, icon: g.icon, parentId: g.parentId }, groups)
}

// CSS color for a resolved color: a theme token for a palette key, the
// validated hex, else undefined. Re-checked here so a caller cannot pass
// an unvalidated string through.
export function colorCss(color: string): string | undefined {
  if (isPaletteKey(color)) return `var(--conn-${color})`
  return HEX.test(color) ? color.toLowerCase() : undefined
}

export function iconComponent(icon: string): Component | undefined {
  return (ICON_KEYS as readonly string[]).includes(icon) ? ICONS[icon as IconKey] : undefined
}
