export type KnowledgeScope = 'global' | 'chat'

export type KnowledgeSource = {
  id: number
  type: 'lark_doc' | 'lark_wiki'
  url: string
  title: string
  scopeType: KnowledgeScope | 'person' | 'project'
  scopeId?: string
  revision?: string
  status: 'ready' | 'error'
  error?: string
  chunkCount: number
  createdAt: number
  updatedAt: number
  syncedAt?: number
}

export type KnowledgeSearchResult = {
  sourceId: number
  chunkId: number
  title: string
  url: string
  heading?: string
  content: string
  blockId?: string
  score: number
}

export type KnowledgeChatOption = { id: string; name: string }

export type KnowledgeSummary = {
  sourceId: number
  summary: string
  facts: string[]
  model: string
  createdAt: number
  updatedAt: number
}
