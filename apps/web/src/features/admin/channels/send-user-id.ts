/**
 * Whether a new channel starts with "send the user identifier" on: only for
 * OpenAI's and Anthropic's own endpoints, since a third-party endpoint may
 * reject a field it does not know. The same rule as the server's
 * (`defaultSendUserID` in internal/api/channels.go), which applies it when the
 * form leaves the switch alone; this copy only shows the switch in the state it
 * would get.
 */
export function defaultSendUserId(endpoint: string | undefined): boolean {
  if (!endpoint) return false
  try {
    const { hostname } = new URL(endpoint.trim())
    return hostname === 'api.openai.com' || hostname === 'api.anthropic.com'
  } catch {
    return false
  }
}
