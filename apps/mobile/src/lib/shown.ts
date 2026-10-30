import { create } from 'zustand'

/** The conversation the chat screen shows, for the drawer to mark (it is rendered outside the screen). */
export const useShownConversation = create<{ id?: string; set: (id?: string) => void }>()((set) => ({
  id: undefined,
  set: (id) => set({ id }),
}))
