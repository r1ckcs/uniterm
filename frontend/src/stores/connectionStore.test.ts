import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

let pushChanged: ((ev: { data: unknown }) => void) | null = null
vi.mock('@wailsio/runtime', async (importOriginal) => ({
  ...(await importOriginal()),
  Events: { On: vi.fn((_: string, cb: (ev: { data: unknown }) => void) => { pushChanged = cb; return () => {} }), Off: vi.fn() },
}))
vi.mock('../../bindings/github.com/ys-ll/uniterm/app', () => ({
  LoadConnections: vi.fn(),
  SaveConnections: vi.fn().mockResolvedValue(undefined),
}))

import { SaveConnections } from '../../bindings/github.com/ys-ll/uniterm/app'
import { DELETE_UNDO_MS, useConnectionStore } from './connectionStore'
import type { ConnectionConfig, ConnectionGroup } from '../types/session'

const conn = (id: string, groupId?: string) => ({ id, name: id.toUpperCase(), host: `h-${id}`, type: 'ssh', groupId }) as unknown as ConnectionConfig
type Saved = { connections: ConnectionConfig[]; groups: ConnectionGroup[] }
const saves = (): Saved[] => vi.mocked(SaveConnections).mock.calls.map((c: unknown[]) => c[0] as Saved)
const ids = (xs: { id: string }[]) => xs.map(x => x.id)

function setup() {
  const st = useConnectionStore()
  st.groups = [{ id: 'g1', name: 'POP' }, { id: 'g2', name: 'Sub', parentId: 'g1' }, { id: 'g3', name: 'Other' }]
  st.connections = [conn('a'), conn('b', 'g1'), conn('c'), conn('d', 'g2'), conn('e', 'g3')]
  return st
}

describe('deferred deletion', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
    vi.mocked(SaveConnections).mockClear()
  })
  afterEach(() => vi.useRealTimers())

  it('hides at once, writes nothing until the deadline, then removes once', async () => {
    const st = setup()
    st.scheduleRemoveMany(['a', 'c'])
    expect(ids(st.connections)).toEqual(['b', 'd', 'e'])
    expect(st.pendingDeletes).toHaveLength(1)
    expect(st.pendingDeletes[0].names).toEqual(['A', 'C'])
    await vi.advanceTimersByTimeAsync(DELETE_UNDO_MS - 1)
    expect(saves()).toHaveLength(0)
    await vi.advanceTimersByTimeAsync(1)
    expect(saves()).toHaveLength(1)
    expect(ids(saves()[0].connections)).toEqual(['b', 'd', 'e'])
    expect(st.pendingDeletes).toHaveLength(0)
    await vi.advanceTimersByTimeAsync(DELETE_UNDO_MS * 3)
    expect(saves()).toHaveLength(1)
  })

  it('undo puts everything back in the same place and never saves', async () => {
    const st = setup()
    const op = st.scheduleRemoveMany(['b', 'd'])!
    expect(st.undoDelete(op)).toBe(true)
    expect(ids(st.connections)).toEqual(['a', 'b', 'c', 'd', 'e'])
    expect(st.connections[1].groupId).toBe('g1')
    expect(st.pendingDeletes).toHaveLength(0)
    await vi.advanceTimersByTimeAsync(DELETE_UNDO_MS * 2)
    expect(saves()).toHaveLength(0)
    expect(st.undoDelete(op)).toBe(false)
  })

  it('another save inside the window keeps the pending items on disk (the vault never sees them go)', async () => {
    const st = setup()
    st.scheduleRemoveMany(['c'])
    await st.update('a', { name: 'renamed' })
    expect(ids(saves()[0].connections)).toEqual(['a', 'b', 'c', 'd', 'e'])
    expect(saves()[0].connections[0].name).toBe('renamed')
    expect(ids(st.connections)).toEqual(['a', 'b', 'd', 'e'])
  })

  it('closing the app with a pending deletion does not delete (nothing was written)', async () => {
    const st = setup()
    st.scheduleRemoveMany(['a'])
    // The window goes away before the timer: no save ever carried the removal.
    expect(saves()).toHaveLength(0)
    await st.save()
    expect(ids(saves()[0].connections)).toContain('a')
  })

  it('each deletion has its own deadline and undo', async () => {
    const st = setup()
    st.scheduleRemoveMany(['a'])
    await vi.advanceTimersByTimeAsync(3000)
    const second = st.scheduleRemoveMany(['c'])!
    await vi.advanceTimersByTimeAsync(DELETE_UNDO_MS - 3000)
    expect(saves()).toHaveLength(1)
    expect(ids(saves()[0].connections)).toEqual(['b', 'c', 'd', 'e'])
    st.undoDelete(second)
    expect(ids(st.connections)).toEqual(['b', 'c', 'd', 'e'])
    await vi.advanceTimersByTimeAsync(DELETE_UNDO_MS)
    expect(saves()).toHaveLength(1)
  })

  it('group with everything: hidden now, undo restores groups and connections', async () => {
    const st = setup()
    const op = st.scheduleDeleteGroup('g1', 'delete-connections', 'delete-all')!
    expect(ids(st.groups)).toEqual(['g3'])
    expect(ids(st.connections)).toEqual(['a', 'c', 'e'])
    expect(st.pendingDeletes[0]).toMatchObject({ kind: 'group', groupName: 'POP', names: ['B', 'D'] })
    await st.save()
    expect(ids(saves()[0].groups)).toEqual(['g1', 'g2', 'g3'])
    expect(ids(saves()[0].connections)).toEqual(['a', 'b', 'c', 'd', 'e'])
    st.undoDelete(op)
    expect(ids(st.groups)).toEqual(['g1', 'g2', 'g3'])
    expect(ids(st.connections)).toEqual(['a', 'b', 'c', 'd', 'e'])
  })

  it('group only: connections and sub-groups move up on screen, the disk keeps the old tree until the deadline', async () => {
    const st = setup()
    const op = st.scheduleDeleteGroup('g1', 'move-out')!
    expect(st.connections.find(c => c.id === 'b')!.groupId).toBeUndefined()
    expect(st.groups.find(g => g.id === 'g2')!.parentId).toBeUndefined()
    await st.save()
    expect(saves()[0].connections.find(c => c.id === 'b')!.groupId).toBe('g1')
    expect(saves()[0].groups.find(g => g.id === 'g2')!.parentId).toBe('g1')
    // The copy written to disk never touched what is on screen.
    expect(st.connections.find(c => c.id === 'b')!.groupId).toBeUndefined()
    st.undoDelete(op)
    expect(st.connections.find(c => c.id === 'b')!.groupId).toBe('g1')
    expect(st.groups.find(g => g.id === 'g2')!.parentId).toBe('g1')
    expect(ids(st.groups)).toEqual(['g1', 'g2', 'g3'])
  })

  it('group deadline commits once with the group gone', async () => {
    const st = setup()
    st.scheduleDeleteGroup('g3', 'delete-connections', 'delete-all')
    await vi.advanceTimersByTimeAsync(DELETE_UNDO_MS)
    expect(saves()).toHaveLength(1)
    expect(ids(saves()[0].groups)).toEqual(['g1', 'g2'])
    expect(ids(saves()[0].connections)).toEqual(['a', 'b', 'c', 'd'])
  })

  it('another window writing the disk does not bring a pending item back; undo returns its newest copy', () => {
    const st = setup()
    const op = st.scheduleRemoveMany(['c'])!
    pushChanged!({ data: { connections: [conn('a'), conn('b', 'g1'), { ...conn('c'), name: 'edited' }, conn('d', 'g2'), conn('e', 'g3')] } })
    expect(ids(st.connections)).toEqual(['a', 'b', 'd', 'e'])
    st.undoDelete(op)
    expect(st.connections.find(c => c.id === 'c')!.name).toBe('edited')
    expect(ids(st.connections)).toEqual(['a', 'b', 'c', 'd', 'e'])
  })

  it('nothing to delete schedules nothing', () => {
    const st = setup()
    expect(st.scheduleRemoveMany(['zz'])).toBeNull()
    expect(st.pendingDeletes).toHaveLength(0)
  })
})
