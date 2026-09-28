// Dependency-free markdown helpers shared by surfaces that render markdown
// into v-html (AI messages, update changelog). Keep in sync with
// AIMessage.sanitize.test.ts, which mirrors sanitizeRenderedHtml.

// Sanitize markdown-produced HTML before it's assigned to v-html.
// This is defense in depth: every caller escapes its input first (see
// escapeHtml), so no raw tag/quote from model or user content can reach
// here. The strip-list below catches what the markdown renderer itself
// could synthesize (javascript: links, quote-less attribute breakouts).
export function sanitizeRenderedHtml(html: string): string {
  // Drop dangerous tags entirely (including their content).
  const dangerousTags = [
    'script', 'iframe', 'object', 'embed', 'style', 'form',
    'link', 'meta', 'base', 'svg', 'math',
  ]
  for (const tag of dangerousTags) {
    const re = new RegExp(`<${tag}\\b[\\s\\S]*?<\\/${tag}>`, 'gi')
    html = html.replace(re, '')
    const reSelf = new RegExp(`<${tag}\\b[^>]*\\/?>`, 'gi')
    html = html.replace(reSelf, '')
  }
  // Strip on*="..." event-handler attributes (any attribute starting with on),
  // whether separated from the previous attribute by whitespace or by the
  // tag-internal slash (<a href="x"/onclick="...">). The slash variant only
  // matches a quoted value so URLs like href="/online=1" stay intact.
  html = html.replace(/\s+on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi, '')
  html = html.replace(/\/on[a-z]+\s*=\s*("[^"]*"|'[^']*')/gi, '')
  // URL attributes: allowlist schemes instead of blacklisting. The browser's
  // URL parser drops ASCII tab/newline anywhere and leading C0 controls, so
  // "java\tscript:" or "\x01javascript:" slipped past a literal match.
  html = html.replace(
    /\s+(href|src|action|formaction|xlink:href)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi,
    (match, _attr: string, rawValue: string) => {
      const value = rawValue.replace(/^["']|["']$/g, '')
      return isSafeUrl(value) ? match : ''
    },
  )
  return html
}

// isSafeUrl normalizes a URL the way the browser's parser would (strip C0
// controls and spaces) and accepts only http(s), mailto, fragments and
// scheme-less relative URLs.
export function isSafeUrl(value: string): boolean {
  // eslint-disable-next-line no-control-regex
  const normalized = value.replace(/[\x00-\x20\x7f]/g, '').replace(/&(amp;)?#0*(9|10|13);?/gi, '')
  const scheme = /^([a-z][a-z0-9+.\-]*):/i.exec(normalized)
  if (!scheme) return true
  return ['http', 'https', 'mailto'].includes(scheme[1].toLowerCase())
}

// Escape HTML-significant characters (including quotes — a raw double quote
// in renderer-synthesized attributes would let `on*=` handlers break out).
// Shared by the markdown pipeline and surfaces that escape model/user text
// before v-html (AIMessage.vue).
export function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function renderInline(md: string): string {
  return md
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')
}

// Minimal markdown renderer covering the constructs release notes use:
// headings, unordered lists, bold, inline code, links, horizontal rules and
// paragraphs. The output must still go through sanitizeRenderedHtml before
// being bound to v-html.
export function renderMarkdownHtml(source: string): string {
  const lines = escapeHtml(source).split(/\r?\n/)
  const out: string[] = []
  let listOpen = false
  const closeList = () => {
    if (listOpen) {
      out.push('</ul>')
      listOpen = false
    }
  }
  for (const raw of lines) {
    const line = raw.trimEnd()
    if (!line.trim()) {
      closeList()
      continue
    }
    const heading = line.match(/^(#{1,6})\s+(.*)$/)
    if (heading) {
      closeList()
      const level = heading[1].length
      out.push(`<h${level}>${renderInline(heading[2])}</h${level}>`)
      continue
    }
    if (/^(-{3,}|\*{3,})$/.test(line.trim())) {
      closeList()
      out.push('<hr>')
      continue
    }
    const item = line.match(/^\s*[-*]\s+(.*)$/)
    if (item) {
      if (!listOpen) {
        out.push('<ul>')
        listOpen = true
      }
      out.push(`<li>${renderInline(item[1])}</li>`)
      continue
    }
    closeList()
    out.push(`<p>${renderInline(line)}</p>`)
  }
  closeList()
  return out.join('\n')
}
