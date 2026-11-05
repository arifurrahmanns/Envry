import { Mail } from 'lucide-react'

import { cn } from '@/lib/utils'

export function Logo({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        'flex size-9 items-center justify-center rounded-[10px] bg-primary text-primary-foreground shadow-xs',
        className,
      )}
    >
      <Mail className="size-[18px]" strokeWidth={2} aria-hidden />
      <span className="sr-only">Envry</span>
    </div>
  )
}
