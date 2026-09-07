import { useState } from 'react'
import { AtSign, Check, Copy, RefreshCw, Send, Sparkles, X } from 'lucide-react'
import type { ReplyRecommendation } from './types'

type Props = {
  recommendation: ReplyRecommendation
  onClose: () => void
  instruction: string
  onInstructionChange: (value: string) => void
  onRefine: () => void
  refining?: boolean
  error?: string
}

export function ReplySuggestionTray({ recommendation, onClose, instruction, onInstructionChange, onRefine, refining, error }: Props) {
  const [copied, setCopied] = useState<number>()

  async function copy(text: string, index: number) {
    await navigator.clipboard.writeText(text)
    setCopied(index)
    window.setTimeout(() => setCopied(undefined), 1200)
  }

  return <section className="reply-tray" aria-live="polite">
    <header><div><Sparkles size={16}/><strong>推荐回复</strong><span>{recommendation.suggestions.length} 条</span></div><button onClick={onClose} aria-label="关闭推荐回复"><X size={16}/></button></header>
    {recommendation.replyTarget && <div className="reply-tray-target"><AtSign size={13}/><span>回复 {recommendation.replyTarget.senderName}</span><p>{recommendation.replyTarget.content}</p></div>}
    {!!recommendation.appliedStyles?.length && <div className="reply-style-chips">{recommendation.appliedStyles.map(style => <span key={style}>{style}</span>)}</div>}
    {recommendation.thinking && <p className="reply-tray-thinking">{recommendation.thinking}</p>}
    <div className="reply-tray-options">
      {recommendation.suggestions.map((suggestion, index) => <article key={index}>
        <div><span>{suggestion.tone || `方案 ${index + 1}`}</span><button onClick={() => void copy(suggestion.text, index)}>{copied === index ? <><Check size={13}/>已复制</> : <><Copy size={13}/>复制</>}</button></div>
        <p>{suggestion.text}</p>
      </article>)}
    </div>
    {recommendation.instruction && <div className="reply-refine-applied">已按要求重新生成：{recommendation.instruction}</div>}
    <form className="reply-refine" onSubmit={event => { event.preventDefault(); if (instruction.trim() && !refining) onRefine() }}>
      <div>
        <Sparkles size={14}/>
        <input
          value={instruction}
          onChange={event => onInstructionChange(event.target.value)}
          placeholder="继续告诉 Agent：例如，给我一个更严肃、更简短的答复"
          maxLength={1000}
          aria-label="补充回复要求"
          disabled={refining}
        />
      </div>
      <button type="submit" disabled={!instruction.trim() || refining}>
        {refining ? <RefreshCw className="spin" size={14}/> : <Send size={14}/>}
        {refining ? '重新思考中' : '按要求生成 3 条'}
      </button>
    </form>
    {error && <div className="reply-refine-error">{error}</div>}
    <small>候选内容不会自动发送；复制后请在飞书中确认并发送。</small>
  </section>
}
