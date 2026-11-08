import { ResizableHandle } from '@/components/ui/resizable'

/** The draggable line between resizable panes: a wider grab area and a hover highlight. */
export function PaneDivider() {
  return <ResizableHandle className="transition-colors after:w-2 hover:bg-ring data-[separator=active]:bg-ring" />
}
