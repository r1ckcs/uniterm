import { describe, expect, it } from 'vitest'
import { answerDeleteConfirm, confirmDelete, deleteConfirmQueue } from './useDeleteConfirm'

describe('delete confirm queue', () => {
  it('answers one request at a time, in order; a stray answer does nothing', async () => {
    const first = confirmDelete({ title: 't', message: 'one', confirmText: 'Delete' })
    const second = confirmDelete({ title: 't', message: 'two', confirmText: 'Delete' })
    expect(deleteConfirmQueue.value.map(r => r.message)).toEqual(['one', 'two'])
    answerDeleteConfirm(false)
    answerDeleteConfirm(true)
    answerDeleteConfirm(true)
    await expect(first).resolves.toBe(false)
    await expect(second).resolves.toBe(true)
    expect(deleteConfirmQueue.value).toHaveLength(0)
  })
})
