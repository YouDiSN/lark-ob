import { FormEvent, useCallback, useEffect, useState } from 'react'
import { ArrowLeft, BookOpen, BrainCircuit, ExternalLink, FileText, RefreshCw, Search, Trash2 } from 'lucide-react'
import { api } from '../lib/api'
import type { KnowledgeChatOption, KnowledgeScope, KnowledgeSearchResult, KnowledgeSource, KnowledgeSummary } from './types'
import './knowledge.css'

type Props = { chats: KnowledgeChatOption[]; onBack: () => void }

export function KnowledgePanel({ chats, onBack }: Props) {
  const [sources, setSources] = useState<KnowledgeSource[]>([])
  const [url, setURL] = useState('')
  const [scopeType, setScopeType] = useState<KnowledgeScope>('global')
  const [scopeId, setScopeId] = useState('')
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<KnowledgeSearchResult[]>([])
  const [summaries, setSummaries] = useState<Record<number, KnowledgeSummary>>({})
  const [busy, setBusy] = useState<string>()
  const [error, setError] = useState<string>()

  const load = useCallback(async () => {
    try {
      const nextSources = await api<KnowledgeSource[]>('/api/knowledge/sources')
      setSources(nextSources)
      const loaded = await Promise.all(nextSources.map(async source => {
        const response = await fetch(`/api/knowledge/sources/${source.id}/summary`)
        return response.ok ? await response.json() as KnowledgeSummary : undefined
      }))
      setSummaries(Object.fromEntries(loaded.filter(Boolean).map(summary => [summary!.sourceId, summary!])))
    } catch (err) {
      setError((err as Error).message)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  async function importSource(event: FormEvent) {
    event.preventDefault()
    setBusy('import')
    setError(undefined)
    try {
      await api<KnowledgeSource>('/api/knowledge/sources', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: url.trim(), scopeType, scopeId: scopeType === 'chat' ? scopeId : undefined }),
      })
      setURL('')
      await load()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(undefined)
    }
  }

  async function syncSource(source: KnowledgeSource) {
    setBusy(`sync-${source.id}`)
    setError(undefined)
    try {
      await api(`/api/knowledge/sources/${source.id}/sync`, { method: 'POST' })
      await load()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(undefined)
    }
  }

  async function deleteSource(source: KnowledgeSource) {
    if (!window.confirm(`移除知识源“${source.title || source.url}”？`)) return
    setBusy(`delete-${source.id}`)
    setError(undefined)
    try {
      await api(`/api/knowledge/sources/${source.id}`, { method: 'DELETE' })
      await load()
      setResults(items => items.filter(item => item.sourceId !== source.id))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(undefined)
    }
  }

  async function summarizeSource(source: KnowledgeSource) {
    setBusy(`summary-${source.id}`)
    setError(undefined)
    try {
      const summary = await api<KnowledgeSummary>(`/api/knowledge/sources/${source.id}/summary`, { method: 'POST' })
      setSummaries(current => ({ ...current, [source.id]: summary }))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(undefined)
    }
  }

  async function search(event: FormEvent) {
    event.preventDefault()
    if (!query.trim()) { setResults([]); return }
    setBusy('search')
    setError(undefined)
    try {
      const params = new URLSearchParams({ q: query.trim(), limit: '20' })
      setResults(await api<KnowledgeSearchResult[]>(`/api/knowledge/search?${params}`))
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(undefined)
    }
  }

  return <main className="knowledge-page">
    <header className="knowledge-header">
      <button className="knowledge-back" onClick={onBack}><ArrowLeft size={18}/>返回消息</button>
      <div><span className="knowledge-kicker">LOCAL KNOWLEDGE</span><h1>知识库</h1><p>导入明确授权的飞书文档，保留来源后在本地检索。</p></div>
      <BookOpen size={28}/>
    </header>

    {error && <div className="knowledge-error">{error}<button onClick={() => setError(undefined)}>关闭</button></div>}

    <section className="knowledge-grid">
      <form className="knowledge-card import-card" onSubmit={importSource}>
        <div className="card-heading"><div><span>01</span><h2>导入飞书文档</h2></div><p>支持 Docx 和 Wiki 页面链接；内容仅写入本机 SQLite。</p></div>
        <label>文档链接<input type="url" required value={url} onChange={event => setURL(event.target.value)} placeholder="https://.../docx/... 或 /wiki/..."/></label>
        <div className="scope-row">
          <label>可见范围<select value={scopeType} onChange={event => { setScopeType(event.target.value as KnowledgeScope); if (event.target.value === 'global') setScopeId('') }}><option value="global">全局</option><option value="chat">指定会话</option></select></label>
          {scopeType === 'chat' && <label>关联会话<select required value={scopeId} onChange={event => setScopeId(event.target.value)}><option value="">请选择</option>{chats.map(chat => <option value={chat.id} key={chat.id}>{chat.name}</option>)}</select></label>}
        </div>
        <button className="primary-action" disabled={busy === 'import'}>{busy === 'import' ? <RefreshCw className="spin" size={16}/> : <BookOpen size={16}/>}导入并建立索引</button>
      </form>

      <section className="knowledge-card search-card">
        <div className="card-heading"><div><span>02</span><h2>检索验证</h2></div><p>搜索结果会带回文档、标题与块级来源。</p></div>
        <form className="knowledge-search" onSubmit={search}><Search size={17}/><input value={query} onChange={event => setQuery(event.target.value)} placeholder="输入要查找的概念或事实"/><button disabled={busy === 'search'}>搜索</button></form>
        <div className="knowledge-results">
          {results.map(result => <article key={`${result.sourceId}-${result.chunkId}`}>
            <div><strong>{result.heading || result.title}</strong><span>{result.title}</span></div>
            <p>{result.content}</p>
            <a href={`${result.url}${result.blockId ? `#${result.blockId}` : ''}`} target="_blank" rel="noreferrer">查看原文 <ExternalLink size={13}/></a>
          </article>)}
          {!results.length && <div className="result-empty"><Search size={20}/><span>导入后可在这里验证召回内容</span></div>}
        </div>
      </section>
    </section>

    <section className="knowledge-card source-card">
      <div className="card-heading source-heading"><div><span>03</span><h2>已接入知识源</h2></div><p>{sources.length} 个来源</p></div>
      <div className="source-list">
        {sources.map(source => <article key={source.id}>
          <div className="source-icon"><FileText size={19}/></div>
          <div className="source-copy"><div><strong>{source.title || '未命名文档'}</strong><em className={source.status}>{source.status === 'ready' ? '可检索' : '同步失败'}</em></div><a href={source.url} target="_blank" rel="noreferrer">{source.url}</a><span>{source.chunkCount} 个片段 · {source.scopeType === 'global' ? '全局' : '会话范围'}{source.error ? ` · ${source.error}` : ''}</span>{summaries[source.id] && <div className="source-summary"><strong>AI 摘要</strong><p>{summaries[source.id].summary}</p><ul>{summaries[source.id].facts.slice(0, 5).map((fact, index) => <li key={index}>{fact}</li>)}</ul></div>}</div>
          <div className="source-actions"><button aria-label="生成 AI 摘要" title="生成 AI 摘要" onClick={() => void summarizeSource(source)} disabled={Boolean(busy)}><BrainCircuit size={16} className={busy === `summary-${source.id}` ? 'spin' : ''}/></button><button aria-label="重新同步" title="重新同步" onClick={() => void syncSource(source)} disabled={Boolean(busy)}><RefreshCw size={16} className={busy === `sync-${source.id}` ? 'spin' : ''}/></button><button aria-label="移除" title="移除" onClick={() => void deleteSource(source)} disabled={Boolean(busy)}><Trash2 size={16}/></button></div>
        </article>)}
        {!sources.length && <div className="source-empty"><BookOpen size={24}/><strong>还没有知识源</strong><span>粘贴一篇你有权限的飞书文档开始。</span></div>}
      </div>
    </section>
  </main>
}
