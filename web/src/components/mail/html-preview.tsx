import { Monitor, Smartphone, Tablet } from 'lucide-react'

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { urls } from '@/lib/api'
import { cn } from '@/lib/utils'

const widths = [
  { id: 'desktop', label: 'Desktop', icon: Monitor, px: null },
  { id: 'tablet', label: 'Tablet (768 px)', icon: Tablet, px: 768 },
  { id: 'mobile', label: 'Mobile (375 px)', icon: Smartphone, px: 375 },
] as const

export type PreviewWidth = (typeof widths)[number]['id']

export function DeviceWidthToggle({
  value,
  onChange,
}: {
  value: PreviewWidth
  onChange: (w: PreviewWidth) => void
}) {
  return (
    <div role="radiogroup" aria-label="Preview width" className="flex items-center gap-0.5 rounded-lg border p-0.5">
      {widths.map(({ id, label, icon: Icon }) => (
        <Tooltip key={id}>
          <TooltipTrigger asChild>
            <button
              type="button"
              role="radio"
              aria-checked={value === id}
              aria-label={label}
              onClick={() => onChange(id)}
              className={cn(
                'flex size-7 items-center justify-center rounded-md text-muted-foreground hover:text-foreground',
                value === id && 'bg-muted text-foreground',
              )}
            >
              <Icon className="size-4" strokeWidth={1.75} />
            </button>
          </TooltipTrigger>
          <TooltipContent>{label}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  )
}

/**
 * Renders the server-sanitised HTML in a sandboxed iframe: no scripts, no
 * same-origin access. Emails are designed for a white canvas, so the frame
 * stays white in dark mode too.
 */
export function HtmlPreview({ messageId, width }: { messageId: string; width: PreviewWidth }) {
  const px = widths.find((w) => w.id === width)?.px ?? null
  return (
    <div className="flex h-full justify-center overflow-x-auto rounded-lg border bg-muted/40">
      <iframe
        key={messageId}
        title="Email preview"
        src={urls.html(messageId)}
        sandbox="allow-popups allow-popups-to-escape-sandbox"
        referrerPolicy="no-referrer"
        className={cn('h-full min-h-[480px] bg-email-canvas transition-[width] duration-150', px ? 'border-x shadow-xs' : 'w-full rounded-lg')}
        style={px ? { width: px, flex: 'none' } : undefined}
      />
    </div>
  )
}
