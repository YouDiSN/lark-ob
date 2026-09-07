export type AgentStatus = {
  enabled: boolean
  framework: string
  model: string
  endpoint: string
  error?: string
}

export type InitializationStatus = {
  running: boolean
  complete: boolean
  phase: 'pending' | 'discovering' | 'history' | 'memory' | 'complete' | 'error' | 'disabled'
  lookbackDays: number
  totalChats: number
  historyDone: number
  memoryDone: number
  messageCount: number
  memoryClaims: number
  failedChats: number
  currentChat?: string
  error?: string
  failures?: Array<{
    chatId: string
    chatName: string
    stage: 'history' | 'memory'
    error: string
    attempts: number
    durationMs?: number
    failedAt: number
  }>
}

export type MemoryClaim = {
  id: number
  subjectType: 'person' | 'chat'
  subjectId: string
  subjectName?: string
  chatId: string
  sourceChatName?: string
  sourceChatType?: 'p2p' | 'group'
  category: string
  content: string
  confidence: number
  evidenceMessageIds: string[]
	 sourceType: 'agent' | 'manual'
	 importance: 'normal' | 'important' | 'constraint'
	 validFrom?: number
	 validUntil?: number
	 pinned: boolean
	 status: 'scheduled' | 'active' | 'expired'
}

export type PersonImpression = {
  personId: string
  personName: string
  tags: string[]
  summary: string
  communicationGuidance: string
  confidence: number
  evidenceMessageIds: string[]
  messageCount: number
  conversationCount: number
  windowStart: number
  windowEnd: number
  model: string
  version: number
  generatedAt: number
}

export type StyleProfile = {
  scopeType: 'self' | 'relationship' | 'chat'
  scopeId: string
  scopeName: string
  tags: string[]
  summary: string
  guidance: string
  confidence: number
  sampleCount: number
  generatedAt: number
}

export type ProfileBundle = {
  personId?: string
  personName?: string
  directoryProfile?: {
    id: string
    name: string
    avatar?: string
    base?: string
    department?: string
    checkedAt: number
  }
  impression?: PersonImpression
  selfStyle?: StyleProfile
  relationshipStyle?: StyleProfile
}

export type ProfileMaintenanceStatus = {
  running: boolean
  phase: 'waiting' | 'sync' | 'memory' | 'impression' | 'error' | 'disabled'
  lookbackDays: number
  schedule: string
  timezone: string
  lastSucceededAt?: number
  nextRunAt?: number
  impressionsSaved: number
  stylesSaved: number
  error?: string
}

export type ReplySuggestion = { text: string; tone?: string; rationale?: string }
export type ReplyRecommendation = {
  thinking: string
  suggestions: ReplySuggestion[]
  evidence: { type: string; reference: string; excerpt?: string }[]
  model: string
  instruction?: string
  appliedStyles?: string[]
  replyTarget?: {
    messageId: string
    senderId: string
    senderName: string
    content: string
    createdAt: number
    reason: 'mention' | 'selected' | 'latest'
  }
}
