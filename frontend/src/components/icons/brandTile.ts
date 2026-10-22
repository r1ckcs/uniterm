import { defineComponent, h } from 'vue'

// Vendor marks for the connection icon list (mikrotik / huawei / juniper):
// a rounded tile with the vendor initial, drawn like a lucide icon (24-unit
// box, currentColor stroke). No vendor logo or external file is loaded.
export function brandTile(letter: string, name: string) {
  return defineComponent({
    name,
    props: { size: { type: [Number, String], default: 24 } },
    setup(props) {
      return () => h('svg', {
        xmlns: 'http://www.w3.org/2000/svg',
        width: props.size,
        height: props.size,
        viewBox: '0 0 24 24',
        fill: 'none',
        stroke: 'currentColor',
        'stroke-width': 2,
        'stroke-linecap': 'round',
        'stroke-linejoin': 'round',
        'aria-hidden': 'true',
      }, [
        h('rect', { x: 3, y: 3, width: 18, height: 18, rx: 4 }),
        h('text', {
          x: 12, y: 16.2, 'text-anchor': 'middle', 'font-size': 11, 'font-weight': 800,
          'font-family': 'system-ui, sans-serif', fill: 'currentColor', stroke: 'none',
        }, letter),
      ])
    },
  })
}
