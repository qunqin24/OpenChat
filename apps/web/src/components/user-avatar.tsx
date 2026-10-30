import { useState } from 'react'

import { avatarColor, avatarInitial } from '@openchat/chat-core/avatar'
import { cn } from '@/lib/utils'

/**
 * An account's picture: the first letter of its name, white on a colour of its
 * own — the way Google draws accounts without a photo.
 *
 * The colour comes from the account's id, so it is the same on every page and
 * every device, survives a change of name, and two people called Alex still
 * tell apart. The palette is saturated mid-tones that carry white text in both
 * themes, so the avatar needs nothing from either design system: the chat and
 * the admin panel draw the same one, sized and shaped by the caller.
 *
 * An account that has uploaded a picture (`src`, a signed URL of this server's
 * own) shows it in place of the letter. The letter is also what stays when the
 * picture cannot be had — an expired link in a cached session, a file that
 * went missing — so the avatar is never a broken image.
 *
 * Nothing third-party is fetched. Pictures used to come from Gravatar, which
 * told it who was being looked at from every page that showed a name.
 */
export function UserAvatar({
  id,
  name,
  email,
  src,
  className,
}: {
  /** Seeds the colour. */
  id?: string
  name?: string
  email?: string
  /** The uploaded picture's URL, if the account has one. */
  src?: string | null
  className?: string
}) {
  const letter = avatarInitial(name, email)
  // The address that failed, not a flag: a new one (the picture was replaced)
  // gets its own try.
  const [failed, setFailed] = useState<string>()
  const picture = src && src !== failed ? src : undefined
  return (
    <span
      aria-hidden="true"
      className={cn(
        'inline-flex shrink-0 select-none items-center justify-center overflow-hidden rounded-full font-medium text-white',
        className,
      )}
      style={{ backgroundColor: avatarColor(id || email || name || '') }}
    >
      {picture ? (
        <img
          src={picture}
          alt=""
          draggable={false}
          className="size-full object-cover"
          onError={() => setFailed(picture)}
        />
      ) : (
        letter
      )}
    </span>
  )
}

export default UserAvatar
