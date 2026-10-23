// Runs before first paint, loaded from index.html as a file (not inline) so
// the Content-Security-Policy needs no 'unsafe-inline' for scripts (csp.go).

// Platform rem baseline (see style.css): macOS native baseline is
// 13pt, so scale the rem-based UI up there. Runs before first paint
// to avoid a flash of the wrong size.
if (/Mac|iPhone|iPad|iPod/.test(navigator.platform)) {
  document.documentElement.classList.add('mac')
}
// Apply the persisted UI font baseline before first paint (falls back
// to the platform default); the settings store re-applies the loaded
// value once settings are in.
try {
  const cached = parseInt(localStorage.getItem('uiFontSize') || '', 10)
  const base = Number.isFinite(cached) && cached >= 10 && cached <= 18
    ? cached
    : (/Mac|iPhone|iPad|iPod/.test(navigator.platform) ? 14 : 12)
  document.documentElement.style.fontSize = (base / 12) * 16 + 'px'
} catch {
  // storage unavailable — html.mac CSS default still applies
}
