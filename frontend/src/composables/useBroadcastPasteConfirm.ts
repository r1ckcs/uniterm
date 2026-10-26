// One broadcast-paste confirmation per window (item 48), same pattern as
// useDeleteConfirm: a queue rendered by BroadcastPasteDialog, Cancel focused.
import { ref } from 'vue'
import type { BroadcastPasteRequest } from '../utils/terminalPaste'

interface Pending extends BroadcastPasteRequest {
  resolve: (ok: boolean) => void
}

export const broadcastPasteQueue = ref<Pending[]>([])

export function confirmBroadcastPaste(req: BroadcastPasteRequest): Promise<boolean> {
  return new Promise((resolve) => {
    broadcastPasteQueue.value = [...broadcastPasteQueue.value, { ...req, resolve }]
  })
}

export function answerBroadcastPaste(ok: boolean) {
  const [head, ...rest] = broadcastPasteQueue.value
  if (!head) return
  broadcastPasteQueue.value = rest
  head.resolve(ok)
}
