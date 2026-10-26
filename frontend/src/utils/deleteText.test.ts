import { describe, expect, it } from 'vitest'
import { setLocale, t } from '../i18n'
import { deleteConnectionsPrompt, deletedNotice, nameList } from './deleteText'

const conns = (n: number) => Array.from({ length: n }, (_, i) => ({ name: `C${i + 1}`, host: `10.0.0.${i + 1}` }))

describe('delete prompt names what goes', () => {
  it('pt-BR: one with host, three by name, twelve with "e mais"', () => {
    setLocale('pt-BR')
    expect(deleteConnectionsPrompt(t, [{ name: 'PE01-REI', host: '10.20.2.1' }])).toBe('Excluir PE01-REI (10.20.2.1)?')
    expect(deleteConnectionsPrompt(t, conns(3))).toBe('Excluir 3 conexões: C1, C2 e C3?')
    expect(deleteConnectionsPrompt(t, conns(12))).toBe('Excluir 12 conexões: C1, C2, C3 e mais 9?')
    expect(deleteConnectionsPrompt(t, conns(2))).toBe('Excluir 2 conexões: C1 e C2?')
  })

  it('no "(ões)" left and the host is not repeated when it is the name', () => {
    setLocale('pt-BR')
    expect(deleteConnectionsPrompt(t, [{ name: '', host: '10.1.1.1' }])).toBe('Excluir 10.1.1.1?')
    for (const n of [1, 2, 3, 12]) expect(deleteConnectionsPrompt(t, conns(n))).not.toMatch(/\(ões\)|\(s\)/)
  })

  it('en and zh-CN', () => {
    setLocale('en')
    expect(deleteConnectionsPrompt(t, [{ name: 'PE01-REI', host: '10.20.2.1' }])).toBe('Delete PE01-REI (10.20.2.1)?')
    expect(deleteConnectionsPrompt(t, conns(3))).toBe('Delete 3 connections: C1, C2 and C3?')
    expect(deleteConnectionsPrompt(t, conns(12))).toBe('Delete 12 connections: C1, C2, C3 and 9 more?')
    setLocale('zh-CN')
    expect(deleteConnectionsPrompt(t, conns(3))).toBe('删除 3 个连接：C1、C2 和 C3？')
  })

  it('a locale without the new keys falls back to en, never to the key', () => {
    setLocale('de')
    expect(deleteConnectionsPrompt(t, conns(3))).toBe('Delete 3 connections: C1, C2 and C3?')
    expect(nameList(t, ['A'])).toBe('A')
  })
})

describe('undo notice', () => {
  it('names the connection, the list or the group with its count', () => {
    setLocale('pt-BR')
    expect(deletedNotice(t, { id: 'x', kind: 'connections', names: ['PE01-REI'], host: '10.20.2.1' })).toBe('Excluída PE01-REI')
    expect(deletedNotice(t, { id: 'x', kind: 'connections', names: ['A', 'B', 'C'] })).toBe('Excluídas 3 conexões: A, B e C')
    expect(deletedNotice(t, { id: 'x', kind: 'group', names: [], groupName: 'POP REI' })).toBe('Grupo POP REI excluído')
    expect(deletedNotice(t, { id: 'x', kind: 'group', names: ['A'], groupName: 'POP REI' })).toBe('Grupo POP REI excluído com 1 conexão')
    expect(deletedNotice(t, { id: 'x', kind: 'group', names: ['A', 'B'], groupName: 'POP REI' })).toBe('Grupo POP REI excluído com 2 conexões')
  })
})
