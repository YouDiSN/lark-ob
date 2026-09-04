import { useCallback, useEffect, useMemo, useState } from 'react'
import { ArrowLeft, Check, ChevronDown, ExternalLink, Hash, Inbox, LockKeyhole, MessageCircle, MoreHorizontal, RefreshCw, Search, Settings2, Users, Wifi, X } from 'lucide-react'

type Chat = { id:string; name:string; description?:string; type:'p2p'|'group'; avatar?:string; lastMessage:string; lastTime:number; unread:number; external:boolean }
type Message = { id:string; chatId:string; senderId:string; senderName:string; content:string; type:string; createdAt:number; isSelf:boolean }
type Status = { mode:'lark'|'lark-cli'; connected:boolean; configured:boolean; lastSync?:number; syncing:boolean; error?:string; authMode?:'cli'|'oauth' }
type HistoryState = { hasMore:boolean; pageToken?:string }

const api = async <T,>(path:string, init?:RequestInit):Promise<T> => { const r=await fetch(path,init); if(!r.ok){const e=await r.json().catch(()=>({error:r.statusText}));throw new Error(e.error)} return r.json() }
const initials = (name:string) => name.replace(/\s+/g,'').slice(0,2).toUpperCase()
const colors = ['#5b6ff6','#00a870','#f59e0b','#8b5cf6','#ef6a6a','#0891b2']
const avatarColor = (id:string) => colors[[...id].reduce((n,c)=>n+c.charCodeAt(0),0)%colors.length]
const relativeTime = (ts:number) => { if(!ts)return ''; const d=Date.now()-ts,m=Math.floor(d/60000);if(m<1)return '刚刚';if(m<60)return `${m} 分钟`;const h=Math.floor(m/60);if(h<24)return `${h} 小时`;const date=new Date(ts);return `${date.getMonth()+1}/${date.getDate()}` }
const clock = (ts:number) => new Intl.DateTimeFormat('zh-CN',{hour:'2-digit',minute:'2-digit',hour12:false}).format(ts)

function Avatar({chat,size='normal'}:{chat:Pick<Chat,'id'|'name'|'type'|'avatar'>;size?:'small'|'normal'|'large'}){
  return <div className={`avatar ${size}`} style={{background:chat.avatar?undefined:avatarColor(chat.id)}}>{chat.avatar?<img src={chat.avatar} alt=""/>:chat.type==='group'?<Users size={size==='small'?14:18}/>:initials(chat.name)}</div>
}

export function App(){
  const [chats,setChats]=useState<Chat[]>([]),[messages,setMessages]=useState<Message[]>([]),[status,setStatus]=useState<Status>({mode:'lark',connected:false,configured:false,syncing:false})
  const [active,setActive]=useState(''),[query,setQuery]=useState(''),[filter,setFilter]=useState<'all'|'unread'|'p2p'|'group'>('all'),[loading,setLoading]=useState(true),[mobileChat,setMobileChat]=useState(false),[history,setHistory]=useState<HistoryState>({hasMore:false}),[loadingOlder,setLoadingOlder]=useState(false)
  const load=useCallback(async()=>{try{const [c,s]=await Promise.all([api<Chat[]>('/api/chats'),api<Status>('/api/status')]);setChats(c);setStatus(s);setActive(a=>a||c[0]?.id||'')}catch(e){setStatus(v=>({...v,error:(e as Error).message}))}finally{setLoading(false)}},[])
  useEffect(()=>{load();const es=new EventSource('/api/events');es.addEventListener('update',load);return()=>es.close()},[load])
  const loadMessages=useCallback(async(chatId:string)=>{const [items,state]=await Promise.all([api<Message[]>(`/api/chats/${encodeURIComponent(chatId)}/messages`),api<HistoryState>(`/api/chats/${encodeURIComponent(chatId)}/history`)]);setMessages(items);setHistory(state)},[])
  useEffect(()=>{if(!active)return;loadMessages(active).catch(()=>{setMessages([]);setHistory({hasMore:false})})},[active,status.lastSync,loadMessages])
  const selected=chats.find(c=>c.id===active)
  const visible=useMemo(()=>chats.filter(c=>(filter==='all'||filter==='unread'&&c.unread>0||filter===c.type)&&(`${c.name} ${c.lastMessage}`.toLowerCase().includes(query.toLowerCase()))),[chats,filter,query])
  const sync=async()=>{setStatus(s=>({...s,syncing:true}));try{await api('/api/sync',{method:'POST'});setTimeout(load,500)}catch(e){setStatus(s=>({...s,syncing:false,error:(e as Error).message}))}}
  const loadOlderMessages=async()=>{if(!active||loadingOlder)return;setLoadingOlder(true);try{await api(`/api/chats/${encodeURIComponent(active)}/history`,{method:'POST'});await loadMessages(active)}catch(e){setStatus(s=>({...s,error:(e as Error).message}))}finally{setLoadingOlder(false)}}
  const pick=(id:string)=>{setActive(id);setMobileChat(true)}
  useEffect(()=>{
    const context=(document as Document & {modelContext?:{registerTool:(tool:unknown,options?:{signal?:AbortSignal})=>void|Promise<void>}}).modelContext
    if(!context?.registerTool)return
    const lifecycle=new AbortController()
    void Promise.resolve(context.registerTool({name:'open_lark_conversation',title:'打开 Lark 会话',description:'在消息工作台中打开一个指定的个人或群组会话。',inputSchema:{type:'object',properties:{chatId:{type:'string',description:'会话 ID'}},required:['chatId'],additionalProperties:false},annotations:{readOnlyHint:true,untrustedContentHint:true},execute:(input:unknown)=>{const chatId=(input as {chatId?:unknown})?.chatId;if(typeof chatId!=='string'||!chats.some(c=>c.id===chatId))throw new Error('找不到这个会话');setActive(chatId);setMobileChat(true);return{opened:true,chatId}}},{signal:lifecycle.signal})).catch(()=>{})
    return()=>lifecycle.abort()
  },[chats])

  return <div className="app-shell">
    <aside className={`sidebar ${mobileChat?'mobile-hidden':''}`}>
      <header className="brand"><div className="brand-mark"><MessageCircle size={20}/></div><div><strong>Lark Inbox</strong><span>个人消息工作台</span></div><button className="icon-button" aria-label="设置"><Settings2 size={19}/></button></header>
      <div className="search"><Search size={17}/><input value={query} onChange={e=>setQuery(e.target.value)} placeholder="搜索会话和消息" aria-label="搜索会话"/>{query&&<button onClick={()=>setQuery('')} aria-label="清空"><X size={15}/></button>}</div>
      <nav className="filters" aria-label="会话筛选">
        {([['all','全部'],['unread','未读'],['p2p','单聊'],['group','群聊']] as const).map(([key,label])=><button key={key} className={filter===key?'active':''} onClick={()=>setFilter(key)}>{label}{key==='unread'&&chats.reduce((n,c)=>n+c.unread,0)>0&&<em>{chats.reduce((n,c)=>n+c.unread,0)}</em>}</button>)}
      </nav>
      <div className="conversation-list">
        {loading?Array.from({length:5}).map((_,i)=><div className="skeleton-row" key={i}><i/><span/></div>):visible.length?visible.map(c=><button className={`conversation ${c.id===active?'selected':''}`} key={c.id} onClick={()=>pick(c.id)}>
          <Avatar chat={c}/><div className="conversation-copy"><div><strong>{c.name}</strong><time>{relativeTime(c.lastTime)}</time></div><p>{c.lastMessage||'暂无消息'}</p></div>{c.unread>0&&<b className="unread">{c.unread}</b>}
        </button>):<div className="empty-list"><Inbox/><strong>没有匹配的会话</strong><span>换个关键词或筛选条件试试</span></div>}
      </div>
      <footer className="sidebar-footer"><StatusBadge status={status}/><button className="sync-button" onClick={sync} disabled={status.syncing||!status.connected}><RefreshCw size={15} className={status.syncing?'spin':''}/>{status.syncing?'同步中':'同步'}</button></footer>
    </aside>
    <main className={`chat-panel ${mobileChat?'mobile-visible':''}`}>
      {selected?<>
        <header className="chat-header"><button className="icon-button mobile-back" onClick={()=>setMobileChat(false)} aria-label="返回会话列表"><ArrowLeft/></button><Avatar chat={selected} size="small"/><div><strong>{selected.name}</strong><span>{selected.type==='group'?'群聊':'单聊'}{selected.external?' · 外部':''}</span></div><div className="header-actions"><button className="icon-button" aria-label="更多"><MoreHorizontal/></button></div></header>
        {!status.connected&&<AuthBanner configured={status.configured} authMode={status.authMode}/>} {status.error&&<div className="error-banner"><span>{status.error}</span><button onClick={()=>setStatus(s=>({...s,error:undefined}))}><X size={15}/></button></div>}
        <section className="message-scroll" aria-live="polite">
          {history.hasMore&&<button className="load-older" onClick={loadOlderMessages} disabled={loadingOlder}><RefreshCw size={14} className={loadingOlder?'spin':''}/>{loadingOlder?'正在加载':'加载更早消息'}</button>}
          <div className="day-divider"><span>今天</span></div>
          {messages.map((m,i)=><MessageBubble key={m.id} message={m} compact={i>0&&messages[i-1].senderId===m.senderId&&m.createdAt-messages[i-1].createdAt<5*60*1000}/>) }
          {!messages.length&&!loading&&<div className="empty-chat"><MessageCircle/><strong>暂无可显示的消息</strong><span>同步后，聊天记录会出现在这里。</span></div>}
        </section>
        <footer className="read-only"><LockKeyhole size={16}/><span>当前为只读版本，发送消息将在下一阶段开放</span></footer>
      </>:<div className="empty-chat whole"><MessageCircle/><strong>选择一个会话</strong><span>查看个人单聊和群聊记录</span></div>}
    </main>
    <aside className="details-panel">
      {selected&&<><div className="detail-title">会话信息<button className="icon-button"><ChevronDown size={17}/></button></div><div className="identity"><Avatar chat={selected} size="large"/><strong>{selected.name}</strong><span>{selected.description||`${selected.type==='group'?'群组会话':'个人会话'} · ${messages.length} 条已加载消息`}</span></div><dl><div><dt>会话类型</dt><dd>{selected.type==='group'?<><Hash size={15}/>群聊</>:<><MessageCircle size={15}/>单聊</>}</dd></div><div><dt>数据来源</dt><dd><Check size={15}/>本地数据库</dd></div><div><dt>同步方式</dt><dd><RefreshCw size={15}/>增量轮询</dd></div></dl><div className="privacy-note"><LockKeyhole size={17}/><div><strong>数据留在本机</strong><p>消息保存在本地 SQLite，不会自动对外发送。</p></div></div></>}
    </aside>
  </div>
}

function StatusBadge({status}:{status:Status}){const ok=status.connected;return <div className={`status ${ok?'ok':''}`}><span>{ok?<Wifi size={15}/>:<span className="status-dot"/>}</span><div><strong>{status.connected?'Lark 已连接':'等待连接'}</strong><small>{status.lastSync?`${relativeTime(status.lastSync)}前同步`:'需要用户授权'}</small></div></div>}
function AuthBanner({configured,authMode}:{configured:boolean;authMode?:'cli'|'oauth'}){const cli=authMode==='cli';return <div className="auth-banner"><div><span>AUTH</span><p>Lark 尚未授权。{configured?'连接后即可同步真实会话。':'配置应用凭证后即可连接真实 Lark 会话。'}</p></div>{cli?<code>lark-cli auth login</code>:configured?<a href="/auth/lark">连接 Lark <ExternalLink size={14}/></a>:<code>.env.example</code>}</div>}
function MessageBubble({message:m,compact}:{message:Message;compact:boolean}){return <article className={`message ${m.isSelf?'self':''} ${compact?'compact':''}`}>{!m.isSelf&&!compact?<div className="sender-avatar" style={{background:avatarColor(m.senderId)}}>{initials(m.senderName)}</div>:<div className="sender-spacer"/>}<div className="message-body">{!m.isSelf&&!compact&&<span className="sender-name">{m.senderName}</span>}<div className="bubble"><p>{m.content}</p><time>{clock(m.createdAt)}</time></div></div></article>}
