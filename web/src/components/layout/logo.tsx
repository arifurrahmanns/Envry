import { cn } from '@/lib/utils'

/** Envry mark: an envelope whose flap is a lightning stroke. */
export function Logo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2.25}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={cn('size-7 text-foreground', className)}
      role="img"
      aria-label="Envry"
    >
      <rect x="2.5" y="5" width="19" height="14" rx="2.5" />
      <path d="m3 6.5 7.2 5.4h3.6L21 6.5" />
      <path d="m13.5 10.5-2 3.5h3l-2 3.5" strokeWidth={1.75} />
    </svg>
  )
}
