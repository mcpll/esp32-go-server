import { useState } from 'react'
import { createPortal } from 'react-dom'
import { Info } from 'lucide-react'

let closeOpenTip: (() => void) | null = null

export function FieldInfo({ text }: { text: string }) {
  const [tip, setTip] = useState<{ top: number; left: number; above: boolean } | null>(null)

  function hide() {
    setTip(null)
  }

  function show(target: HTMLButtonElement) {
    closeOpenTip?.()
    closeOpenTip = hide
    const rect = target.getBoundingClientRect()
    const width = 288
    const left = Math.max(8, Math.min(rect.left, window.innerWidth - width - 8))
    const above = rect.top > 88
    setTip({ top: above ? rect.top - 6 : rect.bottom + 6, left, above })
  }

  return (
    <>
      <button
        type="button"
        aria-label={text}
        className="inline-flex size-5 shrink-0 items-center justify-center rounded-full text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        onMouseEnter={(event) => show(event.currentTarget)}
        onMouseLeave={hide}
        onFocus={(event) => show(event.currentTarget)}
        onBlur={hide}
      >
        <Info className="size-3.5" strokeWidth={1.8} />
      </button>
      {tip !== null ? <Tip top={tip.top} left={tip.left} above={tip.above} text={text} /> : null}
    </>
  )
}

function Tip({ top, left, above, text }: { top: number; left: number; above: boolean; text: string }) {
  return createPortal(
    <span
      role="tooltip"
      style={{ top, left, transform: above ? 'translateY(-100%)' : undefined }}
      className="pointer-events-none fixed z-50 w-72 rounded-lg bg-foreground px-3 py-2 text-xs leading-snug text-background"
    >
      {text}
    </span>,
    document.body,
  )
}
