import type { SkillFile } from '@openchat/api-client'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { adminKeys } from '@/features/admin/use-admin'
import { API_BASE_URL, api, unwrap } from '@/lib/api'
import { queryKeys } from '@openchat/chat-core/query-keys'

/**
 * Skills (docs/design/skills-plan.md): a person's own under /me/skills, shown
 * in the settings dialog, and the site's under /admin/skills, shown in the
 * admin console. The calls are the same over the two; `scope` picks which.
 * Nothing here is persisted to the browser (lib/query-persist.ts keeps only an
 * allow-list): a skill is the person's own words.
 */
export type SkillScope = 'me' | 'site'

function keyOf(scope: SkillScope) {
  return scope === 'me' ? queryKeys.skills.all : adminKeys.skills()
}

export function useMySkills() {
  return useQuery({
    queryKey: queryKeys.skills.list(),
    queryFn: async ({ signal }) => unwrap(await api.GET('/me/skills', { signal })),
  })
}

export function useSiteSkills() {
  return useQuery({
    queryKey: [...adminKeys.skills(), 'list'],
    queryFn: async ({ signal }) => unwrap(await api.GET('/admin/skills', { signal })),
  })
}

/**
 * What the composer's $ picker offers: the skills this conversation's model
 * can open (its snapshot), or what a new conversation would list.
 */
export function useAvailableSkills(conversationId: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: [...queryKeys.skills.all, 'available', conversationId ?? ''],
    enabled,
    queryFn: async ({ signal }) =>
      unwrap(
        await api.GET('/me/skills/available', {
          params: { query: conversationId ? { conversationId } : {} },
          signal,
        }),
      ).items,
  })
}

/** One skill with its files, for the editor; nothing is fetched while id is null. */
export function useSkill(scope: SkillScope, id: string | null) {
  return useQuery({
    queryKey: [...keyOf(scope), 'detail', id ?? ''],
    enabled: id !== null,
    queryFn: async ({ signal }) => {
      const params = { path: { skillId: id ?? '' } }
      return scope === 'me'
        ? unwrap(await api.GET('/me/skills/{skillId}', { params, signal }))
        : unwrap(await api.GET('/admin/skills/{skillId}', { params, signal }))
    },
  })
}

/** A skill's kept versions, newest first; nothing is fetched while id is null. */
export function useSkillVersions(scope: SkillScope, id: string | null) {
  return useQuery({
    queryKey: [...keyOf(scope), 'versions', id ?? ''],
    enabled: id !== null,
    queryFn: async ({ signal }) => {
      const params = { path: { skillId: id ?? '' } }
      return (
        scope === 'me'
          ? unwrap(await api.GET('/me/skills/{skillId}/versions', { params, signal }))
          : unwrap(await api.GET('/admin/skills/{skillId}/versions', { params, signal }))
      ).items
    },
  })
}

/** One kept version's files, fetched when asked for (to load into the editor). */
export async function fetchSkillVersion(scope: SkillScope, id: string, versionId: string) {
  const params = { path: { skillId: id, versionId } }
  return scope === 'me'
    ? unwrap(await api.GET('/me/skills/{skillId}/versions/{versionId}', { params }))
    : unwrap(await api.GET('/admin/skills/{skillId}/versions/{versionId}', { params }))
}

/** Where a skill downloads as a ZIP (a plain link: the session cookie goes with it). */
export function skillExportUrl(scope: SkillScope, id: string) {
  return `${API_BASE_URL}${scope === 'me' ? '/me' : '/admin'}/skills/${encodeURIComponent(id)}/export`
}

function useRefresh(scope: SkillScope) {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: keyOf(scope) })
}

/** Creates a skill (id null) or saves a new version of one. */
export function useSaveSkill(scope: SkillScope) {
  const refresh = useRefresh(scope)
  return useMutation({
    // The editor says what went wrong under the files.
    meta: { silent: true },
    mutationFn: async ({ id, files }: { id: string | null; files: SkillFile[] }) => {
      const body = { files }
      if (id === null) {
        return scope === 'me'
          ? unwrap(await api.POST('/me/skills', { body }))
          : unwrap(await api.POST('/admin/skills', { body }))
      }
      const params = { path: { skillId: id } }
      return scope === 'me'
        ? unwrap(await api.PUT('/me/skills/{skillId}', { params, body }))
        : unwrap(await api.PUT('/admin/skills/{skillId}', { params, body }))
    },
    onSuccess: refresh,
  })
}

export function useSetSkillEnabled(scope: SkillScope) {
  const refresh = useRefresh(scope)
  return useMutation({
    meta: { silent: true },
    mutationFn: async ({ id, enabled }: { id: string; enabled: boolean }) => {
      const params = { path: { skillId: id } }
      const body = { enabled }
      return scope === 'me'
        ? unwrap(await api.PATCH('/me/skills/{skillId}', { params, body }))
        : unwrap(await api.PATCH('/admin/skills/{skillId}', { params, body }))
    },
    onSettled: refresh,
  })
}

export function useDeleteSkill(scope: SkillScope) {
  const refresh = useRefresh(scope)
  return useMutation({
    meta: { silent: true },
    mutationFn: async (id: string) => {
      const params = { path: { skillId: id } }
      return scope === 'me'
        ? unwrap(await api.DELETE('/me/skills/{skillId}', { params }))
        : unwrap(await api.DELETE('/admin/skills/{skillId}', { params }))
    },
    onSuccess: refresh,
  })
}

/** Imports the skills in a SKILL.md, a .zip or a .skill file. */
export function useImportSkills(scope: SkillScope) {
  const refresh = useRefresh(scope)
  return useMutation({
    meta: { silent: true },
    mutationFn: async (file: File) => {
      // The file is the body, as it is; the name says how to read it.
      const init = {
        params: { query: { filename: file.name } },
        body: file as unknown as string,
        bodySerializer: (body: unknown) => body as BodyInit,
        headers: { 'Content-Type': 'application/octet-stream' },
      }
      return scope === 'me'
        ? unwrap(await api.POST('/me/skills/import', init))
        : unwrap(await api.POST('/admin/skills/import', init))
    },
    onSuccess: refresh,
  })
}

/** What a new skill starts as in the editor: the frontmatter it needs, and a body to replace. */
export function skillTemplate(name: string, description: string, body: string): SkillFile[] {
  return [{ path: 'SKILL.md', content: `---\nname: ${name}\ndescription: ${description}\n---\n\n${body}\n` }]
}

/** Switches a built-in skill on or off (admin). */
export function useSetBuiltinSkillEnabled() {
  const refresh = useRefresh('site')
  return useMutation({
    meta: { silent: true },
    mutationFn: async ({ name, enabled }: { name: string; enabled: boolean }) =>
      unwrap(await api.PATCH('/admin/skills/builtin/{name}', { params: { path: { name } }, body: { enabled } })),
    onSettled: refresh,
  })
}

/** A built-in skill with its files, to look at (admin); nothing is fetched while name is null. */
export function useBuiltinSkill(name: string | null) {
  return useQuery({
    queryKey: [...adminKeys.skills(), 'builtin', name ?? ''],
    enabled: name !== null,
    queryFn: async ({ signal }) =>
      unwrap(await api.GET('/admin/skills/builtin/{name}', { params: { path: { name: name ?? '' } }, signal })),
  })
}
