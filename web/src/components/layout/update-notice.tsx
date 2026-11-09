import { ArrowUpCircle, Loader2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { installUpdate, useAppUpdate } from '@/hooks/use-app-update'

/**
 * Tells the user, at the bottom of the sidebar, that the desktop app has
 * downloaded a new version and offers to restart into it. Nothing shows
 * while Envry is up to date or in a browser.
 */
export function UpdateNotice({ collapsed }: { collapsed: boolean }) {
  const update = useAppUpdate()
  if (!update || (update.state !== 'ready' && update.state !== 'installing')) return null
  const installing = update.state === 'installing'
  const label = installing ? `Installing Envry ${update.version}…` : `Restart to update to Envry ${update.version}`

  if (collapsed) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            aria-label={label}
            disabled={installing}
            onClick={() => void installUpdate()}
            className="relative mt-4 flex size-10 items-center justify-center rounded-[10px] text-foreground hover:bg-accent"
          >
            {installing ? <Loader2 className="size-4 animate-spin" /> : <ArrowUpCircle className="size-4" />}
            <span aria-hidden className="absolute top-2 right-2 size-2 rounded-full bg-brand" />
          </button>
        </TooltipTrigger>
        <TooltipContent side="right">{label}</TooltipContent>
      </Tooltip>
    )
  }

  return (
    <div className="mt-4 rounded-xl border bg-card px-3 py-3" role="status">
      <div className="flex items-start gap-2.5">
        <ArrowUpCircle className="mt-0.5 size-4 shrink-0 text-brand" />
        <div className="min-w-0 flex-1">
          <p className="text-[13px] font-medium">Update ready</p>
          <p className="mt-0.5 text-[12px] leading-relaxed text-muted-foreground">
            Envry {update.version} is downloaded. Restart to start using it.
          </p>
        </div>
      </div>
      <Button size="sm" className="mt-3 w-full" disabled={installing} onClick={() => void installUpdate()}>
        {installing && <Loader2 className="animate-spin" />}
        {installing ? 'Installing…' : 'Restart to update'}
      </Button>
    </div>
  )
}
