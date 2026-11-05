// Typed client for the Envry REST API (/api/v1). These types are the
// contract the Go handlers in internal/api serialize to.

export type Address = {
  name: string
  address: string
}

export type RecipientType = 'to' | 'cc' | 'bcc'

export type MessageSource = 'smtp' | 'imap_append' | 'api'

export type MessageSummary = {
  id: string
  from: Address
  to: Address[]
  cc: Address[]
  subject: string
  preview: string
  /** Date header, ISO 8601; null when missing or unparsable. */
  date: string | null
  /** When Envry received the message, ISO 8601. */
  receivedAt: string
  size: number
  hasHtml: boolean
  hasText: boolean
  attachmentCount: number
  source: MessageSource
}

export type Attachment = {
  id: string
  filename: string
  contentType: string
  contentId: string | null
  inline: boolean
  size: number
}

export type Header = {
  name: string
  value: string
}

export type MessageDetail = MessageSummary & {
  messageId: string | null
  envelopeFrom: string
  replyTo: Address[]
  /** Every envelope recipient (RCPT TO) with how it was addressed. */
  recipients: { address: string; type: RecipientType }[]
  headers: Header[]
  text: string | null
  clientIp: string | null
  authUser: string | null
  attachments: Attachment[]
}

/** A message as delivered into one mailbox folder. */
export type Delivery = MessageSummary & {
  deliveryId: number
  folder: string
  rcptType: RecipientType | 'self'
  seen: boolean
  flagged: boolean
}

export type Folder = {
  name: string
  specialUse: string | null
  total: number
  unread: number
}

export type Mailbox = {
  address: string
  displayName: string | null
  isAccount: boolean
  total: number
  unread: number
  lastReceivedAt: string | null
  folders: Folder[]
}

export type Page<T> = {
  items: T[]
  total: number
  /** Pass as `before` to fetch the next (older) page; null on the last page. */
  nextCursor: string | null
}

export type Info = {
  version: string
  commit: string
  ports: { smtp: number; pop3: number; imap: number; http: number }
  settings: { maxMessages: number; maxSize: number; strictAuth: boolean }
  stats: { messages: number; mailboxes: number; accounts: number }
}

export type SendRequest = {
  from: string
  to: string[]
  cc: string[]
  bcc: string[]
  subject: string
  text: string
  html?: string
}

export type CreateAccountRequest = {
  address: string
  displayName?: string
  password?: string
}

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

const base = '/api/v1'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(base + path, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  })
  if (!res.ok) {
    let message = res.statusText || `HTTP ${res.status}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // non-JSON error body: keep the status text
    }
    throw new ApiError(res.status, message)
  }
  if (res.status === 204) return undefined as T
  const type = res.headers.get('Content-Type') ?? ''
  return (type.includes('application/json') ? res.json() : res.text()) as Promise<T>
}

function query(params: Record<string, string | number | undefined | null>) {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') q.set(k, String(v))
  }
  const s = q.toString()
  return s ? `?${s}` : ''
}

const enc = encodeURIComponent

export const PAGE_SIZE = 50

export const api = {
  info: () => request<Info>('/info'),

  messages: (p: { q?: string; before?: string | null; limit?: number } = {}) =>
    request<Page<MessageSummary>>(`/messages${query({ limit: PAGE_SIZE, ...p })}`),
  message: (id: string) => request<MessageDetail>(`/messages/${enc(id)}`),
  rawSource: (id: string) => request<string>(`/messages/${enc(id)}/raw`),
  deleteMessage: (id: string) => request<void>(`/messages/${enc(id)}`, { method: 'DELETE' }),
  deleteAllMessages: () => request<void>('/messages', { method: 'DELETE' }),

  mailboxes: () => request<{ items: Mailbox[] }>('/mailboxes'),
  mailboxMessages: (
    address: string,
    p: { folder?: string; q?: string; before?: string | null; limit?: number } = {},
  ) =>
    request<Page<Delivery>>(
      `/mailboxes/${enc(address)}/messages${query({ limit: PAGE_SIZE, ...p })}`,
    ),

  updateDelivery: (id: number, patch: { seen?: boolean; flagged?: boolean }) =>
    request<Delivery>(`/deliveries/${id}`, { method: 'PATCH', body: JSON.stringify(patch) }),
  /** Removes one mailbox's copy; the message stays in Sent and other mailboxes. */
  deleteDelivery: (id: number) => request<void>(`/deliveries/${id}`, { method: 'DELETE' }),

  createAccount: (body: CreateAccountRequest) =>
    request<Mailbox>('/accounts', { method: 'POST', body: JSON.stringify(body) }),

  send: (body: SendRequest) =>
    request<{ id: string }>('/send', { method: 'POST', body: JSON.stringify(body) }),
}

/** URLs the browser loads directly (iframe, downloads). */
export const urls = {
  html: (id: string, remoteContent = false) =>
    `${base}/messages/${enc(id)}/html${remoteContent ? '?remote=1' : ''}`,
  raw: (id: string, download = false) =>
    `${base}/messages/${enc(id)}/raw${download ? '?download=1' : ''}`,
  attachment: (id: string, aid: string) =>
    `${base}/messages/${enc(id)}/attachments/${enc(aid)}`,
  ws: () => {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${window.location.host}/ws`
  },
}
