import { create } from 'zustand'

/**
 * Whether the network address this guest comes from is banned from guest mode
 * (content moderation). Told by any guest request the server answers with 403
 * `guest_banned` (lib/guest/index.ts) and by `GET /guest/status`; the shell
 * then covers the page (GuestBannedScreen). Kept in memory only: a reload asks
 * the server again, which is also how a ban that has ended is found out.
 */
interface GuestBanState {
  banned: boolean
  /** When the ban ends (ISO time); null for a permanent one. */
  until: string | null
  mark: (until: string | null) => void
}

export const useGuestBan = create<GuestBanState>()((set) => ({
  banned: false,
  until: null,
  mark: (until) => set({ banned: true, until }),
}))

export const markGuestBanned = (until: string | null) => useGuestBan.getState().mark(until)
