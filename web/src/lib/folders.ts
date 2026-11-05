import { FilePen, Folder, Inbox, Send, Trash2, type LucideIcon } from 'lucide-react'

import type { Folder as FolderInfo } from '@/lib/api'

export const DEFAULT_FOLDER = 'INBOX'

const bySpecialUse: Record<string, LucideIcon> = {
  '\\Sent': Send,
  '\\Drafts': FilePen,
  '\\Trash': Trash2,
}

export function folderIcon(f: FolderInfo): LucideIcon {
  if (f.name.toUpperCase() === 'INBOX') return Inbox
  return (f.specialUse && bySpecialUse[f.specialUse]) || Folder
}

export function folderLabel(name: string): string {
  return name.toUpperCase() === 'INBOX' ? 'Inbox' : name
}

/** INBOX first, then special-use folders, then custom folders A–Z. */
export function sortFolders(folders: FolderInfo[]): FolderInfo[] {
  const rank = (f: FolderInfo) =>
    f.name.toUpperCase() === 'INBOX' ? 0 : f.specialUse === '\\Sent' ? 1 : f.specialUse === '\\Drafts' ? 2 : f.specialUse === '\\Trash' ? 3 : 4
  return [...folders].sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name))
}
