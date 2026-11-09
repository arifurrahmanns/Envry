import { differenceInMinutes, format, isThisYear, isToday, isYesterday } from 'date-fns'

import type { Address, MessageSummary } from '@/lib/api'

function parse(iso: string): Date | null {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? null : d
}

/** Short date: "Today", "Yesterday", "Jun 29", or "Jun 29, 2025". */
export function listTime(iso: string): string {
  const d = parse(iso)
  if (!d) return ''
  if (isToday(d)) return format(d, 'h:mm a')
  if (isYesterday(d)) return 'Yesterday'
  return isThisYear(d) ? format(d, 'MMM d') : format(d, 'MMM d, yyyy')
}

/** Row time: "now", "5m ago" and "3h ago" today; "3:29 PM" on earlier days. */
export function rowTime(iso: string, now = new Date()): string {
  const d = parse(iso)
  if (!d) return ''
  if (!isToday(d)) return format(d, 'h:mm a')
  const mins = Math.max(0, differenceInMinutes(now, d))
  if (mins < 1) return 'now'
  if (mins < 60) return `${mins}m ago`
  return `${Math.floor(mins / 60)}h ago`
}

/** Day heading for grouped lists: "Today", "Yesterday", "Aug 21". */
export function dayLabel(iso: string): string {
  const d = parse(iso)
  if (!d) return 'Unknown date'
  if (isToday(d)) return 'Today'
  if (isYesterday(d)) return 'Yesterday'
  return isThisYear(d) ? format(d, 'MMM d') : format(d, 'MMM d, yyyy')
}

/** Splits a newest-first list into consecutive day groups. */
export function groupByDay<T>(items: T[], date: (item: T) => string): { label: string; items: T[] }[] {
  const groups: { label: string; items: T[] }[] = []
  for (const item of items) {
    const label = dayLabel(date(item))
    const last = groups[groups.length - 1]
    if (last && last.label === label) last.items.push(item)
    else groups.push({ label, items: [item] })
  }
  return groups
}

/** Reader timestamp: "Thu, Jun 29, 2026 at 2:05 PM". */
export function fullTime(iso: string): string {
  const d = parse(iso)
  if (!d) return ''
  return format(d, "EEE, MMM d, yyyy 'at' h:mm a")
}

/** Property timestamp: "Nov 5, 2026, 2:05 PM". */
export function mediumTime(iso: string): string {
  const d = parse(iso)
  if (!d) return ''
  return format(d, 'MMM d, yyyy, h:mm a')
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

const sources: Record<string, string> = {
  smtp: 'SMTP',
  api: 'API',
  imap_append: 'IMAP',
  'api:resend': 'Resend',
  'api:sendgrid': 'SendGrid',
  'api:postmark': 'Postmark',
  'api:mailgun': 'Mailgun',
  'api:ses': 'Amazon SES',
}

/** How a message arrived: "SMTP", or the provider API it was sent through. */
export function sourceName(source: string): string {
  return sources[source] ?? source
}

/** Whether the message was sent through an emulated provider API. */
export function viaProvider(source: string): boolean {
  return source.startsWith('api:')
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
