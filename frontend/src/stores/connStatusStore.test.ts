import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { createMockConnStatusApi, setConnStatusApi, useConnStatusStore } from './connStatusStore'

describe('connection status store', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('without the backend: no status, start screen shows plain counts', async () => {
    setConnStatusApi(null)
    const st = useConnStatusStore()
    await st.start()
    expect(st.available).toBe(false)
    expect(st.stateOf('a')).toBeUndefined()
  })

  it('probing is on while at least one start screen holds it', async () => {
    const api = createMockConnStatusApi(() => ['a', 'b', 'c', 'd'])
    const setActive = vi.spyOn(api, 'setActive')
    setConnStatusApi(api)
    const st = useConnStatusStore()
    await st.start()
    expect(st.available).toBe(true)
    expect(st.stateOf('d')).toBe('down')
    expect(st.downCount).toBe(1)
    st.acquire(); st.acquire(); st.release()
    expect(setActive.mock.calls).toEqual([[true]])
    st.release(); st.release()
    expect(setActive.mock.calls).toEqual([[true], [false]])
  })
})
