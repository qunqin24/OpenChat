import type { Message } from '@openchat/api-client'
import * as Clipboard from 'expo-clipboard'
import { Check, Copy, Pencil } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native'

import { parseQuotes } from '@openchat/chat-core/document-selection'

import { Button } from '@/components/Button'
import { useColors } from '@/theme'
import { MessageAttachments } from './MessageAttachments'

interface Props {
  message: Pick<Message, 'content'> & Partial<Pick<Message, 'id' | 'attachments' | 'removedAttachments' | 'references'>>
  /** Rewords the question; absent while that cannot be done (a run going, a message on its way). */
  onEdit?: (content: string) => void
  /** Refused by moderation: marked, and reworded in the composer instead. */
  refused?: boolean
  /** The field opened: the list brings it into view above the keyboard. */
  onEditStart?: () => void
}

/**
 * The person's message, at the right in a bubble. A passage of a writing
 * block it asks about comes wrapped in `<document-selection>`: shown as the
 * request, the passage quoted above it. Copy and edit sit under it.
 */
export function UserBubble({ message, onEdit, refused, onEditStart }: Props) {
  const { t } = useTranslation()
  const colors = useColors()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [copied, setCopied] = useState(false)
  const quoted = parseQuotes(message.content)
  const request = quoted ? quoted.request : message.content

  if (editing && onEdit) {
    return (
      <View style={[styles.editor, { backgroundColor: colors.bubble }]}>
        <TextInput
          style={[styles.editInput, { color: colors.text }]}
          value={draft}
          onChangeText={setDraft}
          multiline
          autoFocus
        />
        <View style={styles.editActions}>
          <View style={styles.editButton}>
            <Button kind="secondary" label={t('chat.edit-cancel')} onPress={() => setEditing(false)} />
          </View>
          <View style={styles.editButton}>
            <Button
              label={t('chat.edit-send')}
              disabled={!draft.trim()}
              onPress={() => {
                setEditing(false)
                onEdit(draft.trim())
              }}
            />
          </View>
        </View>
      </View>
    )
  }

  return (
    <View style={styles.wrap}>
      {quoted?.quotes.map((quote, i) => (
        <Text key={i} style={[styles.quote, { color: colors.text3, borderLeftColor: colors.line }]} numberOfLines={3}>
          {quote.passage}
        </Text>
      ))}
      <MessageAttachments items={message.attachments ?? []} removed={message.removedAttachments} references={message.references} />
      <View style={[styles.bubble, { backgroundColor: refused ? colors.errFill : colors.bubble }]}>
        <Text style={[styles.text, { color: colors.text }]} selectable>
          {request}
        </Text>
      </View>
      {!refused ? (
        <View style={styles.actions}>
          <Pressable
            hitSlop={8}
            style={styles.action}
            accessibilityLabel={t('chat.copy')}
            onPress={() =>
              void Clipboard.setStringAsync(request).then(() => {
                setCopied(true)
                setTimeout(() => setCopied(false), 1500)
              })
            }
          >
            {copied ? <Check size={16} color={colors.text3} /> : <Copy size={16} color={colors.text3} />}
          </Pressable>
          {onEdit ? (
            <Pressable
              hitSlop={8}
              style={styles.action}
              accessibilityLabel={t('chat.edit')}
              onPress={() => {
                setDraft(request)
                setEditing(true)
                onEditStart?.()
              }}
            >
              <Pencil size={16} color={colors.text3} />
            </Pressable>
          ) : null}
        </View>
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  wrap: { alignItems: 'flex-end', gap: 4 },
  bubble: { maxWidth: '85%', borderRadius: 20, paddingHorizontal: 16, paddingVertical: 10 },
  text: { fontSize: 16, lineHeight: 24 },
  quote: { maxWidth: '85%', fontSize: 13, lineHeight: 19, borderLeftWidth: 2, paddingLeft: 8 },
  actions: { flexDirection: 'row', gap: 2 },
  action: { padding: 6 },
  editor: { borderRadius: 20, padding: 12, gap: 10 },
  editInput: { fontSize: 16, lineHeight: 24, maxHeight: 220, textAlignVertical: 'top' },
  editActions: { flexDirection: 'row', justifyContent: 'flex-end', gap: 8 },
  editButton: { minWidth: 88 },
})
