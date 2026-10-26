// One confirm-before-delete dialog for the whole window (item 42). Unlike
// ElMessageBox.confirm, which focuses the confirm button, the dialog opens on
// Cancel: Enter and Esc never delete, only a click (or Tab) on the red button.
import { ref } from 'vue'

export interface DeleteConfirmRequest {
  title: string
  message: string
  confirmText: string
  resolve: (ok: boolean) => void
}

export const deleteConfirmQueue = ref<DeleteConfirmRequest[]>([])

export function confirmDelete(opts: { title: string; message: string; confirmText: string }): Promise<boolean> {
  return new Promise((resolve) => {
    deleteConfirmQueue.value = [...deleteConfirmQueue.value, { ...opts, resolve }]
  })
}

export function answerDeleteConfirm(ok: boolean) {
  const [head, ...rest] = deleteConfirmQueue.value
  if (!head) return
  deleteConfirmQueue.value = rest
  head.resolve(ok)
}
