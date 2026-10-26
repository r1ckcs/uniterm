// Connection names differ at the END ("OLT-ZTE-CORDOVIL" × "OLT-ZTE-PILAR"),
// so a long name is cut in the middle: "OLT-ZTE-…-CORDOVIL". The name is
// split once into a head and a tail (which keeps its separator, except a
// leading dot, which the ellipsis stands for); fitting
// them to the box is fitMiddle(), with a text measurer the component caches.

const SEPARATORS = /[-_./: ]/

// Tail = the shortest ending that starts at a separator and has 3–14
// characters after it ("-CORDOVIL", "-CORDOVIL-02"). For a domain name the
// segment before the last dot comes too (".netflex.local"), since ".local"
// alone tells nothing apart. Without a usable separator: the last 8
// characters. Names up to 12 characters are not split.
export function splitMiddle(text: string): { head: string; tail: string } {
  const s = text || ''
  if (s.length <= 12) return { head: s, tail: '' }
  for (let i = s.length - 2; i >= 4; i--) {
    if (!SEPARATORS.test(s[i - 1])) continue
    const after = s.length - i
    if (after > 14) break
    if (after < 3) continue
    let start = i - 1
    if (s[start] === '.') {
      const prev = s.lastIndexOf('.', start - 1)
      if (prev >= 3 && s.length - prev <= 20) start = prev
    }
    return { head: s.slice(0, start), tail: s.slice(start) }
  }
  const n = Math.min(8, Math.floor(s.length / 2))
  return { head: s.slice(0, s.length - n), tail: s.slice(s.length - n) }
}

export const ELLIPSIS = '…'
// The head always shows at least this many characters before the ellipsis.
export const MIN_HEAD = 4

// What to draw in `width` px: the whole text when it fits; else as much of
// the head as fits + "…" + the tail; when even MIN_HEAD + "…" + tail does
// not fit, the tail gives way from its start (still one "…"). `measure`
// returns a string's rendered width.
export function fitMiddle(text: string, width: number, measure: (s: string) => number): string {
  if (!text || measure(text) <= width) return text
  const split = splitMiddle(text)
  const head = split.head
  // A leading dot reads better as the ellipsis itself ("pop…netflex.local");
  // other separators stay ("S5…-PILAR").
  const tail = split.tail.startsWith('.') ? split.tail.slice(1) : split.tail
  const minHead = head.slice(0, Math.min(MIN_HEAD, head.length))
  if (measure(minHead + ELLIPSIS + tail) <= width) {
    // Largest head prefix that fits (binary search; at least MIN_HEAD).
    let lo = minHead.length
    let hi = head.length
    while (lo < hi) {
      const mid = Math.ceil((lo + hi) / 2)
      if (measure(head.slice(0, mid) + ELLIPSIS + tail) <= width) lo = mid
      else hi = mid - 1
    }
    return head.slice(0, lo) + ELLIPSIS + tail
  }
  // Keep the head stub; the tail loses characters from its start.
  let lo = 0
  let hi = tail.length
  while (lo < hi) {
    const mid = Math.ceil((lo + hi) / 2)
    if (measure(minHead + ELLIPSIS + tail.slice(tail.length - mid)) <= width) lo = mid
    else hi = mid - 1
  }
  return minHead + ELLIPSIS + tail.slice(tail.length - lo)
}
