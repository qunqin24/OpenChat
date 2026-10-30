import type { ToolCallPart } from '@openchat/api-client'
import { useQueryClient } from '@tanstack/react-query'
import * as Clipboard from 'expo-clipboard'
import { Check, Copy, Maximize2, NotebookPen } from 'lucide-react-native'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Pressable, StyleSheet, Text, View } from 'react-native'

import { changedPassages, pendingEditOf, pendingTitle, streamingText, type DocumentChange } from '@openchat/chat-core/document-tool'
import { queryKeys } from '@openchat/chat-core/query-keys'

import { useDocument } from '@/hooks/documents'
import { useDocumentScreen } from '@/lib/documents'
import { useColors } from '@/theme'
import { Markdown } from './Markdown'
import { Pulse } from './Pulse'

/** How much of the text in flight the card shows: its last lines, as they come. */
const LIVE_TAIL = 600

/**
 * A writing block in a reply (the `document` tool; the web's DocumentCard):
 * the text as this call left it - where it created the document, the whole;
 * where it changed it, the passages it changed - with copy, and a tap opens
 * it full screen. While the call is still being written, the words as they
 * come; the model starting on a document opens it, once, so the change can be
 * watched.
 */
export function DocumentCard({ change, call }: { change: DocumentChange | null; call: ToolCallPart }) {
  const { t } = useTranslation()
  const colors = useColors()
  const queryClient = useQueryClient()
  const pending = change ? null : pendingEditOf(call)
  const doc = useDocument(change?.documentId ?? pending?.documentId ?? null, change?.version ?? 0)
  const open = useDocumentScreen((s) => s.open)
  const [copied, setCopied] = useState(false)

  // A document written or changed: the conversation's list has a new one, or a new order.
  const listOf = doc.data?.conversationId
  useEffect(() => {
    if (change && listOf) void queryClient.invalidateQueries({ queryKey: queryKeys.conversationDocuments(listOf) })
  }, [change, listOf, queryClient])

  // The model starting on a document the reader can see opens it once.
  const opened = useRef(false)
  useEffect(() => {
    if (!pending || !doc.data || opened.current) return
    opened.current = true
    if (useDocumentScreen.getState().documentId !== pending.documentId) open(pending.documentId)
  }, [pending, doc.data, open])

  if (!change) {
    const name = pending ? (doc.data?.versions.at(-1)?.title ?? '') : pendingTitle(call)
    const updating = pending !== null || /"op"\s*:\s*"update"/.test(call.rawArguments ?? '')
    const text = streamingText(call)
    return (
      <Pressable
        disabled={!pending}
        onPress={() => pending && open(pending.documentId)}
        style={[styles.card, { backgroundColor: colors.card, borderColor: colors.line }]}
      >
        <View style={styles.head}>
          <Pulse />
          <Text style={[styles.label, { color: colors.text2 }]}>{t(updating ? 'document.rewriting' : 'document.writing')}</Text>
        </View>
        {name ? <Text style={[styles.title, { color: colors.text }]}>{name}</Text> : null}
        {text ? (
          <Text style={[styles.live, { color: colors.text2 }]} numberOfLines={8}>
            {text.length > LIVE_TAIL ? `…${text.slice(-LIVE_TAIL)}` : text}
          </Text>
        ) : null}
      </Pressable>
    )
  }

  // This call's version, and the one it changed.
  const versions = doc.data?.versions
  const made = versions?.find((v) => v.version === change.version) ?? versions?.at(-1)
  const before = change.op === 'update' && made ? versions?.find((v) => v.version === made.version - 1) : undefined
  const title = made?.title ?? change.title
  const last = versions?.at(-1)?.version
  const show = () => open(change.documentId, made && made.version !== last ? made.version : null)

  return (
    <View style={[styles.card, { backgroundColor: colors.card, borderColor: colors.line }]}>
      <View style={styles.head}>
        <NotebookPen size={15} color={colors.text3} />
        <Text style={[styles.label, styles.flex, { color: colors.text3 }]}>
          {t(change.op === 'update' ? 'document.updated' : 'document.label')}
          {made ? ` · ${t('document.pill-version', { version: made.version })}` : ''}
        </Text>
        <Pressable
          hitSlop={8}
          disabled={!made}
          accessibilityLabel={t('document.copy')}
          onPress={() =>
            made &&
            void Clipboard.setStringAsync(made.content).then(() => {
              setCopied(true)
              setTimeout(() => setCopied(false), 1500)
            })
          }
        >
          {copied ? <Check size={17} color={colors.text2} /> : <Copy size={17} color={colors.text2} />}
        </Pressable>
        <Pressable hitSlop={8} disabled={!doc.data} accessibilityLabel={t('document.open')} onPress={show}>
          <Maximize2 size={17} color={colors.text2} />
        </Pressable>
      </View>
      <Pressable onPress={show} disabled={!doc.data} style={styles.body}>
        <Text style={[styles.title, { color: colors.text }]}>{title}</Text>
        {made ? (
          changedPassages(before?.content, made.content)
            .slice(0, 3)
            .map((part, i) =>
              part === true ? (
                <Text key={i} style={[styles.gap, { color: colors.text3 }]}>
                  ⋯
                </Text>
              ) : (
                <Markdown key={i} text={part} />
              ),
            )
        ) : doc.isError ? (
          <Text style={[styles.label, { color: colors.text3 }]}>{t('document.gone')}</Text>
        ) : null}
      </Pressable>
    </View>
  )
}

const styles = StyleSheet.create({
  card: { borderRadius: 18, borderWidth: StyleSheet.hairlineWidth, padding: 14, gap: 8 },
  head: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  flex: { flex: 1 },
  label: { fontSize: 13 },
  body: { maxHeight: 280, overflow: 'hidden', gap: 8 },
  title: { fontSize: 17, lineHeight: 24, fontWeight: '600' },
  live: { fontSize: 14, lineHeight: 21 },
  gap: { textAlign: 'center' },
})
