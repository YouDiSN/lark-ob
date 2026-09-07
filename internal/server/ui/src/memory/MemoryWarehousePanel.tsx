import { useCallback, useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { ArrowLeft, BrainCircuit, CalendarClock, Clock3, Database, MessageCircle, MessageSquareText, Pencil, Pin, Plus, Search, Trash2, UserRound, Users, X } from 'lucide-react'
import { api } from '../lib/api'
import type { ManualMemoryInput, MemoryClaim, MemorySubjectOptions, MemoryWarehouse } from './types'
import './memory.css'

type Props = { onBack: () => void }
type SubjectFilter = '' | 'person' | 'group' | 'p2p'

const categoryName: Record<string, string> = {
  preference: '偏好', fact: '事实', commitment: '承诺', relationship: '关系', project_context: '项目上下文', temporary: '临时信息', constraint: '约束',
}
const importanceName = { normal: '普通', important: '重要', constraint: '硬约束' }
const statusName = { active: '生效中', scheduled: '待生效', expired: '已过期' }
const emptyManual = (): ManualMemoryInput => ({ subjectType: 'person', subjectId: '', subjectName: '', chatId: '', category: 'fact', content: '', importance: 'normal', validFrom: Date.now(), validUntil: 0, pinned: false })
const toLocalInput = (timestamp?: number) => timestamp ? new Date(timestamp - new Date(timestamp).getTimezoneOffset() * 60_000).toISOString().slice(0, 16) : ''
const fromLocalInput = (value: string) => value ? new Date(value).getTime() : 0

const formatDate = (timestamp: number) => timestamp
  ? new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(timestamp)
  : '未知'

export function MemoryWarehousePanel({ onBack }: Props) {
  const [warehouse, setWarehouse] = useState<MemoryWarehouse>()
  const [subjectType, setSubjectType] = useState<SubjectFilter>('')
  const [query, setQuery] = useState('')
  const [appliedQuery, setAppliedQuery] = useState('')
  const [deleting, setDeleting] = useState<number>()
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState<string>()
	const [subjects, setSubjects] = useState<MemorySubjectOptions>({ people: [], chats: [] })
	const [manual, setManual] = useState<ManualMemoryInput>(emptyManual)
	const [editing, setEditing] = useState<MemoryClaim>()
	const [editorOpen, setEditorOpen] = useState(false)
	const [saving, setSaving] = useState(false)
  const requestSequence = useRef(0)
  const warehouseRef = useRef<MemoryWarehouse | undefined>(undefined)

  const load = useCallback(async (silent = false) => {
    const sequence = ++requestSequence.current
    if (!silent) setLoading(true)
    const visibleLimit = silent ? Math.max(100, warehouseRef.current?.items.length ?? 0) : 100
    const params = new URLSearchParams({ limit: String(visibleLimit), offset: '0' })
    if (subjectType === 'person') params.set('subjectType', 'person')
    if (subjectType === 'group' || subjectType === 'p2p') {
      params.set('subjectType', 'chat')
      params.set('sourceChatType', subjectType)
    }
    if (appliedQuery) params.set('q', appliedQuery)
    try {
      const result = await api<MemoryWarehouse>(`/api/memories?${params}`)
      if (sequence === requestSequence.current) {
        warehouseRef.current = result
        setWarehouse(result)
      }
    } finally {
      if (sequence === requestSequence.current) setLoading(false)
    }
  }, [subjectType, appliedQuery])

  useEffect(() => { void load().catch(err => setError((err as Error).message)) }, [load])
	useEffect(() => { void api<MemorySubjectOptions>('/api/memories/subjects').then(setSubjects).catch(err => setError((err as Error).message)) }, [])
  useEffect(() => {
    const events = new EventSource('/api/events')
    events.addEventListener('update', () => void load(true).catch(() => {}))
    return () => events.close()
  }, [load])

  async function remove(claim: MemoryClaim) {
    if (!window.confirm(`确定删除“${claim.content}”这条记忆吗？`)) return
    setDeleting(claim.id); setError(undefined)
    try {
      await api(`/api/memories/${claim.id}`, { method: 'DELETE' })
      await load()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setDeleting(undefined)
    }
  }

	function openCreate() {
		setEditing(undefined); setManual(emptyManual()); setEditorOpen(true); setError(undefined)
	}

	function openEdit(claim: MemoryClaim) {
		if (claim.sourceType !== 'manual') return
		setEditing(claim)
		setManual({ subjectType: claim.subjectType, subjectId: claim.subjectId, subjectName: claim.subjectName || '', chatId: claim.chatId,
			category: claim.category, content: claim.content, importance: claim.importance, validFrom: claim.validFrom || 0,
			validUntil: claim.validUntil || 0, pinned: claim.pinned })
		setEditorOpen(true); setError(undefined)
	}

	async function saveManual(event: FormEvent) {
		event.preventDefault(); setSaving(true); setError(undefined)
		try {
			const selected = (manual.subjectType === 'person' ? subjects.people : subjects.chats).find(item => item.id === manual.subjectId)
			if (!selected) throw new Error('请选择记忆对象')
			const body = { ...manual, subjectName: selected.name, chatId: selected.chatId }
			await api(editing ? `/api/memories/${editing.id}` : '/api/memories', { method: editing ? 'PUT' : 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
			setEditorOpen(false); setEditing(undefined); await load()
		} catch (err) {
			setError((err as Error).message)
		} finally { setSaving(false) }
	}

  function selectSubjectType(next: SubjectFilter) {
    if (next === subjectType) return
    requestSequence.current++
    warehouseRef.current = undefined
    setLoading(true)
    setSubjectType(next)
  }

  function applySearch(next: string) {
    requestSequence.current++
    warehouseRef.current = undefined
    setLoading(true)
    if (next === appliedQuery) {
      void load().catch(err => setError((err as Error).message))
      return
    }
    setAppliedQuery(next)
  }

  async function loadMore() {
    if (!warehouse?.hasMore || loadingMore) return
    const sequence = ++requestSequence.current
    setLoadingMore(true); setError(undefined)
    const params = new URLSearchParams({ limit: '100', offset: String(warehouse.nextOffset) })
    if (subjectType === 'person') params.set('subjectType', 'person')
    if (subjectType === 'group' || subjectType === 'p2p') {
      params.set('subjectType', 'chat')
      params.set('sourceChatType', subjectType)
    }
    if (appliedQuery) params.set('q', appliedQuery)
    try {
      const result = await api<MemoryWarehouse>(`/api/memories?${params}`)
      if (sequence === requestSequence.current) {
        const current = warehouseRef.current
        const merged = current ? { ...result, items: [...current.items, ...result.items] } : result
        warehouseRef.current = merged
        setWarehouse(merged)
      }
    } catch (err) {
      if (sequence === requestSequence.current) setError((err as Error).message)
    } finally {
      if (sequence === requestSequence.current) setLoadingMore(false)
    }
  }

  const stats = warehouse?.stats
  return <main className="memory-page">
    <header className="memory-header">
      <button className="memory-back" onClick={onBack}><ArrowLeft size={17}/>返回消息</button>
		<div><span className="memory-kicker">LOCAL MEMORY STORE</span><h1>记忆仓库</h1><p>查看 Agent 提炼的记忆，也可以手动加入明确事实、临时信息和回复约束。</p></div>
      <BrainCircuit size={34}/>
    </header>

    {error && <div className="memory-error"><span>{error}</span><button onClick={() => setError(undefined)} aria-label="关闭"><X size={15}/></button></div>}

    <section className="memory-stats" aria-label="记忆统计">
      <article><Database/><div><strong>{stats?.total ?? '—'}</strong><span>记忆总数</span></div></article>
      <article><UserRound/><div><strong>{stats?.personMemories ?? '—'}</strong><span>{stats?.people ?? 0} 人的人物记忆</span></div></article>
      <article><Users/><div><strong>{stats?.groupMemories ?? '—'}</strong><span>{stats?.groups ?? 0} 个群组</span></div></article>
      <article><MessageCircle/><div><strong>{stats?.p2pMemories ?? '—'}</strong><span>{stats?.p2pChats ?? 0} 个单聊上下文</span></div></article>
      <article><Clock3/><div><strong>{warehouse?.decayHalfLifeDays ?? 90} 天</strong><span>有效评分半衰期</span></div></article>
    </section>

    <section className="memory-repository">
      <div className="memory-toolbar">
        <div><h2>全部记忆</h2><p>按当前有效评分从高到低排列{warehouse ? ` · 已显示 ${warehouse.items.length}/${warehouse.totalMatches}` : ''}</p></div>
        <div className="memory-controls">
			<button className="memory-create" onClick={openCreate}><Plus size={15}/>新增记忆</button>
          <nav aria-label="记忆类型筛选">
            {([['', '全部'], ['person', '人物'], ['group', '群组'], ['p2p', '单聊上下文']] as const).map(([value, label]) => <button key={value || 'all'} className={subjectType === value ? 'active' : ''} onClick={() => selectSubjectType(value)}>{label}</button>)}
          </nav>
          <form onSubmit={event => { event.preventDefault(); applySearch(query.trim()) }}>
            <Search size={15}/><input value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索人物、群组或内容" aria-label="搜索记忆"/>
            {query && <button type="button" onClick={() => { setQuery(''); applySearch('') }} aria-label="清空搜索"><X size={14}/></button>}
          </form>
        </div>
      </div>

      <div className="memory-list-table">
        {!loading && warehouse?.items.map(claim => <article key={claim.id}>
          <div className={`memory-subject ${claim.subjectType === 'person' ? 'person' : claim.sourceChatType}`} aria-hidden="true">{claim.subjectType === 'person' ? <UserRound/> : claim.sourceChatType === 'group' ? <Users/> : <MessageCircle/>}</div>
          <div className="memory-record">
			<div><strong>{claim.subjectName || claim.subjectId}</strong><span>{claim.subjectType === 'person' ? '人物' : claim.sourceChatType === 'group' ? '群组' : '单聊上下文'}</span><span>{categoryName[claim.category] || claim.category}</span><span className={`memory-source ${claim.sourceType}`}>{claim.sourceType === 'manual' ? '手动' : 'Agent'}</span><span className={`memory-state ${claim.status}`}>{statusName[claim.status]}</span>{claim.pinned && <Pin size={11}/>}</div>
            <p>{claim.content}</p>
			<small>{claim.sourceType === 'manual' ? <CalendarClock size={12}/> : <MessageSquareText size={12}/>} {claim.sourceType === 'manual' ? `${importanceName[claim.importance]} · ${claim.validFrom ? formatDate(claim.validFrom) : '立即'}生效${claim.validUntil ? ` · ${formatDate(claim.validUntil)}失效` : ' · 长期有效'}` : `${claim.sourceChatName || '未知会话'} · 最后证据 ${formatDate(claim.lastEvidenceAt || claim.updatedAt)} · ${claim.evidenceMessageIds.length} 条证据`}</small>
          </div>
          <div className="memory-score" title={`原始置信度 ${Math.round(claim.confidence * 100)}%`}><strong>{Math.round(claim.effectiveScore * 100)}</strong><span>有效评分</span><meter min="0" max="1" value={claim.effectiveScore}/></div>
			<div className="memory-row-actions">{claim.sourceType === 'manual' && <button onClick={() => openEdit(claim)} aria-label="编辑记忆" title="编辑记忆"><Pencil size={15}/></button>}<button onClick={() => void remove(claim)} disabled={deleting === claim.id} aria-label="删除记忆" title="删除记忆"><Trash2 size={16}/></button></div>
        </article>)}
        {!loading && warehouse && !warehouse.items.length && <div className="memory-empty"><BrainCircuit size={25}/><strong>没有匹配的记忆</strong><span>首次初始化生成的记忆会自动出现在这里。</span></div>}
        {loading && !error && <div className="memory-empty"><Database size={25}/><strong>正在读取本地记忆</strong></div>}
        {!loading && warehouse?.hasMore && <button className="memory-load-more" onClick={() => void loadMore()} disabled={loadingMore}>{loadingMore ? '正在加载…' : `继续加载（剩余 ${warehouse.totalMatches - warehouse.items.length} 条）`}</button>}
      </div>
    </section>

	{editorOpen && <div className="memory-modal-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) setEditorOpen(false) }}>
		<form className="memory-editor" onSubmit={event => void saveManual(event)}>
			<header><div><span className="memory-kicker">MANUAL MEMORY</span><h2>{editing ? '编辑手动记忆' : '新增手动记忆'}</h2></div><button type="button" onClick={() => setEditorOpen(false)} aria-label="关闭"><X size={18}/></button></header>
			<label><span>对象类型</span><select value={manual.subjectType} onChange={event => setManual(value => ({ ...value, subjectType: event.target.value as 'person'|'chat', subjectId: '', chatId: '' }))}><option value="person">人物（跨会话生效）</option><option value="chat">群组 / 会话</option></select></label>
			<label><span>记忆对象</span><select required value={manual.subjectId} onChange={event => { const list = manual.subjectType === 'person' ? subjects.people : subjects.chats; const selected = list.find(item => item.id === event.target.value); setManual(value => ({ ...value, subjectId: selected?.id || '', subjectName: selected?.name || '', chatId: selected?.chatId || '' })) }}><option value="">请选择</option>{(manual.subjectType === 'person' ? subjects.people : subjects.chats).map(item => <option key={item.id} value={item.id}>{item.name}{manual.subjectType === 'person' && item.chatName ? ` · 最近在 ${item.chatName}` : item.chatType === 'group' ? ' · 群聊' : ' · 单聊'}</option>)}</select></label>
			<div className="memory-editor-grid"><label><span>分类</span><select value={manual.category} onChange={event => setManual(value => ({ ...value, category: event.target.value }))}>{Object.entries(categoryName).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label><label><span>重要程度</span><select value={manual.importance} onChange={event => setManual(value => ({ ...value, importance: event.target.value as ManualMemoryInput['importance'] }))}><option value="normal">普通</option><option value="important">重要</option><option value="constraint">硬约束</option></select></label></div>
			<label><span>记忆内容</span><textarea required maxLength={500} rows={4} value={manual.content} onChange={event => setManual(value => ({ ...value, content: event.target.value }))} placeholder="例如：费阳目前在重庆，除非明确约好见面，不要建议让她带食物给我。"/><small>{manual.content.length}/500</small></label>
			<div className="memory-editor-grid"><label><span>生效时间</span><input type="datetime-local" value={toLocalInput(manual.validFrom)} onChange={event => setManual(value => ({ ...value, validFrom: fromLocalInput(event.target.value) }))}/></label><label><span>失效时间（可选）</span><input type="datetime-local" value={toLocalInput(manual.validUntil)} onChange={event => setManual(value => ({ ...value, validUntil: fromLocalInput(event.target.value) }))}/></label></div>
			<label className="memory-pin"><input type="checkbox" checked={manual.pinned} onChange={event => setManual(value => ({ ...value, pinned: event.target.checked }))}/><span><strong>置顶记忆</strong><small>长期稳定事实可置顶；临时信息建议设置失效时间。</small></span></label>
			<footer><button type="button" onClick={() => setEditorOpen(false)}>取消</button><button className="primary" disabled={saving}>{saving ? '正在保存…' : editing ? '保存修改' : '新增记忆'}</button></footer>
		</form>
	</div>}
  </main>
}
