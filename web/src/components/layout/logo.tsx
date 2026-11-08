import { useId } from 'react'

import { cn } from '@/lib/utils'

/**
 * The Envry app icon: an envelope whose flap is a lightning stroke, on a
 * graphite tile with a red glow (the same tokens as primary buttons).
 */
export function Logo({ className }: { className?: string }) {
  // useId returns characters url(#...) references cannot contain.
  const id = `logo${useId().replace(/[^a-zA-Z0-9]/g, '')}`
  return (
    <svg viewBox="0 0 32 32" className={cn('size-7 shrink-0', className)} role="img" aria-label="Envry">
      <defs>
        <linearGradient id={`${id}-tile`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" style={{ stopColor: 'var(--primary-top)' }} />
          <stop offset="1" style={{ stopColor: 'var(--primary-bottom)' }} />
        </linearGradient>
        <radialGradient id={`${id}-glow`} cx="1" cy="1" r="0.8">
          <stop offset="0" style={{ stopColor: 'var(--primary-glow)' }} />
          <stop offset="1" style={{ stopColor: 'var(--primary-glow)', stopOpacity: 0 }} />
        </radialGradient>
      </defs>
      <rect width="32" height="32" rx="7.5" fill={`url(#${id}-tile)`} />
      <rect width="32" height="32" rx="7.5" fill={`url(#${id}-glow)`} />
      <g
        transform="translate(4 3.6)"
        fill="none"
        style={{ stroke: 'var(--brand-foreground)' }}
        strokeWidth={2.25}
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <rect x="2.5" y="5" width="19" height="14" rx="2.5" />
        <path d="m3 6.5 7.2 5.4h3.6L21 6.5" />
        <path d="m13.5 10.5-2 3.5h3l-2 3.5" strokeWidth={1.75} />
      </g>
    </svg>
  )
}
