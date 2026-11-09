import {
  QueryClient,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'

import { api, type CreateAccountRequest, type SendRequest } from '@/lib/api'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

/** Query keys, grouped so live events can invalidate whole families. */
export const keys = {
  info: ['info'] as const,
  messages: (q = '') => ['messages', { q }] as const,
  message: (id: string) => ['message', id] as const,
  raw: (id: string) => ['message', id, 'raw'] as const,
  checks: (id: string) => ['message', id, 'checks'] as const,
  extracts: (id: string) => ['message', id, 'extracts'] as const,
  mailboxes: ['mailboxes'] as const,
  webhooks: ['webhooks'] as const,
  webhookDeliveries: ['webhook-deliveries'] as const,
  mailboxMessages: (address: string, folder: string, q = '') =>
    ['mailbox-messages', address, folder, { q }] as const,
}

export function useInfo() {
  return useQuery({ queryKey: keys.info, queryFn: api.info, staleTime: Infinity })
}

export function useMessages(q: string) {
  return useInfiniteQuery({
    queryKey: keys.messages(q),
    queryFn: ({ pageParam }) => api.messages({ q, before: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.nextCursor,
  })
}

export function useMailboxMessages(address: string, folder: string, q: string) {
  return useInfiniteQuery({
    queryKey: keys.mailboxMessages(address, folder, q),
    queryFn: ({ pageParam }) => api.mailboxMessages(address, { folder, q, before: pageParam }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.nextCursor,
    enabled: address !== '',
  })
}

export function useMessage(id: string | null) {
  return useQuery({
    queryKey: keys.message(id ?? ''),
    queryFn: () => api.message(id!),
    enabled: id !== null,
  })
}

export function useRawSource(id: string, enabled: boolean) {
  return useQuery({ queryKey: keys.raw(id), queryFn: () => api.rawSource(id), enabled })
}

/** Static checks; a stored message never changes, so they never go stale. */
export function useMessageChecks(id: string) {
  return useQuery({ queryKey: keys.checks(id), queryFn: () => api.checks(id), staleTime: Infinity })
}

/** One-time codes and action links; like checks, they never go stale. */
export const extractsQuery = (id: string) => ({
  queryKey: keys.extracts(id),
  queryFn: () => api.extracts(id),
  staleTime: Infinity,
})

export function useMessageExtracts(id: string) {
  return useQuery(extractsQuery(id))
}

export function useCheckLinks(id: string) {
  return useMutation({ mutationFn: () => api.checkLinks(id) })
}

/** Provider webhook settings (the Connection page). */
export function useWebhooks() {
  return useQuery({ queryKey: keys.webhooks, queryFn: api.webhooks, select: (d) => d.items })
}

/** Recent webhook calls; live events refresh them. */
export function useWebhookDeliveries() {
  return useQuery({ queryKey: keys.webhookDeliveries, queryFn: api.webhookDeliveries, select: (d) => d.items })
}

export function useMailboxes() {
  return useQuery({ queryKey: keys.mailboxes, queryFn: api.mailboxes })
}

function useInvalidateMail() {
  const qc = useQueryClient()
  return () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: ['messages'] }),
      qc.invalidateQueries({ queryKey: ['mailbox-messages'] }),
      qc.invalidateQueries({ queryKey: keys.mailboxes }),
    ])
}

export function useDeleteMessage() {
  const invalidate = useInvalidateMail()
  return useMutation({ mutationFn: api.deleteMessage, onSuccess: invalidate })
}

export function useDeleteAllMessages() {
  const invalidate = useInvalidateMail()
  return useMutation({ mutationFn: api.deleteAllMessages, onSuccess: invalidate })
}

export function useUpdateDelivery() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { id: number; seen?: boolean; flagged?: boolean }) =>
      api.updateDelivery(v.id, { seen: v.seen, flagged: v.flagged }),
    onSuccess: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: ['mailbox-messages'] }),
        qc.invalidateQueries({ queryKey: keys.mailboxes }),
      ]),
  })
}

export function useDeleteDelivery() {
  const invalidate = useInvalidateMail()
  return useMutation({ mutationFn: api.deleteDelivery, onSuccess: invalidate })
}

export function useSend() {
  const invalidate = useInvalidateMail()
  return useMutation({ mutationFn: (body: SendRequest) => api.send(body), onSuccess: invalidate })
}

export function useCreateAccount() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: CreateAccountRequest) => api.createAccount(body),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.mailboxes }),
  })
}
