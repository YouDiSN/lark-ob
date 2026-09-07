import { useCallback, useEffect, useState } from 'react'
import { BrainCircuit, ChevronDown, Clock3, MessageCircle, RefreshCw, Sparkles, UserRound, Users } from 'lucide-react'
import { api } from '../lib/api'
import type { AgentStatus, InitializationStatus, MemoryClaim, ProfileBundle, ProfileMaintenanceStatus } from './types'
import './agent.css'

type Props = { chatId: string; personId?: string }

const categoryName: Record<string, string> = {
  preference: '偏好', fact: '事实', commitment: '承诺', relationship: '关系', project_context: '项目',
}

const displayTime = (value?: number) => value ? new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false }).format(value) : '尚未执行'

export function AgentPanel({ chatId, personId }: Props) {
  const [status, setStatus] = useState<AgentStatus>()
  const [initialization, setInitialization] = useState<InitializationStatus>()
  const [memories, setMemories] = useState<MemoryClaim[]>([])
  const [profile, setProfile] = useState<ProfileBundle>({})
  const [maintenance, setMaintenance] = useState<ProfileMaintenanceStatus>()
  const [impressionOpen, setImpressionOpen] = useState(false)
  const [memoryOpen, setMemoryOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [profileBusy, setProfileBusy] = useState(false)
  const [error, setError] = useState<string>()

  const load = useCallback(async () => {
    const target = personId ? `?personId=${encodeURIComponent(personId)}` : ''
    const [nextStatus, nextInitialization, nextMemories, nextProfile, nextMaintenance] = await Promise.all([
      api<AgentStatus>('/api/agent/status'),
      api<InitializationStatus>('/api/initialization/status'),
      api<MemoryClaim[]>(`/api/chats/${encodeURIComponent(chatId)}/memories`),
      api<ProfileBundle>(`/api/chats/${encodeURIComponent(chatId)}/profile${target}`),
      api<ProfileMaintenanceStatus>('/api/profile-maintenance/status'),
    ])
    setStatus(nextStatus)
    setInitialization(nextInitialization)
    setMemories(nextMemories)
    setProfile(nextProfile)
    setMaintenance(nextMaintenance)
  }, [chatId, personId])

  useEffect(() => {
    setError(undefined)
    setImpressionOpen(false)
    setMemoryOpen(false)
    void load().catch(err => setError((err as Error).message))
  }, [load])

  useEffect(() => {
    if (initialization?.complete || initialization?.phase === 'error' || initialization?.phase === 'disabled') return
    const timer = window.setInterval(() => void load().catch(() => {}), 3000)
    return () => window.clearInterval(timer)
  }, [initialization?.complete, initialization?.phase, load])

  useEffect(() => {
    if (!maintenance?.running) return
    const timer = window.setInterval(() => void load().catch(() => {}), 4000)
    return () => window.clearInterval(timer)
  }, [maintenance?.running, load])

  async function extractMemory() {
    setBusy(true); setError(undefined)
    try {
      const result = await api<{ claims: MemoryClaim[] }>(`/api/chats/${encodeURIComponent(chatId)}/memories/extract`, { method: 'POST' })
      setMemories(result.claims)
    } catch (err) { setError((err as Error).message) } finally { setBusy(false) }
  }

  async function refreshProfile() {
    setProfileBusy(true); setError(undefined)
    try {
      const target = personId ? `?personId=${encodeURIComponent(personId)}` : ''
      const result = await api<ProfileBundle>(`/api/chats/${encodeURIComponent(chatId)}/profile/refresh${target}`, { method: 'POST' })
      setProfile(result)
      setImpressionOpen(true)
    } catch (err) { setError((err as Error).message) } finally { setProfileBusy(false) }
  }

  return <section className="agent-panel">
    <div className="agent-heading"><div><BrainCircuit size={17}/><strong>记忆 Agent</strong></div><span className={status?.enabled ? 'online' : ''}>{status?.enabled ? status.model : '未配置'}</span></div>
    <p className="agent-description">提炼当前会话中的人物与群组记忆；推荐回复已移至聊天底部。</p>
    {initialization?.running && <div className="initialization-progress">
      <div><RefreshCw className="spin" size={14}/><span>{initialization.phase === 'history' ? '正在同步最近消息' : initialization.phase === 'memory' ? '正在自动生成记忆' : '正在初始化'}</span><em>{initialization.phase === 'memory' ? initialization.memoryDone : initialization.historyDone}/{initialization.totalChats || '—'}</em></div>
      <progress value={initialization.phase === 'memory' ? initialization.memoryDone : initialization.historyDone} max={initialization.totalChats || 1}/>
      <small>默认范围：最近 {initialization.lookbackDays} 天{initialization.currentChat ? ` · ${initialization.currentChat}` : ''}</small>
    </div>}
    {initialization?.complete && <div className="initialization-ready">最近 {initialization.lookbackDays} 天的聊天记录与记忆已自动初始化</div>}
    {maintenance && maintenance.phase !== 'disabled' && <div className="profile-schedule"><Clock3 size={13}/><span>{maintenance.running ? '正在更新每日画像' : `每日 ${maintenance.schedule} 更新`}</span><em>{maintenance.nextRunAt ? `下次 ${displayTime(maintenance.nextRunAt)}` : `最近 ${displayTime(maintenance.lastSucceededAt)}`}</em></div>}
    {maintenance?.error && <div className="agent-warning">每日画像：{maintenance.error}</div>}
    {initialization?.phase === 'error' && initialization.error && <div className="agent-warning initialization-error-detail">
      <strong>自动初始化：{initialization.error}</strong>
      {!!initialization.failures?.length && <details>
        <summary>查看 {initialization.failures.length} 个失败会话</summary>
        {initialization.failures.map(failure => <div key={failure.chatId}>
          <b>{failure.chatName}</b>
          <span>{failure.stage === 'memory' ? '记忆提取' : '历史同步'} · {failure.attempts} 次尝试{failure.durationMs ? ` · ${(failure.durationMs / 1000).toFixed(1)} 秒` : ''}</span>
          <code>{failure.error}</code>
        </div>)}
      </details>}
    </div>}
    {status?.error && <div className="agent-warning">{status.error}</div>}
    {error && <div className="agent-warning">{error}</div>}
    <div className="agent-actions">
      <button onClick={() => void extractMemory()} disabled={!status?.enabled || busy}>{busy ? <RefreshCw className="spin" size={15}/> : <BrainCircuit size={15}/>}{busy ? '正在提炼' : '提炼当前会话记忆'}</button>
    </div>
    <div className={`profile-card ${impressionOpen ? 'open' : ''}`}>
      <button className="profile-toggle" onClick={() => setImpressionOpen(value => !value)} aria-expanded={impressionOpen}>
        <span><Sparkles size={15}/><strong>基础印象</strong></span><span>{profile.impression ? `${profile.impression.tags.length} 个标签` : '等待生成'}<ChevronDown size={15}/></span>
      </button>
      {profile.directoryProfile && (profile.directoryProfile.base || profile.directoryProfile.department) && <div className="directory-facts">
        {profile.directoryProfile.base && <span>Base · {profile.directoryProfile.base}</span>}
        {profile.directoryProfile.department && <span>部门 · {profile.directoryProfile.department}</span>}
      </div>}
      {profile.impression && <div className="profile-preview"><div className="profile-tags">{profile.impression.tags.map(tag => <em key={tag}>{tag}</em>)}</div><p>{profile.impression.summary}</p></div>}
      {impressionOpen && <div className="profile-detail">
        {profile.impression ? <>
          <h4>{profile.impression.personName} · 互动印象</h4><p>{profile.impression.summary}</p>
          <h4>沟通建议</h4><p>{profile.impression.communicationGuidance}</p>
          {profile.relationshipStyle && <><h4>我与 TA 的表达方式</h4><div className="profile-tags">{profile.relationshipStyle.tags.map(tag => <em key={tag}>{tag}</em>)}</div><p>{profile.relationshipStyle.summary}</p></>}
          {profile.selfStyle && <><h4>我的基础风格</h4><div className="profile-tags">{profile.selfStyle.tags.map(tag => <em key={tag}>{tag}</em>)}</div></>}
          <small>基于最近 {maintenance?.lookbackDays || 30} 天 · {profile.impression.messageCount} 条相关发言 · {profile.impression.conversationCount} 个会话 · 更新于 {displayTime(profile.impression.generatedAt)}</small>
        </> : <div className="agent-empty">{profile.personId ? '基础印象会在首次初始化或下一次每日任务中生成。' : '群聊中会根据被 @ 或当前回复目标展示对应人物印象。'}</div>}
        {profile.personId && <button className="profile-refresh" onClick={() => void refreshProfile()} disabled={profileBusy || !status?.enabled}>{profileBusy ? <RefreshCw className="spin" size={13}/> : <Sparkles size={13}/>} {profileBusy ? '正在生成印象' : profile.impression ? '重新生成印象' : '立即生成印象'}</button>}
      </div>}
    </div>
    <div className={`memory-list collapsible ${memoryOpen ? 'open' : ''}`}>
      <button className="profile-toggle" onClick={() => setMemoryOpen(value => !value)} aria-expanded={memoryOpen}>
        <span><BrainCircuit size={15}/><strong>当前记忆</strong></span><span>{memories.length}<ChevronDown size={15}/></span>
      </button>
      {memoryOpen && <div className="memory-content">
        {memories.slice(0, 12).map(memory => <article key={memory.id}>
          {memory.subjectType === 'person' ? <UserRound size={14}/> : memory.sourceChatType === 'group' ? <Users size={14}/> : <MessageCircle size={14}/>}<div><span>{memory.subjectName || memory.subjectId} · {memory.subjectType === 'person' ? '人物' : memory.sourceChatType === 'group' ? '群组' : '单聊上下文'} · {categoryName[memory.category] || memory.category}</span><p>{memory.content}</p></div>
        </article>)}
        {!memories.length && <div className="agent-empty">点击“提炼记忆”，从当前会话生成带证据的记忆。</div>}
      </div>}
    </div>
  </section>
}
