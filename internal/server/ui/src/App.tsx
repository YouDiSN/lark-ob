import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Archive, ArchiveRestore, ArrowLeft, AtSign, Bell, BellOff, BookOpen, BrainCircuit, Check, CheckCheck, ChevronDown, ExternalLink, Hash, Image as ImageIcon, Inbox, MessageCircle, RefreshCw, Search, Users, Wifi, X } from 'lucide-react'
import { KnowledgePanel } from './knowledge/KnowledgePanel'
import { MemoryWarehousePanel } from './memory/MemoryWarehousePanel'
import { AgentPanel } from './agent/AgentPanel'
import { ReplyComposer } from './agent/ReplyComposer'
import { api } from './lib/api'
import { imageKeys, latestPendingMention, messagePreview, messageText } from './inbox/types'
import type { Chat, HistoryState, Message, Status } from './inbox/types'

type View = 'inbox'|'knowledge'|'memory'
const viewFromHash = ():View => window.location.hash==='#knowledge'?'knowledge':window.location.hash==='#memory'?'memory':'inbox'

const initials = (name:string) => name.replace(/\s+/g,'').slice(0,2).toUpperCase()
const colors = ['#5b6ff6','#00a870','#f59e0b','#8b5cf6','#ef6a6a','#0891b2']
const avatarColor = (id:string) => colors[[...id].reduce((n,c)=>n+c.charCodeAt(0),0)%colors.length]
const relativeTime = (ts:number) => { if(!ts)return ''; const d=Date.now()-ts,m=Math.floor(d/60000);if(m<1)return '刚刚';if(m<60)return `${m} 分钟`;const h=Math.floor(m/60);if(h<24)return `${h} 小时`;const date=new Date(ts);return `${date.getMonth()+1}/${date.getDate()}` }
const clock = (ts:number) => ts ? new Intl.DateTimeFormat('zh-CN',{hour:'2-digit',minute:'2-digit',hour12:false}).format(ts) : ''
const conversationTime = (ts:number) => { if(!ts)return '';const now=new Date(),date=new Date(ts),start=new Date(now.getFullYear(),now.getMonth(),now.getDate()).getTime(),day=new Date(date.getFullYear(),date.getMonth(),date.getDate()).getTime();if(day===start)return clock(ts);if(day===start-86400000)return '昨天';if(date.getFullYear()===now.getFullYear())return `${date.getMonth()+1}/${date.getDate()}`;return `${date.getFullYear()}/${date.getMonth()+1}/${date.getDate()}` }

function Avatar({chat,size='normal'}:{chat:Pick<Chat,'id'|'name'|'type'|'avatar'>;size?:'small'|'normal'|'large'}){
  return <div className={`avatar ${size}`} style={{background:avatarColor(chat.id)}}>{chat.type==='group'?<Users size={size==='small'?14:18}/>:initials(chat.name)}<AvatarImage src={chat.avatar}/></div>
}

function AvatarImage({src}:{src?:string}){const [failed,setFailed]=useState(false);useEffect(()=>setFailed(false),[src]);return src&&!failed?<img src={src} alt="" onError={()=>setFailed(true)}/>:null}

export function App(){
  const [chats,setChats]=useState<Chat[]>([]),[messages,setMessages]=useState<Message[]>([]),[status,setStatus]=useState<Status>({mode:'lark',connected:false,configured:false,syncing:false})
  const [active,setActive]=useState(''),[query,setQuery]=useState(''),[filter,setFilter]=useState<'all'|'new'|'mention'|'p2p'|'group'|'box'>('all'),[loading,setLoading]=useState(true),[mobileChat,setMobileChat]=useState(false),[history,setHistory]=useState<HistoryState>({hasMore:false}),[loadingOlder,setLoadingOlder]=useState(false),[refreshingChat,setRefreshingChat]=useState(false),[markingAllRead,setMarkingAllRead]=useState(false),[savingPreference,setSavingPreference]=useState(false)
  const [view,setView]=useState<View>(viewFromHash)
  const messageScrollRef=useRef<HTMLElement>(null),stickToBottomRef=useRef(true),forceBottomRef=useRef(true),activeRef=useRef(''),restoreScrollRef=useRef<{height:number;top:number}|undefined>(undefined)
  const load=useCallback(async()=>{try{const [c,s]=await Promise.all([api<Chat[]>('/api/chats'),api<Status>('/api/status')]);setChats(c);setStatus(s);setActive(a=>a||c[0]?.id||'')}catch(e){setStatus(v=>({...v,error:(e as Error).message}))}finally{setLoading(false)}},[])
  useEffect(()=>{load();const es=new EventSource('/api/events');es.addEventListener('update',load);return()=>es.close()},[load])
  useEffect(()=>{const update=()=>setView(viewFromHash());window.addEventListener('hashchange',update);return()=>window.removeEventListener('hashchange',update)},[])
  const loadMessages=useCallback(async(chatId:string)=>{const [items,state]=await Promise.all([api<Message[]>(`/api/chats/${encodeURIComponent(chatId)}/messages`),api<HistoryState>(`/api/chats/${encodeURIComponent(chatId)}/history`)]);setMessages(items);setHistory(state)},[])
  useEffect(()=>{if(!active)return;if(activeRef.current!==active){activeRef.current=active;forceBottomRef.current=true;stickToBottomRef.current=true;restoreScrollRef.current=undefined;setMessages([])}loadMessages(active).catch(()=>{setMessages([]);setHistory({hasMore:false})})},[active,status.lastSync,loadMessages])
  useLayoutEffect(()=>{const viewport=messageScrollRef.current;if(!viewport)return;const restore=restoreScrollRef.current;if(restore){viewport.scrollTop=viewport.scrollHeight-restore.height+restore.top;restoreScrollRef.current=undefined;return}if(forceBottomRef.current||stickToBottomRef.current){viewport.scrollTop=viewport.scrollHeight;forceBottomRef.current=false}},[active,messages])
  const selected=chats.find(c=>c.id===active)
  const replyTarget=useMemo(()=>latestPendingMention(messages),[messages])
  const visible=useMemo(()=>chats.filter(c=>((filter==='box'?c.inMessageBox:!c.inMessageBox)&&(filter==='box'||filter==='all'||filter==='new'&&c.newMessages>0||filter==='mention'&&c.mentionCount>0||filter===c.type))&&(`${c.name} ${c.lastMessage}`.toLowerCase().includes(query.toLowerCase()))),[chats,filter,query])
  const sync=async()=>{setStatus(s=>({...s,syncing:true}));try{await api('/api/sync',{method:'POST'});setTimeout(load,500)}catch(e){setStatus(s=>({...s,syncing:false,error:(e as Error).message}))}}
  const loadOlderMessages=async()=>{if(!active||loadingOlder)return;const viewport=messageScrollRef.current;if(viewport)restoreScrollRef.current={height:viewport.scrollHeight,top:viewport.scrollTop};setLoadingOlder(true);try{await api(`/api/chats/${encodeURIComponent(active)}/history`,{method:'POST'});await loadMessages(active)}catch(e){restoreScrollRef.current=undefined;setStatus(s=>({...s,error:(e as Error).message}))}finally{setLoadingOlder(false)}}
  const markViewed=useCallback(async(chatId:string)=>{try{await api(`/api/chats/${encodeURIComponent(chatId)}/viewed`,{method:'POST'});setChats(current=>current.map(chat=>chat.id===chatId?{...chat,newMessages:0,mentionCount:0}:chat))}catch(e){setStatus(s=>({...s,error:(e as Error).message}))}},[])
  const markAllRead=async()=>{if(markingAllRead||!chats.some(chat=>chat.newMessages>0||chat.mentionCount>0))return;setMarkingAllRead(true);try{await api('/api/chats/read-all',{method:'POST'});setChats(current=>current.map(chat=>({...chat,newMessages:0,mentionCount:0})))}catch(e){setStatus(s=>({...s,error:(e as Error).message}))}finally{setMarkingAllRead(false)}}
  const updatePreference=async(change:Partial<Pick<Chat,'muted'|'inMessageBox'>>)=>{if(!selected||selected.type!=='group'||savingPreference)return;setSavingPreference(true);try{const updated=await api<Chat>(`/api/chats/${encodeURIComponent(selected.id)}/preferences`,{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify(change)});setChats(current=>current.map(chat=>chat.id===updated.id?updated:chat))}catch(e){setStatus(s=>({...s,error:(e as Error).message}))}finally{setSavingPreference(false)}}
  const refreshActive=async()=>{if(!active||refreshingChat)return;setRefreshingChat(true);setStatus(s=>({...s,error:undefined}));try{await api(`/api/chats/${encodeURIComponent(active)}/refresh`,{method:'POST'});await loadMessages(active);await markViewed(active);await load()}catch(e){setStatus(s=>({...s,error:(e as Error).message}))}finally{setRefreshingChat(false)}}
  const trackMessageScroll=()=>{const viewport=messageScrollRef.current;if(viewport)stickToBottomRef.current=viewport.scrollHeight-viewport.scrollTop-viewport.clientHeight<80}
  const pick=(id:string)=>{setActive(id);setMobileChat(true);void markViewed(id)}
  const focusReplyTarget=()=>{if(!replyTarget)return;document.getElementById(`message-${replyTarget.id}`)?.scrollIntoView({behavior:'smooth',block:'center'})}
  const keepComposerAnchored=()=>{window.requestAnimationFrame(()=>{const viewport=messageScrollRef.current;if(viewport&&stickToBottomRef.current)viewport.scrollTop=viewport.scrollHeight})}
  const openView=(next:View)=>{setView(next);window.location.hash=next==='inbox'?'':next}
  useEffect(()=>{
    const context=(document as Document & {modelContext?:{registerTool:(tool:unknown,options?:{signal?:AbortSignal})=>void|Promise<void>}}).modelContext
    if(!context?.registerTool)return
    const lifecycle=new AbortController()
    void Promise.resolve(context.registerTool({name:'open_lark_conversation',title:'打开 Lark 会话',description:'在消息工作台中打开一个指定的个人或群组会话。',inputSchema:{type:'object',properties:{chatId:{type:'string',description:'会话 ID'}},required:['chatId'],additionalProperties:false},annotations:{readOnlyHint:true,untrustedContentHint:true},execute:(input:unknown)=>{const chatId=(input as {chatId?:unknown})?.chatId;if(typeof chatId!=='string'||!chats.some(c=>c.id===chatId))throw new Error('找不到这个会话');setActive(chatId);setMobileChat(true);return{opened:true,chatId}}},{signal:lifecycle.signal})).catch(()=>{})
    return()=>lifecycle.abort()
  },[chats])

  if(view==='knowledge')return <KnowledgePanel chats={chats.map(({id,name})=>({id,name}))} onBack={()=>openView('inbox')}/>
  if(view==='memory')return <MemoryWarehousePanel onBack={()=>openView('inbox')}/>

  return <div className="app-shell">
    <aside className={`sidebar ${mobileChat?'mobile-hidden':''}`}>
      <header className="brand"><div className="brand-mark"><MessageCircle size={20}/></div><div><strong>Lark Inbox</strong><span>个人消息工作台</span></div><div className="brand-actions"><button className="icon-button" aria-label="打开知识库" title="知识库" onClick={()=>openView('knowledge')}><BookOpen size={18}/></button><button className="icon-button" aria-label="打开记忆仓库" title="记忆仓库" onClick={()=>openView('memory')}><BrainCircuit size={18}/></button></div></header>
      <div className="search"><Search size={17}/><input value={query} onChange={e=>setQuery(e.target.value)} placeholder="搜索会话和消息" aria-label="搜索会话"/>{query&&<button onClick={()=>setQuery('')} aria-label="清空"><X size={15}/></button>}</div>
      <div className="list-tools"><span>会话</span><button onClick={()=>void markAllRead()} disabled={markingAllRead||!chats.some(chat=>chat.newMessages>0||chat.mentionCount>0)}><CheckCheck size={14}/>{markingAllRead?'处理中':'全部已读'}</button></div>
      <nav className="filters" aria-label="会话筛选">
        {([['all','全部'],['new','新消息'],['mention','@我'],['p2p','单聊'],['group','群聊'],['box','消息盒子']] as const).map(([key,label])=><button key={key} className={filter===key?'active':''} onClick={()=>setFilter(key)}>{label}{key==='new'&&chats.filter(c=>!c.inMessageBox&&c.newMessages>0).length>0&&<em>{chats.filter(c=>!c.inMessageBox&&c.newMessages>0).length}</em>}{key==='mention'&&chats.filter(c=>!c.inMessageBox&&c.mentionCount>0).length>0&&<em className="mention-count">{chats.filter(c=>!c.inMessageBox&&c.mentionCount>0).length}</em>}{key==='box'&&chats.filter(c=>c.inMessageBox).length>0&&<em className="box-count">{chats.filter(c=>c.inMessageBox).length}</em>}</button>)}
      </nav>
      <div className="conversation-list">
        {loading?Array.from({length:5}).map((_,i)=><div className="skeleton-row" key={i}><i/><span/></div>):visible.length?visible.map(c=><button className={`conversation ${c.id===active?'selected':''} ${c.newMessages>0?'has-new':''}`} key={c.id} onClick={()=>pick(c.id)}>
          <Avatar chat={c}/><div className="conversation-copy"><div><strong>{c.name}</strong><time>{conversationTime(c.lastTime)}</time></div><p>{messagePreview(c.lastMessage)||'暂无消息'}</p></div><div className="activity-badges">{c.muted&&<b className="state-badge" title="已屏蔽"><BellOff size={12}/></b>}{c.inMessageBox&&<b className="state-badge" title="消息盒子"><Archive size={12}/></b>}{c.mentionCount>0&&<b className="mention-badge"><AtSign size={11}/>我{c.mentionCount>1?` ${c.mentionCount}`:''}</b>}{c.newMessages>0&&<b className={`unread ${c.muted?'muted-unread':''}`}>{c.newMessages>99?'99+':c.newMessages}</b>}</div>
        </button>):<div className="empty-list"><Inbox/><strong>没有匹配的会话</strong><span>换个关键词或筛选条件试试</span></div>}
      </div>
      <footer className="sidebar-footer"><StatusBadge status={status}/><button className="sync-button" onClick={sync} disabled={status.syncing||!status.connected}><RefreshCw size={15} className={status.syncing?'spin':''}/>{status.syncing?'同步中':'同步'}</button></footer>
    </aside>
    <main className={`chat-panel ${mobileChat?'mobile-visible':''}`}>
      {selected?<>
        <header className="chat-header"><button className="icon-button mobile-back" onClick={()=>setMobileChat(false)} aria-label="返回会话列表"><ArrowLeft/></button><Avatar chat={selected} size="small"/><div><strong>{selected.name}</strong><span>{selected.type==='group'?'群聊':'单聊'}{selected.external?' · 外部':''}{selected.muted?' · 已屏蔽':''}{selected.inMessageBox?' · 消息盒子':''}</span></div><div className="header-actions">{selected.type==='group'&&<><button className={`chat-state-action ${selected.muted?'active':''}`} onClick={()=>void updatePreference({muted:!selected.muted})} disabled={savingPreference} title={selected.muted?'取消屏蔽':'屏蔽消息'}>{selected.muted?<Bell size={15}/>:<BellOff size={15}/>}<span>{selected.muted?'取消屏蔽':'屏蔽'}</span></button><button className={`chat-state-action ${selected.inMessageBox?'active':''}`} onClick={()=>void updatePreference({inMessageBox:!selected.inMessageBox})} disabled={savingPreference} title={selected.inMessageBox?'移回会话列表':'移到消息盒子'}>{selected.inMessageBox?<ArchiveRestore size={15}/>:<Archive size={15}/>}<span>{selected.inMessageBox?'移回列表':'消息盒子'}</span></button></>}<button className="chat-refresh" onClick={()=>void refreshActive()} disabled={refreshingChat||!status.connected} aria-label="刷新当前对话"><RefreshCw size={15} className={refreshingChat?'spin':''}/>{refreshingChat?'刷新中':'刷新'}</button></div></header>
        {!status.connected&&<AuthBanner configured={status.configured} authMode={status.authMode}/>} {status.error&&<div className="error-banner"><span>{status.error}</span><button onClick={()=>setStatus(s=>({...s,error:undefined}))}><X size={15}/></button></div>}
        {replyTarget&&<div className="mention-focus"><AtSign size={16}/><div><strong>{replyTarget.senderName} @了你</strong><p>{replyTarget.content}</p></div><button onClick={focusReplyTarget}>定位消息</button></div>}
        <section className="message-scroll" aria-live="polite" ref={messageScrollRef} onScroll={trackMessageScroll}>
          {history.hasMore&&<button className="load-older load-older-top" onClick={loadOlderMessages} disabled={loadingOlder}><RefreshCw size={14} className={loadingOlder?'spin':''}/>{loadingOlder?'正在加载':'加载更早消息'}</button>}
          {messages.map((m,i)=><MessageBubble key={m.id} message={m} replyTarget={m.id===replyTarget?.id} compact={i>0&&messages[i-1].senderId===m.senderId&&Math.abs(m.createdAt-messages[i-1].createdAt)<5*60*1000}/>) }
          {!messages.length&&!loading&&<div className="empty-chat"><MessageCircle/><strong>暂无可显示的消息</strong><span>同步后，聊天记录会出现在这里。</span></div>}
        </section>
        <ReplyComposer chatId={selected.id} replyTarget={replyTarget} disabled={!status.connected} onResize={keepComposerAnchored}/>
      </>:<div className="empty-chat whole"><MessageCircle/><strong>选择一个会话</strong><span>查看个人单聊和群聊记录</span></div>}
    </main>
    <aside className="details-panel">
      {selected&&<><div className="detail-title">会话信息<button className="icon-button"><ChevronDown size={17}/></button></div><div className="identity"><Avatar chat={selected} size="large"/><strong>{selected.name}</strong><span>{selected.description||`${selected.type==='group'?'群组会话':'个人会话'} · ${messages.length} 条已加载消息`}</span></div><dl><div><dt>会话类型</dt><dd>{selected.type==='group'?<><Hash size={15}/>群聊</>:<><MessageCircle size={15}/>单聊</>}</dd></div>{selected.type==='group'&&<><div><dt>消息提醒</dt><dd>{selected.muted?<><BellOff size={15}/>已屏蔽</>:<><Bell size={15}/>正常</>}</dd></div><div><dt>消息位置</dt><dd>{selected.inMessageBox?<><Archive size={15}/>消息盒子</>:<><Inbox size={15}/>会话列表</>}</dd></div></>}<div><dt>数据来源</dt><dd><Check size={15}/>本地数据库</dd></div><div><dt>同步方式</dt><dd><RefreshCw size={15}/>轮询 + 主动刷新</dd></div></dl><div className="privacy-note"><BrainCircuit size={17}/><div><strong>本地智能工作台</strong><p>消息与记忆保存在本地；会话屏蔽和消息盒子也是当前工作台的本地状态。</p></div></div><AgentPanel chatId={selected.id} personId={selected.type==='group' ? replyTarget?.senderId : undefined}/></>}
    </aside>
  </div>
}

function StatusBadge({status}:{status:Status}){const ok=status.connected;return <div className={`status ${ok?'ok':''}`}><span>{ok?<Wifi size={15}/>:<span className="status-dot"/>}</span><div><strong>{status.connected?'Lark 已连接':'等待连接'}</strong><small>{status.lastSync?`${relativeTime(status.lastSync)}前同步`:'需要用户授权'}</small></div></div>}
function AuthBanner({configured,authMode}:{configured:boolean;authMode?:'cli'|'oauth'}){const cli=authMode==='cli';return <div className="auth-banner"><div><span>AUTH</span><p>Lark 尚未授权。{configured?'连接后即可同步真实会话。':'配置应用凭证后即可连接真实 Lark 会话。'}</p></div>{cli?<code>lark-cli auth login</code>:configured?<a href="/auth/lark">连接 Lark <ExternalLink size={14}/></a>:<code>.env.example</code>}</div>}
function MessageBubble({message:m,compact,replyTarget}:{message:Message;compact:boolean;replyTarget:boolean}){return <article id={`message-${m.id}`} className={`message ${m.isSelf?'self':''} ${compact?'compact':''} ${m.mentionsSelf?'mentions-self':''} ${replyTarget?'reply-target':''}`}>{!m.isSelf&&!compact?<div className="sender-avatar" style={{background:avatarColor(m.senderId)}}>{initials(m.senderName)}<AvatarImage src={m.senderAvatar}/></div>:<div className="sender-spacer"/>}<div className="message-body">{!m.isSelf&&!compact&&<span className="sender-name">{m.senderName}{m.mentionsSelf&&<em><AtSign size={10}/>我的消息</em>}</span>}<div className={`bubble ${imageKeys(m.content).length?'has-images':''}`}><MessageContent message={m}/><time>{clock(m.createdAt)}</time></div></div></article>}

function MessageContent({message}:{message:Message}){
  const keys=imageKeys(message.content),text=messageText(message.content)
  const [failed,setFailed]=useState<string[]>([])
  useEffect(()=>setFailed([]),[message.id,message.content])
  return <>{keys.length>0&&<div className="message-images">{keys.map(key=>{const src=`/api/messages/${encodeURIComponent(message.id)}/images/${encodeURIComponent(key)}`;return failed.includes(key)?<div className="image-failed" key={key}><ImageIcon size={20}/><span>图片加载失败</span></div>:<a href={src} target="_blank" rel="noreferrer" key={key} title="查看原图"><img src={src} alt="飞书消息图片" loading="lazy" onError={()=>setFailed(current=>current.includes(key)?current:[...current,key])}/></a>})}</div>}{text&&<p>{text}</p>}{!text&&!keys.length&&<p>{message.content}</p>}</>
}
