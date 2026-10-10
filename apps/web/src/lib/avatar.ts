import { API_BASE_URL } from '@/lib/api'

/** The formats a picture may be chosen in; the server takes these and no others. */
export const AVATAR_TYPES = ['image/png', 'image/jpeg', 'image/webp']
/** The largest file taken from the disk. It is scaled down before it is sent, so this is not the upload's limit. */
export const AVATAR_PICK_LIMIT = 10 << 20
/** The side of the picture that is sent, in pixels. */
const SIDE = 256

/** Why a picture was not set, as the `settings.account.avatar-*` key that says so. */
export type AvatarError = 'type' | 'too-large' | 'failed'

export class AvatarFailed extends Error {
  constructor(readonly reason: AvatarError) {
    super(reason)
  }
}

/**
 * The middle square of an image, scaled to at most 256 px, as a PNG.
 *
 * Done here as well as on the server (which cuts and scales whatever it gets):
 * the browser applies a photo's rotation, which the server's decoder does not
 * know about, and a phone photo of several megabytes is a few tens of kilobytes
 * by the time it leaves.
 */
export async function squareAvatar(file: File): Promise<Blob> {
  if (!AVATAR_TYPES.includes(file.type)) throw new AvatarFailed('type')
  if (file.size > AVATAR_PICK_LIMIT) throw new AvatarFailed('too-large')

  let bitmap: ImageBitmap
  try {
    bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' })
  } catch {
    throw new AvatarFailed('failed')
  }
  try {
    const side = Math.min(bitmap.width, bitmap.height)
    const out = Math.min(side, SIDE)
    const canvas = document.createElement('canvas')
    canvas.width = canvas.height = out
    const context = canvas.getContext('2d')
    if (!context) throw new AvatarFailed('failed')
    context.imageSmoothingQuality = 'high'
    context.drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, out, out)
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/png'))
    if (!blob) throw new AvatarFailed('failed')
    return blob
  } finally {
    bitmap.close()
  }
}

/** PUTs the picture. The image is the body, which the typed client has no way to say, so this is `fetch`. */
export async function putAvatar(picture: Blob): Promise<unknown> {
  const response = await fetch(`${API_BASE_URL}/me/avatar`, {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': picture.type },
    body: picture,
  })
  if (response.status === 413) throw new AvatarFailed('too-large')
  if (response.status === 415) throw new AvatarFailed('type')
  if (!response.ok) throw new AvatarFailed('failed')
  return response.json()
}
