import type { PluginCallProgress, PluginCodeProgress } from '@openchat/api-client'
import type { TFunction } from 'i18next'

// Plugin tool calls in a reply (docs/design/plugins-plan.md §6): plugin_code,
// a script that calls the plugins' tools, or one tool of a plugin listed
// directly (`<plugin>__<tool>`). Both report the same progress - the calls so
// far, and any waiting for the person to allow it - while they run, and leave
// it in their result's details.

export const PLUGIN_CODE = 'plugin_code'

/** A call that goes to a plugin: a script, or one of a directly listed plugin's tools. */
export function isPluginTool(name: string): boolean {
  return name === PLUGIN_CODE || name.includes('__')
}

function isProgress(value: unknown): value is PluginCodeProgress {
  return typeof value === 'object' && value !== null && Array.isArray((value as PluginCodeProgress).calls)
}

/** What a plugin step reports: its result's details once done, else its live progress. */
export function pluginProgressOf(details: unknown, progress: unknown): PluginCodeProgress | null {
  if (isProgress(details)) return details
  if (isProgress(progress)) return progress
  return null
}

/** The calls waiting for the person, in the order the script made them. */
export function waitingCalls(progress: PluginCodeProgress | null): PluginCallProgress[] {
  return (progress?.calls ?? []).filter((call) => call.status === 'waiting' && call.confirmationId)
}

/** A plugin's name from its short one, as the progress lists it. */
export function pluginName(progress: PluginCodeProgress | null, slug: string): string {
  return progress?.plugins.find((p) => p.slug === slug)?.name ?? slug
}

type T = TFunction<'common'>

/**
 * The step's line: each plugin with the tools it was asked for -
 * "GitHub · get_me、search_repositories" - or, for a script that only looked
 * tools up, what it looked in; while it runs, the call in flight.
 */
export function pluginStepLabel(t: T, name: string, progress: PluginCodeProgress | null, running: boolean, failed: boolean): string {
  const calls = progress?.calls ?? []
  if (running) {
    const current = [...calls].reverse().find((call) => call.status === 'running' || call.status === 'waiting')
    if (current?.status === 'waiting') return t('plugins.step.waiting', { plugin: pluginName(progress, current.plugin), tool: current.tool })
    if (current) return t('plugins.step.calling', { plugin: pluginName(progress, current.plugin), tool: current.tool })
    return name === PLUGIN_CODE ? t('plugins.step.preparing') : t('plugins.step.calling', { plugin: name.split('__')[0], tool: name.split('__')[1] ?? '' })
  }
  if (calls.length > 0) {
    const tools = new Map<string, string[]>()
    for (const call of calls) {
      const list = tools.get(call.plugin) ?? []
      if (!list.includes(call.tool)) list.push(call.tool)
      tools.set(call.plugin, list)
    }
    return [...tools].map(([slug, list]) => `${pluginName(progress, slug)} · ${list.join(t('plugins.step.joiner'))}`).join(t('plugins.step.separator'))
  }
  if (failed) return t('plugins.step.failed-generic')
  const searched = progress?.searches?.find((s) => s.plugin)?.plugin
  if (searched) return t('plugins.step.searched', { plugin: pluginName(progress, searched) })
  if (progress?.searches?.length) return t('plugins.step.searched-any')
  return t('plugins.step.no-calls')
}
