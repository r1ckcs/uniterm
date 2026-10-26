import { describe, expect, it } from 'vitest'
import { answerBroadcastPaste, broadcastPasteQueue, confirmBroadcastPaste } from './useBroadcastPasteConfirm'

const req = { lineCount: 2, sessions: [{ name: 'A', host: '' }, { name: 'B', host: '' }], preview: ['a', 'b'], more: 0 }

describe('broadcast paste queue', () => {
  it('one dialog at a time, answered in order; a stray answer does nothing', async () => {
    const first = confirmBroadcastPaste(req)
    const second = confirmBroadcastPaste({ ...req, lineCount: 3 })
    expect(broadcastPasteQueue.value.map((r) => r.lineCount)).toEqual([2, 3])
    answerBroadcastPaste(false)
    answerBroadcastPaste(true)
    answerBroadcastPaste(true)
    await expect(first).resolves.toBe(false)
    await expect(second).resolves.toBe(true)
    expect(broadcastPasteQueue.value).toHaveLength(0)
  })
})
