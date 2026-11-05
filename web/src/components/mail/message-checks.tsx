import { CircleCheck, CircleX, Info, Link2, Loader2, TriangleAlert } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

import { Section } from '@/components/mail/section'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { Check, CheckStatus, LinkResult, MessageLink } from '@/lib/api'
import { useCheckLinks, useMessageChecks } from '@/lib/queries'
import { cn } from '@/lib/utils'

const severity: Record<CheckStatus, number> = { fail: 0, warn: 1, info: 2, pass: 3 }

const statusStyle: Record<CheckStatus, { icon: LucideIcon; tone: string; label: string }> = {
  fail: { icon: CircleX, tone: 'text-destructive', label: 'Failed' },
  warn: { icon: TriangleAlert, tone: 'text-warning', label: 'Warning' },
  info: { icon: Info, tone: 'text-muted-foreground', label: 'Note' },
  pass: { icon: CircleCheck, tone: 'text-success', label: 'Passed' },
}

/** Rendering and deliverability checks for one message, plus an on-demand link check. */
export function ChecksSection({ messageId }: { messageId: string }) {
  const { data, isLoading, error } = useMessageChecks(messageId)
  const checks = data ? [...data.checks].sort((a, b) => severity[a.status] - severity[b.status]) : []

  return (
    <Section title="Checks" aside={data && <Summary checks={data.checks} />}>
      {isLoading ? (
        <div className="space-y-4">
          {[0, 1, 2].map((i) => (
            <div key={i} className="flex gap-3">
              <Skeleton className="size-[18px] rounded-full" />
              <div className="flex-1 space-y-2">
                <Skeleton className="h-4 w-40" />
                <Skeleton className="h-4 w-3/4" />
              </div>
            </div>
          ))}
        </div>
      ) : error || !data ? (
        <p className="text-sm text-muted-foreground">Couldn't run the checks: {error?.message ?? 'no data'}</p>
      ) : (
        <>
          <ul className="space-y-4">
            {checks.map((c) => (
              <CheckRow key={c.id} check={c} />
            ))}
          </ul>
          {data.links.length > 0 && <LinkStatus messageId={messageId} links={data.links} />}
        </>
      )}
    </Section>
  )
}

function Summary({ checks }: { checks: Check[] }) {
  const failed = checks.filter((c) => c.status === 'fail').length
  const warnings = checks.filter((c) => c.status === 'warn').length
  if (failed === 0 && warnings === 0) {
    return <span className="text-[13px] text-success">No problems found</span>
  }
  return (
    <span className="text-[13px] tabular-nums">
      {failed > 0 && <span className="text-destructive">{failed} failed</span>}
      {failed > 0 && warnings > 0 && <span className="text-muted-foreground"> · </span>}
      {warnings > 0 && (
        <span className="text-warning">
          {warnings} warning{warnings === 1 ? '' : 's'}
        </span>
      )}
    </span>
  )
}

function CheckRow({ check: c }: { check: Check }) {
  const { icon: Icon, tone, label } = statusStyle[c.status]
  return (
    <li className="flex gap-3">
      <Icon className={cn('mt-0.5 size-[18px] shrink-0', tone)} strokeWidth={1.75} aria-label={label} role="img" />
      <div className="min-w-0 flex-1">
        <p className="text-[14px] font-medium">{c.title}</p>
        <p className="mt-0.5 text-[14px] leading-relaxed text-muted-foreground">{c.message}</p>
        {c.details.length > 0 && (
          <ul className="mt-2 list-disc space-y-1 pl-4 text-[13px] leading-relaxed text-foreground/80 marker:text-muted-foreground">
            {c.details.map((d, i) => (
              <li key={i} className="[overflow-wrap:anywhere]">
                {d}
              </li>
            ))}
          </ul>
        )}
      </div>
    </li>
  )
}

function LinkStatus({ messageId, links }: { messageId: string; links: MessageLink[] }) {
  const check = useCheckLinks(messageId)
  const results = new Map(check.data?.results.map((r) => [r.url, r]))
  const broken = check.data?.results.filter((r) => !r.ok).length ?? 0
  const skipped = check.data ? check.data.total - check.data.results.length : 0

  return (
    <div className="mt-8">
      <div className="flex items-center gap-3">
        <div className="min-w-0">
          <h4 className="text-[14px] font-medium">
            Link status{' '}
            <span className="font-normal text-muted-foreground tabular-nums">{links.length}</span>
          </h4>
          <p className="mt-0.5 text-[13px] text-muted-foreground">
            {check.data
              ? broken === 0
                ? 'Every link responded.'
                : `${broken} broken or unreachable link${broken === 1 ? '' : 's'}.`
              : 'Envry requests each link only when you ask.'}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          className="ml-auto shrink-0"
          onClick={() => check.mutate()}
          disabled={check.isPending}
        >
          {check.isPending ? <Loader2 className="animate-spin" /> : <Link2 />}
          {check.data ? 'Check again' : 'Check links'}
        </Button>
      </div>
      {check.error && (
        <p className="mt-3 text-[13px] text-destructive">Couldn't check the links: {check.error.message}</p>
      )}
      <ul className="mt-3 divide-y overflow-hidden rounded-xl border">
        {links.map((l, i) => (
          <li key={l.url} className="flex items-center gap-3 px-3 py-2">
            <div className="min-w-0 flex-1">
              <a
                href={l.url}
                target="_blank"
                rel="noopener noreferrer"
                className="block truncate font-mono text-xs text-foreground/90 hover:underline"
              >
                {l.url}
              </a>
              {l.text && <p className="truncate text-[13px] text-muted-foreground">{l.text}</p>}
            </div>
            <LinkBadge result={results.get(l.url)} pending={check.isPending && i < 50} />
          </li>
        ))}
      </ul>
      {skipped > 0 && (
        <p className="mt-2 text-[13px] text-muted-foreground">
          Checked the first {check.data?.results.length} links; {skipped} more were skipped.
        </p>
      )}
    </div>
  )
}

function LinkBadge({ result, pending }: { result?: LinkResult; pending: boolean }) {
  if (pending) return <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" aria-label="Checking" />
  if (!result) return null
  const tone = result.ok ? 'bg-success/15 text-success' : 'bg-destructive/15 text-destructive'
  if (result.status > 0) {
    return (
      <span className={cn('shrink-0 rounded-md px-2 py-0.5 font-mono text-xs font-medium tabular-nums', tone)}>
        {result.status}
      </span>
    )
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={cn('max-w-40 shrink-0 truncate rounded-md px-2 py-0.5 text-xs font-medium', tone)}>
          {result.error ?? 'Failed'}
        </span>
      </TooltipTrigger>
      <TooltipContent>{result.error ?? 'The request failed'}</TooltipContent>
    </Tooltip>
  )
}
