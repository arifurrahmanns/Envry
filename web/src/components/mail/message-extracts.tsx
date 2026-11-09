import { useState } from 'react'
import { Check, Copy, KeyRound, LockKeyhole, LogIn, MailCheck, UserPlus } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { ActionLink, ActionLinkKind, ExtractedCode } from '@/lib/api'
import { useMessageExtracts } from '@/lib/queries'

const LINKS: Record<ActionLinkKind, { label: string; icon: LucideIcon }> = {
  verify: { label: 'Verify link', icon: MailCheck },
  reset: { label: 'Reset link', icon: LockKeyhole },
  login: { label: 'Sign-in link', icon: LogIn },
  invite: { label: 'Invite link', icon: UserPlus },
}

/**
 * The one-time code and action links found in the message, as chips under
 * the header: what a verification or sign-in email is usually opened for.
 */
export function ExtractChips({ messageId }: { messageId: string }) {
  const { data } = useMessageExtracts(messageId)
  if (!data || (data.codes.length === 0 && data.links.length === 0)) return null
  return (
    <div className="mt-3 flex flex-wrap items-center gap-2 sm:pl-12">
      {data.codes.map((c) => (
        <CodeChip key={c.value} code={c} />
      ))}
      {data.links.map((l) => (
        <LinkChip key={l.url} link={l} />
      ))}
    </div>
  )
}

function CodeChip({ code }: { code: ExtractedCode }) {
  return (
    <div className="flex h-8 items-center gap-1.5 rounded-lg border bg-muted/40 pr-1 pl-2.5 text-[13px]">
      <KeyRound className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="text-muted-foreground">Code</span>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="font-mono font-semibold tracking-wider select-all">{code.value}</span>
        </TooltipTrigger>
        {code.context && <TooltipContent className="max-w-sm">{code.context}</TooltipContent>}
      </Tooltip>
      <CopyIconButton value={code.value} label="Copy code" />
    </div>
  )
}

function LinkChip({ link }: { link: ActionLink }) {
  const { label, icon: Icon } = LINKS[link.kind]
  return (
    <div className="flex h-8 max-w-full min-w-0 items-center gap-1.5 rounded-lg border bg-muted/40 pr-1 pl-2.5 text-[13px]">
      <Icon className="size-3.5 shrink-0 text-muted-foreground" />
      <Tooltip>
        <TooltipTrigger asChild>
          <a href={link.url} target="_blank" rel="noreferrer" className="truncate font-medium hover:underline">
            {label}
          </a>
        </TooltipTrigger>
        <TooltipContent className="max-w-sm break-all">
          {link.text && <span className="block font-medium">{link.text}</span>}
          {link.url}
        </TooltipContent>
      </Tooltip>
      <CopyIconButton value={link.url} label="Copy link" />
    </div>
  )
}

function CopyIconButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false)
  const copy = () =>
    void navigator.clipboard.writeText(value).then(() => {
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    })
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon-xs" aria-label={label} onClick={copy}>
          {copied ? <Check className="text-success" /> : <Copy />}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{copied ? 'Copied' : label}</TooltipContent>
    </Tooltip>
  )
}
