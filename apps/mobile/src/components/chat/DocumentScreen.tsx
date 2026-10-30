import { isOpenChatError } from '@openchat/api-client'
import * as Clipboard from 'expo-clipboard'
import { Check, ChevronLeft, ChevronRight, Copy, MessageSquarePlus, X } from 'lucide-react-native'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Modal, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native'
import { KeyboardAvoidingView } from 'react-native-keyboard-controller'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

import { passageFor } from '@openchat/chat-core/document-selection'

import { useDocument, useSaveDocument } from '@/hooks/documents'
import { useDocumentQuote, useDocumentScreen } from '@/lib/documents'
import { lowerKeyboardOr } from '@/lib/back'
import { toast } from '@/lib/toast'
import { useColors } from '@/theme'
import { Markdown } from './Markdown'

/** How long typing pauses before an edit is saved as a version. */
const SAVE_AFTER_MS = 1200

/**
 * The writing block open full screen (the web's DocumentPanel). The latest
 * version reads as laid out and is edited as its Markdown - the same text the
 * model writes - saved as a new version a moment after typing stops. The
 * arrows step through the versions, the model's and the person's; an older
 * one can be made the current one again. A version the model writes while
 * this is open comes in, unless an edit is waiting to be saved, which then
 * meets a conflict and gives way to the model's. A passage selected while
 * editing can be quoted to the composer as a request for a change.
 */
export function DocumentScreen() {
  const documentId = useDocumentScreen((s) => s.documentId)
  if (!documentId) return null
  return <Screen key={documentId} documentId={documentId} />
}

function Screen({ documentId }: { documentId: string }) {
  const { t } = useTranslation()
  const colors = useColors()
  const insets = useSafeAreaInsets()
  const doc = useDocument(documentId)
  const save = useSaveDocument(documentId)
  const { viewing, view, close } = useDocumentScreen()
  const setQuote = useDocumentQuote((s) => s.set)
  const versions = doc.data?.versions ?? []
  const latest = versions.at(-1)
  const shown = viewing === null ? latest : versions.find((v) => v.version === viewing)

  const [editing, setEditing] = useState(false)
  const [selection, setSelection] = useState({ start: 0, end: 0 })
  const [copied, setCopied] = useState(false)
  // The person's edit not saved yet, on top of version `base`. Without one the
  // latest version is what shows - so a version the model writes comes in by
  // itself; with one, the model's waits until the save meets the conflict.
  const [edit, setEdit] = useState<{ base: number; draft: string; title: string } | null>(null)
  const draft = edit?.draft ?? latest?.content ?? ''
  const title = edit?.title ?? latest?.title ?? ''
  const held = useRef(edit)
  useEffect(() => {
    held.current = edit
  }, [edit])
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const saving = useRef<Promise<void> | null>(null)

  // One save at a time: a second sent while one is out would carry the same
  // base and be refused as a conflict; it waits, and takes what was typed since.
  const flush = useCallback(async () => {
    clearTimeout(timer.current)
    while (saving.current) await saving.current
    const pending = held.current
    if (!pending || !latest) return
    const run = (async () => {
      try {
        const saved = await save.mutateAsync({ title: pending.title.trim() || latest.title, content: pending.draft, baseVersion: pending.base })
        const version = saved.versions.at(-1)?.version ?? pending.base
        // Typed on while it was out: that stays, now on top of the saved version.
        setEdit((now) => (now === pending ? null : now && { ...now, base: version }))
        held.current = held.current === pending ? null : held.current && { ...held.current, base: version }
      } catch (error) {
        if (isOpenChatError(error) && error.code === 'version_conflict') {
          // The model wrote meanwhile: its version is the one shown.
          toast.info(t('document.conflict'))
          setEdit(null)
          held.current = null
          void doc.refetch()
        } else {
          toast.error(t('document.save-failed'))
        }
      }
    })()
    saving.current = run
    try {
      await run
    } finally {
      saving.current = null
    }
  }, [latest, save, doc, t])
  const flushRef = useRef(flush)
  useEffect(() => {
    flushRef.current = flush
  }, [flush])
  const edited = (change: { draft?: string; title?: string }) => {
    if (!latest) return
    setEdit((now) => ({ base: now?.base ?? latest.version, draft, title, ...now, ...change }))
    clearTimeout(timer.current)
    timer.current = setTimeout(() => void flushRef.current(), SAVE_AFTER_MS)
  }
  // What is not saved yet is, when the screen closes.
  useEffect(() => () => void flushRef.current(), [])

  const leave = () => {
    void flush()
    close()
  }
  const index = shown ? versions.indexOf(shown) : -1
  const step = (by: -1 | 1) => {
    const next = versions[index + by]
    if (!next) return
    void flush()
    setEditing(false)
    view(next.version === latest?.version ? null : next.version)
  }
  const restore = async () => {
    if (!shown || !latest) return
    try {
      await save.mutateAsync({ title: shown.title, content: shown.content, baseVersion: latest.version })
      toast.info(t('document.restored', { version: shown.version }))
      view(null)
    } catch {
      toast.error(t('document.save-failed'))
    }
  }
  // "提出修改请求": the selected passage, as it stands in the document, goes to
  // the composer as a quote - once it is saved, so the model edits what is shown.
  const ask = async () => {
    if (!doc.data) return
    const selected = draft.slice(selection.start, selection.end)
    const passage = passageFor(draft, [selected])
    if (!passage) return toast.info(t('mobile.quote-not-unique'))
    await flush()
    setQuote({ conversationId: doc.data.conversationId, documentId, title: title || latest?.title || '', passage })
    leave()
  }

  const old = viewing !== null
  const selectedSome = editing && selection.end > selection.start

  return (
    <Modal visible animationType="slide" onRequestClose={() => lowerKeyboardOr(leave)} statusBarTranslucent navigationBarTranslucent>
      <KeyboardAvoidingView behavior="padding" style={[styles.page, { backgroundColor: colors.bg, paddingTop: insets.top }]}>
        <View style={[styles.bar, { borderBottomColor: colors.line }]}>
          <Pressable hitSlop={10} onPress={leave} accessibilityLabel={t('document.close')}>
            <X size={22} color={colors.text} />
          </Pressable>
          <View style={styles.flex} />
          <Pressable hitSlop={8} disabled={index <= 0} onPress={() => step(-1)} accessibilityLabel={t('document.previous')}>
            <ChevronLeft size={22} color={index <= 0 ? colors.text3 : colors.text} />
          </Pressable>
          <Text style={[styles.version, { color: colors.text2 }]}>
            {shown ? t('document.pill-version', { version: shown.version }) : ''}
          </Text>
          <Pressable hitSlop={8} disabled={index < 0 || index >= versions.length - 1} onPress={() => step(1)} accessibilityLabel={t('document.next')}>
            <ChevronRight size={22} color={index < 0 || index >= versions.length - 1 ? colors.text3 : colors.text} />
          </Pressable>
          <Pressable
            hitSlop={8}
            disabled={!shown}
            accessibilityLabel={t('document.copy')}
            onPress={() =>
              shown &&
              void Clipboard.setStringAsync(old ? shown.content : draft).then(() => {
                setCopied(true)
                setTimeout(() => setCopied(false), 1500)
              })
            }
          >
            {copied ? <Check size={19} color={colors.text2} /> : <Copy size={19} color={colors.text2} />}
          </Pressable>
          {!old ? (
            <Pressable
              hitSlop={8}
              onPress={() => {
                if (editing) void flush()
                setEditing(!editing)
              }}
            >
              <Text style={[styles.action, { color: colors.text }]}>{editing ? t('mobile.document-done') : t('mobile.document-edit')}</Text>
            </Pressable>
          ) : null}
        </View>

        {old && shown ? (
          <View style={[styles.notice, { backgroundColor: colors.bubble }]}>
            <Text style={[styles.noticeText, { color: colors.text2 }]}>
              {t('document.viewing', { version: shown.version, total: versions.length, who: t(`document.by.${shown.source}`) })}
            </Text>
            <View style={styles.noticeActions}>
              <Pressable onPress={() => void restore()} disabled={save.isPending}>
                <Text style={[styles.action, { color: colors.text }]}>{t('document.restore')}</Text>
              </Pressable>
              <Pressable onPress={() => view(null)}>
                <Text style={[styles.action, { color: colors.text2 }]}>{t('document.latest')}</Text>
              </Pressable>
            </View>
          </View>
        ) : null}

        {!shown ? (
          <Text style={[styles.empty, { color: colors.text3 }]}>{doc.isError ? t('document.gone') : ''}</Text>
        ) : editing && !old ? (
          // The inset is here, not on the KeyboardAvoidingView: in padding mode it sets paddingBottom itself.
          <View style={[styles.flex, { paddingBottom: insets.bottom }]}>
            <TextInput
              style={[styles.titleInput, { color: colors.text }]}
              value={title}
              onChangeText={(value) => edited({ title: value })}
              placeholder={t('document.title')}
              placeholderTextColor={colors.text3}
            />
            <TextInput
              style={[styles.editor, { color: colors.text }]}
              value={draft}
              onChangeText={(value) => edited({ draft: value })}
              onSelectionChange={(event) => setSelection(event.nativeEvent.selection)}
              multiline
              placeholder={t('document.placeholder')}
              placeholderTextColor={colors.text3}
              accessibilityLabel={t('document.editor')}
              autoCapitalize="none"
            />
            {selectedSome ? (
              <Pressable style={[styles.ask, { backgroundColor: colors.accent }]} onPress={() => void ask()}>
                <MessageSquarePlus size={16} color={colors.accentFg} />
                <Text style={[styles.askText, { color: colors.accentFg }]}>{t('document.ask')}</Text>
              </Pressable>
            ) : null}
          </View>
        ) : (
          <ScrollView contentContainerStyle={[styles.read, { paddingBottom: insets.bottom + 20 }]}>
            <Text style={[styles.heading, { color: colors.text }]}>{old ? shown.title : title}</Text>
            <Markdown text={old ? shown.content : draft} />
          </ScrollView>
        )}
      </KeyboardAvoidingView>
    </Modal>
  )
}

const styles = StyleSheet.create({
  page: { flex: 1 },
  flex: { flex: 1 },
  bar: { flexDirection: 'row', alignItems: 'center', gap: 14, paddingHorizontal: 16, paddingVertical: 12, borderBottomWidth: StyleSheet.hairlineWidth },
  version: { fontSize: 13, minWidth: 28, textAlign: 'center' },
  action: { fontSize: 15, fontWeight: '500' },
  notice: { margin: 12, padding: 12, borderRadius: 14, gap: 8 },
  noticeText: { fontSize: 13, lineHeight: 19 },
  noticeActions: { flexDirection: 'row', gap: 20 },
  empty: { padding: 24, textAlign: 'center' },
  read: { padding: 20, gap: 14 },
  heading: { fontSize: 22, lineHeight: 30, fontWeight: '600' },
  titleInput: { fontSize: 20, fontWeight: '600', paddingHorizontal: 20, paddingTop: 16, paddingBottom: 6 },
  editor: { flex: 1, fontSize: 16, lineHeight: 24, paddingHorizontal: 20, paddingVertical: 8, textAlignVertical: 'top', fontFamily: 'monospace' },
  ask: { position: 'absolute', alignSelf: 'center', bottom: 16, flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 16, height: 40, borderRadius: 20 },
  askText: { fontSize: 14, fontWeight: '500' },
})
