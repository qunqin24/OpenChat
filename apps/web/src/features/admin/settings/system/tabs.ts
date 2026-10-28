/** The System page's tabs, in order. The one shown is the `tab` search
 *  parameter, so a reload or a shared link keeps it. */
export const SYSTEM_TABS = ['general', 'chat', 'subagents', 'access', 'moderation', 'data'] as const
export type SystemTab = (typeof SYSTEM_TABS)[number]
