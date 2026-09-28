// Local command-risk classifier for the built-in agent.
//
// The model labels each command with a risk level, but the model can be
// steered by prompt injection (remote output it reads back), so its label is
// only a lower bound: the effective risk is the stricter of the model's label
// and this local classification. Mirrors backend/mcp/risk.go.

export type RiskLevel = 'read' | 'write' | 'dangerous'

const ORDER: Record<RiskLevel, number> = { read: 0, write: 1, dangerous: 2 }

export function maxRisk(a: RiskLevel, b: RiskLevel): RiskLevel {
  return ORDER[a] >= ORDER[b] ? a : b
}

const WRAPPERS = new Set(['sudo', 'doas', 'nohup', 'time', 'nice', 'env', 'command', 'builtin'])

const DANGER_HEADS = new Set([
  'mkfs', 'shutdown', 'reboot', 'halt', 'poweroff', 'init', 'fdisk', 'parted',
  'wipefs', 'shred', 'eval', 'exec', 'source', '.', 'xargs', 'crontab',
])

const INTERPRETERS = new Set([
  'sh', 'bash', 'zsh', 'dash', 'ksh', 'fish', 'csh', 'tcsh', 'ash',
  'perl', 'ruby', 'node', 'nodejs', 'php', 'lua', 'osascript',
  'pwsh', 'powershell', 'cmd', 'cmd.exe',
])

const WRITE_HEADS = new Set([
  'rm', 'rmdir', 'mv', 'cp', 'mkdir', 'touch', 'chmod', 'chown', 'chgrp', 'ln',
  'tee', 'truncate', 'install', 'rsync', 'dd', 'kill', 'pkill', 'killall',
  'useradd', 'userdel', 'usermod', 'passwd', 'mount', 'umount', 'iptables',
  'nft', 'ufw', 'apt', 'apt-get', 'yum', 'dnf', 'zypper', 'pacman', 'apk',
  'brew', 'pip', 'pip3', 'npm', 'yarn', 'pnpm', 'gem', 'cargo', 'go',
  'systemctl', 'service', 'docker', 'podman', 'kubectl', 'helm', 'git',
  'scp', 'curl', 'wget', 'patch', 'unzip', 'tar', 'sed', 'awk', 'vi', 'vim',
  'nano', 'emacs', 'reset', 'mysql', 'psql', 'redis-cli',
])

const DANGEROUS_PATHS = ['/etc/', '/boot/', '/sys/', '/dev/', '/proc/sys/', '~/.ssh', '.bashrc', '.zshrc', '.profile']

export function classifyCommand(cmd: string): RiskLevel {
  const joined = cmd.toLowerCase()
  // Opaque execution hides the real command from any classifier.
  if (/\$\(|`|<\(|>\(/.test(cmd)) return 'dangerous'
  // Fork bomb shapes.
  if (joined.includes(':|:') || joined.includes('|:&')) return 'dangerous'

  let overall: RiskLevel = 'read'
  for (const seg of cmd.split(/[;\n|&]/)) {
    overall = maxRisk(overall, classifySegment(seg))
    if (overall === 'dangerous') break
  }
  return overall
}

function classifySegment(raw: string): RiskLevel {
  const seg = raw.trim()
  if (!seg) return 'read'
  let words = seg.split(/\s+/)
  while (words.length > 1 && (WRAPPERS.has(words[0].toLowerCase()) || /^[A-Za-z_][A-Za-z0-9_]*=/.test(words[0]))) {
    words = words.slice(1)
  }
  const head = (words[0].split('/').pop() || '').toLowerCase()
  const lower = seg.toLowerCase()
  const flags = words.slice(1).map(w => w.toLowerCase())

  if (DANGER_HEADS.has(head) || head.startsWith('mkfs')) return 'dangerous'
  if (INTERPRETERS.has(head) || head.startsWith('python')) return 'dangerous'
  if (head === 'find' && flags.some(f => ['-exec', '-execdir', '-delete', '-ok', '-okdir'].includes(f))) return 'dangerous'
  if (head === 'rm') {
    const recursive = flags.some(f => f === '--recursive' || /^-[a-z]*r/i.test(f))
    const force = flags.some(f => f === '--force' || /^-[a-z]*f/i.test(f))
    if (recursive && force) return 'dangerous'
  }
  if (head === 'dd' && lower.includes('of=/dev/')) return 'dangerous'
  if ((head === 'chmod' || head === 'chown') && flags.some(f => /^-[a-z]*r/i.test(f)) && /\s\/(\s|$)/.test(seg)) return 'dangerous'

  for (const p of DANGEROUS_PATHS) {
    const redirected = new RegExp(`>>?\\s*${p.replace(/[.*+?^${}()|[\]\\/~]/g, '\\$&')}`).test(lower)
    if (redirected && !lower.includes('/dev/null')) return 'dangerous'
    if (WRITE_HEADS.has(head) && head !== 'curl' && head !== 'wget' && head !== 'git' && lower.includes(' ' + p)) return 'dangerous'
  }

  if (/(^|[^>&0-9])>>?/.test(seg.replace(/[0-9]?>>?\s*\/dev\/null/g, ''))) return 'write'
  if (WRITE_HEADS.has(head)) return 'write'
  return 'read'
}
