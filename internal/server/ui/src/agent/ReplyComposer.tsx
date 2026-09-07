import { useEffect, useState } from 'react'
import { AtSign, RefreshCw, Sparkles } from 'lucide-react'
import type { Message } from '../inbox/types'
import { api } from '../lib/api'
import { ReplySuggestionTray } from './ReplySuggestionTray'
import type { ReplyRecommendation } from './types'

type Props = {
  chatId: string
  replyTarget?: Message
  disabled?: boolean
  onResize?: () => void
}

export function ReplyComposer({ chatId, replyTarget, disabled, onResize }: Props) {
  const [recommendation, setRecommendation] = useState<ReplyRecommendation>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const [instruction, setInstruction] = useState('')

  useEffect(() => {
    setRecommendation(undefined)
    setError(undefined)
    setInstruction('')
  }, [chatId])

  async function recommend(refinement = '') {
    if (busy || disabled) return
    setBusy(true)
    setError(undefined)
    try {
      const result = await api<ReplyRecommendation>(`/api/chats/${encodeURIComponent(chatId)}/recommendation`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          targetMessageId: replyTarget?.id || recommendation?.replyTarget?.messageId || '',
          instruction: refinement,
          previousSuggestions: refinement ? recommendation?.suggestions || [] : [],
        }),
      })
      setRecommendation(result)
      if (refinement) setInstruction('')
      onResize?.()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  function close() {
    setRecommendation(undefined)
    onResize?.()
  }

  return <section className="reply-composer" aria-label="推荐回复">
    {recommendation && <ReplySuggestionTray
      recommendation={recommendation}
      onClose={close}
      instruction={instruction}
      onInstructionChange={setInstruction}
      onRefine={() => void recommend(instruction.trim())}
      refining={busy}
      error={error}
    />}
    <div className="reply-composer-bar">
      <div className="reply-composer-context">
        <span className="reply-composer-icon"><Sparkles size={17}/></span>
        <div><strong>推荐回复</strong><span>{replyTarget ? <><AtSign size={11}/>优先回复 {replyTarget.senderName}：{replyTarget.content}</> : '根据当前会话、记忆和知识库生成候选内容'}</span></div>
      </div>
      {error && !recommendation && <span className="reply-composer-error">{error}</span>}
      <button className="reply-composer-action" onClick={() => void recommend()} disabled={disabled || busy}>
        {busy ? <RefreshCw className="spin" size={15}/> : <Sparkles size={15}/>}
        {busy ? '正在生成' : recommendation ? '重新生成' : replyTarget ? `回复 @${replyTarget.senderName}` : '生成推荐回复'}
      </button>
    </div>
  </section>
}
