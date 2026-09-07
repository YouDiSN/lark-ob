export type Mention = { id: string; key?: string; name?: string }

export type Chat = {
  id: string
  name: string
  description?: string
  type: 'p2p' | 'group'
  avatar?: string
  lastMessage: string
  lastTime: number
  lastPosition?: number
  newMessages: number
  mentionCount: number
  external: boolean
  muted: boolean
  inMessageBox: boolean
}

export type Message = {
  id: string
  chatId: string
  senderId: string
  senderName: string
  senderAvatar?: string
  content: string
  type: string
  createdAt: number
  position?: number
  isSelf: boolean
  mentions?: Mention[]
  mentionsSelf: boolean
}

export type Status = {
  mode: 'lark' | 'lark-cli'
  connected: boolean
  configured: boolean
  lastSync?: number
  syncing: boolean
  error?: string
  authMode?: 'cli' | 'oauth'
}

export type HistoryState = { hasMore: boolean; pageToken?: string }

export function imageKeys(content: string) {
  return [...new Set(content.match(/img_[A-Za-z0-9_-]+/g) || [])]
}

export function messageText(content: string) {
  return content
    .replace(/!\[Image\]\(img_[A-Za-z0-9_-]+\)/g, '')
    .replace(/\[Image:\s*img_[A-Za-z0-9_-]+\]/g, '')
    .replace(/(?:🖼️\s*)?Image\(img_key:img_[A-Za-z0-9_-]+\)/g, '')
    .replace(/\(img_key:img_[A-Za-z0-9_-]+\)/g, '')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}

export function messagePreview(content: string) {
  const text = messageText(content)
  return imageKeys(content).length ? `[图片]${text ? ` ${text}` : ''}` : text
}

// A mention is considered pending until the user has sent a newer message.
// This mirrors the authoritative target selection in the Go Agent module.
export function latestPendingMention(messages: Message[]) {
  const lastSelf = messages.reduce<Message | undefined>((latest, message) =>
    message.isSelf && (!latest || messageAfter(message, latest)) ? message : latest, undefined)
  for (let index = messages.length - 1; index >= 0; index--) {
    const message = messages[index]
    if (!message.isSelf && message.mentionsSelf && (!lastSelf || messageAfter(message, lastSelf))) return message
  }
  return undefined
}

function messageAfter(left: Message, right: Message) {
  if (left.createdAt !== right.createdAt) return left.createdAt > right.createdAt
  if ((left.position || 0) !== (right.position || 0)) return (left.position || 0) > (right.position || 0)
  return left.id > right.id
}
