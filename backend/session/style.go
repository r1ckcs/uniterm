package session

import "strings"

// Color and icon of a connection or group (CONTRATO-cor-icone.md). Both are
// optional and both also arrive from the vault, written by another device,
// so only closed values pass: a palette key or #rrggbb for the color, a key
// of the icon list for the icon. Anything else is dropped to "" (no color
// or icon: inherit from the group).

// ColorNone is an explicit "no color": the connection does not inherit its
// group's color.
const ColorNone = "none"

// IconNone is an explicit "no icon": the default type icon, not the group's.
const IconNone = "none"

// ColorPalette is the fixed palette (key → hex shown by the UI).
var ColorPalette = map[string]string{
	"red":    "#e5484d",
	"orange": "#f46414",
	"amber":  "#f5a524",
	"green":  "#30a46c",
	"cyan":   "#22d3ee",
	"blue":   "#4f63e6",
	"purple": "#8e4ec6",
	"gray":   "#8b93a7",
}

// ConnectionIcons is the closed icon list (keys only; the UI maps each to
// a bundled icon).
var ConnectionIcons = map[string]bool{
	"olt": true, "mikrotik": true, "huawei": true, "juniper": true,
	"switch": true, "router": true, "server": true, "firewall": true,
	"radio": true, "generic": true,
}

// NormalizeColor returns c if it is a palette key, "none" or #rrggbb
// (lowercased), else "".
func NormalizeColor(c string) string {
	if c == ColorNone {
		return c
	}
	if _, ok := ColorPalette[c]; ok {
		return c
	}
	if len(c) != 7 || c[0] != '#' {
		return ""
	}
	for _, r := range c[1:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return ""
		}
	}
	return strings.ToLower(c)
}

// NormalizeIcon returns i if it is in ConnectionIcons or "none", else "".
func NormalizeIcon(i string) string {
	if i == IconNone || ConnectionIcons[i] {
		return i
	}
	return ""
}

// SanitizeStyles normalizes every color and icon in data, in place.
func SanitizeStyles(data *ConnectionStoreData) {
	for i := range data.Groups {
		g := &data.Groups[i]
		g.Color, g.Icon = NormalizeColor(g.Color), NormalizeIcon(g.Icon)
	}
	for i := range data.Connections {
		c := &data.Connections[i]
		c.Color, c.Icon = NormalizeColor(c.Color), NormalizeIcon(c.Icon)
	}
}

// maxGroupDepth bounds the walk up the group tree (vault data may hold a
// cycle).
const maxGroupDepth = 32

// EffectiveStyle is the color and icon a connection shows: its own, else
// the nearest ancestor group's that has one. "none" stops the walk and
// shows nothing. Each of color and icon is resolved on its own.
func EffectiveStyle(c ConnectionConfig, groups []ConnectionGroup) (color, icon string) {
	byID := make(map[string]ConnectionGroup, len(groups))
	for _, g := range groups {
		byID[g.ID] = g
	}
	color, icon = NormalizeColor(c.Color), NormalizeIcon(c.Icon)
	next := c.GroupId
	for depth := 0; next != nil && (color == "" || icon == "") && depth < maxGroupDepth; depth++ {
		g, ok := byID[*next]
		if !ok {
			break
		}
		if color == "" {
			color = NormalizeColor(g.Color)
		}
		if icon == "" {
			icon = NormalizeIcon(g.Icon)
		}
		next = g.ParentId
	}
	if color == ColorNone {
		color = ""
	}
	if icon == IconNone {
		icon = ""
	}
	return color, icon
}
