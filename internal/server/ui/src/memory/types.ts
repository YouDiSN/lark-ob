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
  effectiveScore: number
  evidenceMessageIds: string[]
  lastEvidenceAt: number
  createdAt: number
  updatedAt: number
	 sourceType: 'agent' | 'manual'
	 importance: 'normal' | 'important' | 'constraint'
	 validFrom?: number
	 validUntil?: number
	 pinned: boolean
	 status: 'scheduled' | 'active' | 'expired'
}

export type MemorySubjectOption = {
  id: string
  name: string
  type: 'person' | 'chat'
  chatId: string
  chatName?: string
  chatType?: 'p2p' | 'group'
}

export type MemorySubjectOptions = { people: MemorySubjectOption[]; chats: MemorySubjectOption[] }

export type ManualMemoryInput = {
  subjectType: 'person' | 'chat'
  subjectId: string
  subjectName: string
  chatId: string
  category: string
  content: string
  importance: 'normal' | 'important' | 'constraint'
  validFrom: number
  validUntil: number
  pinned: boolean
}

export type MemoryStats = {
  total: number
  personMemories: number
  groupMemories: number
  p2pMemories: number
  people: number
  groups: number
  p2pChats: number
}

export type MemoryWarehouse = {
  stats: MemoryStats
  items: MemoryClaim[]
  decayHalfLifeDays: number
  totalMatches: number
  hasMore: boolean
  nextOffset: number
}
