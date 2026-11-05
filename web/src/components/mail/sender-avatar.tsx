import { hashBucket, initials } from '@/lib/format'
import { cn } from '@/lib/utils'

// Static class names so Tailwind can see them; colours are theme tokens.
const palettes = [
  'bg-(--avatar-1-bg) text-(--avatar-1-fg)',
  'bg-(--avatar-2-bg) text-(--avatar-2-fg)',
  'bg-(--avatar-3-bg) text-(--avatar-3-fg)',
  'bg-(--avatar-4-bg) text-(--avatar-4-fg)',
  'bg-(--avatar-5-bg) text-(--avatar-5-fg)',
  'bg-(--avatar-6-bg) text-(--avatar-6-fg)',
]

const sizes = {
  sm: 'size-7 text-[11px]',
  md: 'size-9 text-xs',
  lg: 'size-10 text-sm',
}

export function SenderAvatar({
  name,
  address,
  size = 'sm',
  className,
}: {
  name?: string
  address: string
  size?: keyof typeof sizes
  className?: string
}) {
  return (
    <span
      aria-hidden
      className={cn(
        'flex shrink-0 items-center justify-center rounded-full font-semibold tracking-tight select-none',
        palettes[hashBucket(address.toLowerCase(), palettes.length)],
        sizes[size],
        className,
      )}
    >
      {initials(name?.trim() || address)}
    </span>
  )
}
