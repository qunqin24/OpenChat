import type { components } from '@openchat/api-client'
import { useQuery } from '@tanstack/react-query'

import { api, unwrap } from '@/lib/api'
import { adminKeys } from '@/features/admin/use-admin'

export type ChannelTypeInfo = components['schemas']['ChannelTypeInfo']
export type ChannelType = components['schemas']['ChannelType']

/**
 * What a channel can be: a vendor, not a protocol.
 *
 * The two used to be separate fields and the operator had to answer both — one
 * of which ("which protocol does this speak") they should never have been
 * asked. A type answers it: most of these speak the OpenAI wire protocol and
 * differ only in their endpoint and their branding, which is precisely the part
 * an operator does know.
 *
 * Fetched rather than written out here. The same table fills in the default
 * endpoint this form shows and tells the server which protocol to speak, and
 * two copies of it would drift — with the failure landing as a channel talking
 * the wrong protocol to a real endpoint.
 */
export function useChannelTypes() {
  return useQuery({
    queryKey: [...adminKeys.all, 'channel-types'] as const,
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/channel-types', { signal })),
    // A build-time table behind an endpoint: it cannot change without a deploy.
    staleTime: Infinity,
  })
}

/** What a new channel starts as — the one most operators are adding. */
export const DEFAULT_CHANNEL_TYPE: ChannelType = 'openai'
