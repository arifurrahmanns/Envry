import { format, isThisYear, isToday, isYesterday } from 'date-fns'

import type { Address, MessageSummary } from '@/lib/api'

/** List timestamp: "14:05" today, "Yesterday", "Jun 29", or "Jun 29, 2025". */
export function listTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  if (isToday(d)) return format(d, 'HH:mm')
  if (isYesterday(d)) return 'Yesterday'
  return isThisYear(d) ? format(d, 'MMM d') : format(d, 'MMM d, yyyy')
}

/** Reader timestamp: "Thu, Jun 29, 2026 at 14:05". */
export function fullTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return format(d, "EEE, MMM d, yyyy 'at' HH:mm")
}

export function fileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB']
  let v = bytes / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`
}

export function count(n: number): string {
  return new Intl.NumberFormat().format(n)
}

export function displayName(a: Address): string {
  return a.name.trim() || a.address
}

export function formatAddress(a: Address): string {
  return a.name.trim() ? `${a.name} <${a.address}>` : a.address
}

export function messageTime(m: MessageSummary): string {
  return m.date ?? m.receivedAt
}

export function initials(nameOrAddress: string): string {
  const local = nameOrAddress.split('@')[0] ?? ''
  const parts = local
    .replace(/[._+-]+/g, ' ')
    .split(/\s+/)
    .filter(Boolean)
  if (parts.length === 0) return '?'
  const first = parts[0][0] ?? ''
  const last = parts.length > 1 ? (parts[parts.length - 1][0] ?? '') : (parts[0][1] ?? '')
  return (first + last).toUpperCase()
}

/** Stable 0..n-1 bucket for an address, used to pick an avatar colour. */
export function hashBucket(value: string, n: number): number {
  let h = 0
  for (let i = 0; i < value.length; i++) h = (h * 31 + value.charCodeAt(i)) | 0
  return Math.abs(h) % n
}
