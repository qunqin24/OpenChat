import type { components } from './schema'

type Schemas = components['schemas']

export type Conversation = Schemas['Conversation']
export type ConversationPage = Schemas['ConversationPage']
export type Message = Schemas['Message']
export type MessagePage = Schemas['MessagePage']
export type Attachment = Schemas['Attachment']
export type Upload = Schemas['Upload']
export type Health = Schemas['Health']
export type ApiError = Schemas['Error']
export type Role = Schemas['Role']
export type MessageStatus = Schemas['MessageStatus']

export type CreateConversationRequest = Schemas['CreateConversationRequest']
export type UpdateConversationRequest = Schemas['UpdateConversationRequest']
export type CreateMessageRequest = Schemas['CreateMessageRequest']
export type CreateUploadRequest = Schemas['CreateUploadRequest']

export type StreamEvent = Schemas['StreamEvent']
export type StreamStart = Schemas['StreamStart']
export type StreamDelta = Schemas['StreamDelta']
export type StreamDone = Schemas['StreamDone']
export type StreamError = Schemas['StreamError']
