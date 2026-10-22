// node render.mjs <jobs.json> : each job {svg, size, out}; renders with Google Chrome
// headless onto a transparent canvas and writes PNGs.
import { spawn } from 'node:child_process'
import { mkdtempSync, readFileSync, writeFileSync, existsSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
const jobs = JSON.parse(readFileSync(process.argv[2], 'utf8'))
const CH = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
const prof = mkdtempSync(join(process.env.TMPDIR || tmpdir(), 'chicon-'))
const ch = spawn(CH, ['--headless=new', '--remote-debugging-port=0', `--user-data-dir=${prof}`, '--no-first-run', 'about:blank'], { detached: true, stdio: 'ignore' })
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
let port
for (let i = 0; i < 100 && !port; i++) { await sleep(100); const f = join(prof, 'DevToolsActivePort'); if (existsSync(f)) port = readFileSync(f, 'utf8').split('\n')[0] }
const tabs = await (await fetch(`http://127.0.0.1:${port}/json`)).json()
const ws = new WebSocket(tabs.find((t) => t.type === 'page').webSocketDebuggerUrl)
await new Promise((r) => ws.addEventListener('open', r))
let n = 0; const pending = new Map()
ws.addEventListener('message', (e) => { const m = JSON.parse(e.data); if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id) } })
const send = (method, params = {}) => new Promise((r) => { const id = ++n; pending.set(id, r); ws.send(JSON.stringify({ id, method, params })) })
const ev = async (expr) => { const r = await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true }); if (r.result?.exceptionDetails) throw new Error(JSON.stringify(r.result.exceptionDetails)); return r.result?.result?.value }
for (const j of jobs) {
  const b64 = await ev(`(async () => {
    const svg = ${JSON.stringify(j.svg)}
    const img = new Image()
    img.src = 'data:image/svg+xml;base64,' + btoa(unescape(encodeURIComponent(svg)))
    await img.decode()
    const c = document.createElement('canvas'); c.width = c.height = ${j.size}
    const g = c.getContext('2d'); g.imageSmoothingQuality = 'high'
    g.drawImage(img, 0, 0, ${j.size}, ${j.size})
    return c.toDataURL('image/png').split(',')[1]
  })()`)
  writeFileSync(j.out, Buffer.from(b64, 'base64'))
}
console.log('rendered', jobs.length)
ws.close(); try { process.kill(-ch.pid) } catch {}
await sleep(300); try { rmSync(prof, { recursive: true, force: true }) } catch {}
process.exit(0)
