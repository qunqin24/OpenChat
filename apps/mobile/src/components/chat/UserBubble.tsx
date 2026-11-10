import type { Message } from '@openchat/api-client'
import * as Clipboard from 'expo-clipboard'
import { Check, Copy, Pencil } from 'lucide-react-native'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native'

import { parseUserMessage, rewordUserMessage } from '@openchat/chat-core/user-message'

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
 * The person's message, at the right in a bubble. What was picked for it
 * travels in its text (chat-core/src/user-message.ts): the passages it asks
 * about are quoted above it, and deep research, skills and pictures picked on
 * the web lead its words as they do there. Copy and edit sit under it, and
 * work on the words alone; an edit keeps the rest.
 */
export function UserBubble({ message, onEdit, refused, onEditStart }: Props) {
  const { t } = useTranslation()
  const colors = useColors()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [copied, setCopied] = useState(false)
  const parsed = parseUserMessage(message.content)
  const { research, skills, pictures, quotes, words } = parsed

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
                onEdit(rewordUserMessage(parsed, draft.trim()))
              }}
            />
          </View>
        </View>
      </View>
    )
  }

  return (
    <View style={styles.wrap}>
      {quotes.map((quote, i) => (
        <Text key={i} style={[styles.quote, { color: colors.text3, borderLeftColor: colors.line }]} numberOfLines={3}>
          {quote.passage}
        </Text>
      ))}
      <MessageAttachments items={message.attachments ?? []} removed={message.removedAttachments} references={message.references} />
      <View style={[styles.bubble, { backgroundColor: refused ? colors.errFill : colors.bubble }]}>
        <Text style={[styles.text, { color: colors.text }]} selectable>
          {research ? <Text style={{ color: colors.info }}>{t('chat.research.token')} </Text> : null}
          {skills.map((name) => (
            <Text key={name} style={{ color: colors.info }}>
              /{name}{' '}
            </Text>
          ))}
          {pictures.map((handle, i) => (
            <Text key={handle} style={{ color: colors.info }}>
              {t('chat.picture.ref-token', { count: i + 1 })}{' '}
            </Text>
          ))}
          {words}
        </Text>
      </View>
      {!refused ? (
        <View style={styles.actions}>
          <Pressable
            hitSlop={8}
            style={styles.action}
            accessibilityLabel={t('chat.copy')}
            onPress={() =>
              void Clipboard.setStringAsync(words).then(() => {
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
                setDraft(words)
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
