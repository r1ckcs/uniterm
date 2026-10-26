// Connection names differ at the END ("OLT-ZTE-CORDOVIL" × "OLT-ZTE-PILAR"),
// so a long name is cut in the middle: "OLT-ZTE-…CORDOVIL". No text
// measuring: the name is split once into a head that may shrink (CSS
// ellipsis) and a tail that always shows.

const SEPARATORS = /[-_./: ]/

// Tail = the shortest ending that starts after a separator and has 3–14
// characters ("CORDOVIL", "CORDOVIL-01"), else the last 8 characters. Short
// names are not split.
export function splitMiddle(text: string): { head: string; tail: string } {
  const s = text || ''
  if (s.length <= 12) return { head: s, tail: '' }
  for (let i = s.length - 2; i >= 4; i--) {
    if (!SEPARATORS.test(s[i - 1])) continue
    const tail = s.slice(i)
    if (tail.length > 14) break
    if (tail.length >= 3) return { head: s.slice(0, i), tail }
  }
  const n = Math.min(8, Math.floor(s.length / 2))
  return { head: s.slice(0, s.length - n), tail: s.slice(s.length - n) }
}
