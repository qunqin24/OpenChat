import type { Skill, SkillFile } from '@openchat/api-client'
import { Download, Eye, FilePlus2, Pencil, Plus, Trash2, Upload } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfigDrawer } from '@/components/shadcn/config-drawer'
import { Header } from '@/components/shadcn/layout/header'
import { Main } from '@/components/shadcn/layout/main'
import { Search } from '@/components/shadcn/search'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/shadcn/ui/alert-dialog'
import { Button } from '@/components/shadcn/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/shadcn/ui/dialog'
import { Input } from '@/components/shadcn/ui/input'
import { Separator } from '@/components/shadcn/ui/separator'
import { Skeleton } from '@/components/shadcn/ui/skeleton'
import { Switch } from '@/components/shadcn/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/shadcn/ui/table'
import { Textarea } from '@/components/shadcn/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/shadcn/ui/select'
import {
  fetchSkillVersion,
  skillExportUrl,
  skillTemplate,
  useBuiltinSkill,
  useDeleteSkill,
  useImportSkills,
  useSaveSkill,
  useSetBuiltinSkillEnabled,
  useSetSkillEnabled,
  usePluginSkills,
  useSiteSkills,
  useSkill,
  useSkillVersions,
  type SkillScope,
} from '@/hooks/use-skills'
import { useSkillErrorText } from '@/hooks/use-skill-errors'
import { cn } from '@/lib/utils'

/**
 * The site's skills (docs/design/skills-plan.md): instructions every account's
 * conversations may use, written here and not checked by content moderation -
 * they are the administrator's, like the rest of the site settings. Nobody's
 * personal skills are listed: the endpoints do not reach them. Whether skills
 * are on at all, and whether people may write their own, are switches in
 * System settings.
 */
export function SiteSkills() {
  const { t, i18n } = useTranslation()
  const skills = useSiteSkills()
  const toggle = useSetSkillEnabled('site')
  const remove = useDeleteSkill('site')
  const importer = useImportSkills('site')
  const errorText = useSkillErrorText()
  const picker = useRef<HTMLInputElement>(null)
  const [editing, setEditing] = useState<string | null | undefined>(undefined)
  const [deleting, setDeleting] = useState<Skill | null>(null)
  const [viewing, setViewing] = useState<string | null>(null)
  const toggleBuiltin = useSetBuiltinSkillEnabled()

  const items = skills.data?.items ?? []
  const full = skills.data ? items.length >= skills.data.limit : false
  const day = (iso: string) => new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(new Date(iso))

  const importFile = (file: File | undefined) => {
    if (!file) return
    importer.mutate(file, {
      onSuccess: (result) => {
        if (result.created.length > 0) toast.success(t('admin.skills.imported', { count: result.created.length }))
        for (const failed of result.failed) toast.error(`${failed.name}: ${failed.message}`)
        if (result.skipped.length > 0) toast.message(t('admin.skills.import-skipped', { count: result.skipped.length }))
      },
      onError: (failure) => toast.error(errorText(failure)),
    })
  }

  return (
    <>
      <Header fixed>
        <Search className="me-auto" />
        <ConfigDrawer />
      </Header>

      <Main className="flex flex-1 flex-col">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h2 className="text-2xl font-bold tracking-tight">{t('admin.skills.title')}</h2>
            <p className="text-muted-foreground">{t('admin.skills.subtitle')}</p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" disabled={full || importer.isPending} onClick={() => picker.current?.click()}>
              <Upload />
              {t('admin.skills.import')}
            </Button>
            <Button disabled={full} onClick={() => setEditing(null)}>
              <Plus />
              {t('admin.skills.new')}
            </Button>
            <input
              ref={picker}
              type="file"
              hidden
              accept=".md,.zip,.skill"
              onChange={(e) => {
                importFile(e.target.files?.[0])
                e.target.value = ''
              }}
            />
          </div>
        </div>
        <Separator className="my-4 lg:my-6" />

        {skills.isPending ? (
          <Skeleton className="h-48 w-full" />
        ) : skills.isError ? (
          <p className="text-muted-foreground text-sm">{t('admin.skills.load-failed')}</p>
        ) : items.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('admin.skills.empty')}</p>
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('admin.skills.name')}</TableHead>
                  <TableHead>{t('admin.skills.description')}</TableHead>
                  <TableHead className="w-32">{t('admin.skills.updated')}</TableHead>
                  <TableHead className="w-20">{t('admin.skills.enabled')}</TableHead>
                  <TableHead className="w-32" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((skill) => (
                  <TableRow key={skill.id}>
                    <TableCell className="font-mono text-xs font-medium">{skill.name}</TableCell>
                    <TableCell className="text-muted-foreground max-w-md text-sm whitespace-normal">
                      <span className="line-clamp-2">{skill.description}</span>
                    </TableCell>
                    <TableCell className="text-muted-foreground text-sm">{day(skill.updatedAt)}</TableCell>
                    <TableCell>
                      <Switch
                        checked={skill.enabled}
                        disabled={toggle.isPending}
                        aria-label={t('admin.skills.enabled')}
                        onCheckedChange={(enabled) =>
                          toggle.mutate({ id: skill.id, enabled }, { onError: (failure) => toast.error(errorText(failure)) })
                        }
                      />
                    </TableCell>
                    <TableCell className="text-end">
                      <Button variant="ghost" size="icon" aria-label={t('admin.skills.edit')} onClick={() => setEditing(skill.id)}>
                        <Pencil />
                      </Button>
                      <Button variant="ghost" size="icon" aria-label={t('admin.skills.export')} asChild>
                        <a href={skillExportUrl('site', skill.id)} download>
                          <Download />
                        </a>
                      </Button>
                      <Button variant="ghost" size="icon" aria-label={t('admin.skills.delete')} onClick={() => setDeleting(skill)}>
                        <Trash2 />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}

        {(skills.data?.builtin.length ?? 0) > 0 && (
          <section className="mt-10 space-y-4">
            <div>
              <h3 className="text-lg font-medium">{t('admin.skills.builtin-title')}</h3>
              <p className="text-muted-foreground text-sm">{t('admin.skills.builtin-description')}</p>
            </div>
            <div className="rounded-md border">
              <Table>
                <TableBody>
                  {skills.data?.builtin.map((skill) => (
                    <TableRow key={skill.name}>
                      <TableCell className="w-48 font-mono text-xs font-medium">{skill.name}</TableCell>
                      <TableCell className="text-muted-foreground text-sm whitespace-normal">
                        <span className="line-clamp-2">{skill.description}</span>
                      </TableCell>
                      <TableCell className="w-20">
                        <Switch
                          checked={skill.enabled}
                          disabled={toggleBuiltin.isPending}
                          aria-label={t('admin.skills.enabled')}
                          onCheckedChange={(enabled) =>
                            toggleBuiltin.mutate({ name: skill.name, enabled }, { onError: (failure) => toast.error(errorText(failure)) })
                          }
                        />
                      </TableCell>
                      <TableCell className="w-16 text-end">
                        <Button variant="ghost" size="icon" aria-label={t('admin.skills.view')} onClick={() => setViewing(skill.name)}>
                          <Eye />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </section>
        )}
      </Main>

      <Dialog open={viewing !== null} onOpenChange={(open) => !open && setViewing(null)}>
        <DialogContent className="sm:max-w-3xl">{viewing !== null && <BuiltinView name={viewing} />}</DialogContent>
      </Dialog>

      <Dialog open={editing !== undefined} onOpenChange={(open) => !open && setEditing(undefined)}>
        <DialogContent className="sm:max-w-4xl">
          {editing !== undefined && <EditorLoader scope="site" id={editing} onClose={() => setEditing(undefined)} />}
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('admin.skills.delete-title', { name: deleting?.name ?? '' })}</AlertDialogTitle>
            <AlertDialogDescription>{t('admin.skills.delete-description')}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('admin.skills.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                deleting &&
                remove.mutate(deleting.id, {
                  onSuccess: () => toast.success(t('admin.skills.deleted')),
                  onError: (failure) => toast.error(errorText(failure)),
                })
              }
            >
              {t('admin.skills.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function EditorLoader({ scope, id, onClose }: { scope: SkillScope; id: string | null; onClose: () => void }) {
  const { t } = useTranslation()
  const skill = useSkill(scope, id)
  if (id === null) {
    return (
      <Editor
        scope={scope}
        id={null}
        initial={skillTemplate(typeof scope === 'object' ? 'plugin-skill' : 'site-skill', t('admin.skills.template-description'), t('admin.skills.template-body'))}
        onClose={onClose}
      />
    )
  }
  if (skill.isPending) return <Skeleton className="h-96 w-full" />
  if (skill.isError || !skill.data) return <p className="text-muted-foreground text-sm">{t('admin.skills.load-failed')}</p>
  return <Editor scope={scope} id={id} initial={skill.data.files} onClose={onClose} />
}

/**
 * The files down the left, SKILL.md first and not removable; the chosen one's
 * text on the right. Name and description are SKILL.md's frontmatter.
 */
function Editor({ scope, id, initial, onClose }: { scope: SkillScope; id: string | null; initial: SkillFile[]; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const save = useSaveSkill(scope)
  const errorText = useSkillErrorText()
  const [files, setFiles] = useState<SkillFile[]>(() => sortFiles(initial))
  const [current, setCurrent] = useState('SKILL.md')
  const [path, setPath] = useState('')
  const [error, setError] = useState<string>()

  // A plugin's skills keep no versions to go back to.
  const versions = useSkillVersions(scope, id)
  const loadVersion = async (versionId: string) => {
    if (!id || typeof scope === 'object') return
    try {
      setFiles(sortFiles((await fetchSkillVersion(scope, id, versionId)).files))
      setCurrent('SKILL.md')
      setError(undefined)
    } catch (failure) {
      setError(errorText(failure))
    }
  }

  const file = files.find((f) => f.path === current) ?? files[0]

  const addFile = () => {
    const clean = path.trim().replace(/^\.?\//, '')
    if (!clean) return
    if (!files.some((f) => f.path === clean)) setFiles((list) => sortFiles([...list, { path: clean, content: '' }]))
    setCurrent(clean)
    setPath('')
  }

  const submit = async () => {
    try {
      await save.mutateAsync({ id, files })
      toast.success(t('admin.skills.saved'))
      onClose()
    } catch (failure) {
      setError(errorText(failure))
    }
  }

  return (
    <>
      <DialogHeader className="flex-row items-center justify-between gap-4 pe-8">
        <DialogTitle>{t(id === null ? 'admin.skills.new' : 'admin.skills.edit')}</DialogTitle>
        {versions.data && versions.data.length > 1 && (
          <Select value="" onValueChange={(v) => void loadVersion(v)}>
            <SelectTrigger className="h-8 w-56 text-xs" aria-label={t('admin.skills.versions')}>
              <SelectValue placeholder={t('admin.skills.versions')} />
            </SelectTrigger>
            <SelectContent>
              {versions.data.map((v) => (
                <SelectItem key={v.id} value={v.id} className="text-xs">
                  {new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(v.createdAt))}
                  {v.current ? ` · ${t('admin.skills.current')}` : ''}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </DialogHeader>
      <div className="flex flex-col gap-4 sm:flex-row">
        <div className="flex w-full flex-none flex-col gap-2 sm:w-56">
          <ul className="flex max-h-96 flex-col gap-1 overflow-y-auto">
            {files.map((f) => (
              <li key={f.path} className={cn('flex items-center rounded-md', f.path === file?.path && 'bg-muted')}>
                <button
                  type="button"
                  className="min-w-0 flex-1 truncate px-2 py-1.5 text-start font-mono text-xs"
                  onClick={() => setCurrent(f.path)}
                >
                  {f.path}
                </button>
                {f.path !== 'SKILL.md' && (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-7"
                    aria-label={t('admin.skills.remove-file', { path: f.path })}
                    onClick={() => {
                      setFiles((list) => list.filter((x) => x.path !== f.path))
                      if (current === f.path) setCurrent('SKILL.md')
                    }}
                  >
                    <Trash2 />
                  </Button>
                )}
              </li>
            ))}
          </ul>
          <div className="flex gap-1">
            <Input
              value={path}
              placeholder="references/guide.md"
              className="h-8 font-mono text-xs"
              aria-label={t('admin.skills.file-path')}
              onChange={(e) => setPath(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
                  e.preventDefault()
                  addFile()
                }
              }}
            />
            <Button variant="outline" size="icon" className="size-8" aria-label={t('admin.skills.add-file')} onClick={addFile}>
              <FilePlus2 />
            </Button>
          </div>
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <Textarea
            value={file?.content ?? ''}
            spellCheck={false}
            aria-label={file?.path}
            className="min-h-96 font-mono text-xs"
            onChange={(e) => {
              setError(undefined)
              const content = e.target.value
              setFiles((list) => list.map((f) => (f.path === file?.path ? { ...f, content } : f)))
            }}
          />
          {error ? (
            <p className="text-destructive text-sm" role="alert">
              {error}
            </p>
          ) : (
            <p className="text-muted-foreground text-xs">{t('admin.skills.editor-hint')}</p>
          )}
        </div>
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t('admin.skills.cancel')}
        </Button>
        <Button disabled={save.isPending} onClick={() => void submit()}>
          {t('admin.skills.save')}
        </Button>
      </DialogFooter>
    </>
  )
}

/** A built-in skill's files, read only: it ships with the server. */
function BuiltinView({ name }: { name: string }) {
  const { t } = useTranslation()
  const skill = useBuiltinSkill(name)
  if (skill.isPending) return <Skeleton className="h-96 w-full" />
  if (skill.isError || !skill.data) return <p className="text-muted-foreground text-sm">{t('admin.skills.load-failed')}</p>
  return (
    <>
      <DialogHeader>
        <DialogTitle className="font-mono">{skill.data.skill.name}</DialogTitle>
      </DialogHeader>
      <div className="max-h-[70vh] space-y-4 overflow-y-auto">
        {skill.data.files.map((f) => (
          <div key={f.path} className="space-y-1">
            <div className="text-muted-foreground font-mono text-xs">{f.path}</div>
            <pre className="bg-muted overflow-x-auto rounded-md p-3 text-xs whitespace-pre-wrap">{f.content}</pre>
          </div>
        ))}
      </div>
    </>
  )
}

function sortFiles(files: SkillFile[]): SkillFile[] {
  return [...files].sort((a, b) =>
    a.path === 'SKILL.md' ? -1 : b.path === 'SKILL.md' ? 1 : a.path.localeCompare(b.path),
  )
}

export default SiteSkills

/**
 * A plugin's own skills (docs/design/plugins-plan.md §2), in its editor's
 * Skills tab: listed in the conversations of the people who installed the
 * plugin. Written and switched as the site's are; no versions to go back to,
 * no import or export.
 */
export function PluginSkills({ pluginId }: { pluginId: string }) {
  const { t, i18n } = useTranslation()
  const scope = { plugin: pluginId }
  const skills = usePluginSkills(pluginId)
  const toggle = useSetSkillEnabled(scope)
  const remove = useDeleteSkill(scope)
  const errorText = useSkillErrorText()
  const [editing, setEditing] = useState<string | null | undefined>(undefined)
  const [deleting, setDeleting] = useState<Skill | null>(null)
  const items = skills.data?.items ?? []
  const full = skills.data ? items.length >= skills.data.limit : false
  const day = (iso: string) => new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(new Date(iso))

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <p className="text-muted-foreground text-sm">{t('admin.plugins.skills-hint')}</p>
        <Button size="sm" disabled={full} onClick={() => setEditing(null)}>
          <Plus />
          {t('admin.skills.new')}
        </Button>
      </div>
      {skills.isPending ? (
        <Skeleton className="h-24 w-full" />
      ) : skills.isError ? (
        <p className="text-muted-foreground text-sm">{t('admin.skills.load-failed')}</p>
      ) : items.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('admin.plugins.skills-empty')}</p>
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableBody>
              {items.map((skill) => (
                <TableRow key={skill.id}>
                  <TableCell className="font-mono text-xs font-medium">{skill.name}</TableCell>
                  <TableCell className="text-muted-foreground max-w-56 text-sm whitespace-normal">
                    <span className="line-clamp-2">{skill.description}</span>
                  </TableCell>
                  <TableCell className="text-muted-foreground w-28 text-sm">{day(skill.updatedAt)}</TableCell>
                  <TableCell className="w-14">
                    <Switch
                      checked={skill.enabled}
                      disabled={toggle.isPending}
                      aria-label={t('admin.skills.enabled')}
                      onCheckedChange={(enabled) => toggle.mutate({ id: skill.id, enabled }, { onError: (failure) => toast.error(errorText(failure)) })}
                    />
                  </TableCell>
                  <TableCell className="w-24 text-end">
                    <Button variant="ghost" size="icon" aria-label={t('admin.skills.edit')} onClick={() => setEditing(skill.id)}>
                      <Pencil />
                    </Button>
                    <Button variant="ghost" size="icon" aria-label={t('admin.skills.delete')} onClick={() => setDeleting(skill)}>
                      <Trash2 />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <Dialog open={editing !== undefined} onOpenChange={(open) => !open && setEditing(undefined)}>
        <DialogContent className="sm:max-w-4xl">
          {editing !== undefined && <EditorLoader scope={scope} id={editing} onClose={() => setEditing(undefined)} />}
        </DialogContent>
      </Dialog>

      <AlertDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('admin.skills.delete-title', { name: deleting?.name ?? '' })}</AlertDialogTitle>
            <AlertDialogDescription>{t('admin.skills.delete-description')}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('admin.skills.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                deleting &&
                remove.mutate(deleting.id, {
                  onSuccess: () => toast.success(t('admin.skills.deleted')),
                  onError: (failure) => toast.error(errorText(failure)),
                })
              }
            >
              {t('admin.skills.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
