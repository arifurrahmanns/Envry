import { Check, Monitor, Moon, Sun } from 'lucide-react'

import { useTheme, type Theme } from '@/components/theme-provider'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

const options: { value: Theme; label: string; icon: typeof Sun }[] = [
  { value: 'light', label: 'Light', icon: Sun },
  { value: 'dark', label: 'Dark', icon: Moon },
  { value: 'system', label: 'System', icon: Monitor },
]

export function ModeToggle({
  className,
  side = 'bottom',
}: {
  className?: string
  side?: 'right' | 'bottom'
}) {
  const { theme, resolvedTheme, setTheme } = useTheme()
  const Icon = resolvedTheme === 'dark' ? Moon : Sun

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className={cn(className)} aria-label="Change theme">
        <Icon className="size-5" strokeWidth={1.75} />
      </DropdownMenuTrigger>
      <DropdownMenuContent side={side} align="end">
        {options.map(({ value, label, icon: OptionIcon }) => (
          <DropdownMenuItem key={value} onSelect={() => setTheme(value)}>
            <OptionIcon />
            {label}
            {theme === value && <Check className="ml-auto" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
