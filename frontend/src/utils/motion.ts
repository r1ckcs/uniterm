// Honour the OS "reduce motion" setting in JS-driven scrolling (the CSS
// rule in style.css covers transitions and animations, not scrollIntoView).
export function prefersReducedMotion(): boolean {
  try {
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

export function scrollBehavior(): ScrollBehavior {
  return prefersReducedMotion() ? 'auto' : 'smooth'
}
