import { create } from 'zustand'

export interface Toast {
  id: number
  text: string
  /** A second, quieter line: the error id to quote, a warning's detail. */
  detail?: string
  tone: 'error' | 'info'
}

interface ToastState {
  toasts: Toast[]
  dismiss: (id: number) => void
}

let next = 0

/** What the web app says with sonner's toasts, said at the foot of the screen. */
export const useToasts = create<ToastState>()((set) => ({
  toasts: [],
  dismiss: (id) => set((state) => ({ toasts: state.toasts.filter((toast) => toast.id !== id) })),
}))

function show(tone: Toast['tone'], text: string, detail?: string) {
  const id = ++next
  // One at a time: a second failure replaces the first rather than stacking.
  useToasts.setState({ toasts: [{ id, text, detail, tone }] })
  setTimeout(() => useToasts.getState().dismiss(id), 4000)
}

export const toast = {
  error: (text: string, detail?: string) => show('error', text, detail),
  info: (text: string, detail?: string) => show('info', text, detail),
}
