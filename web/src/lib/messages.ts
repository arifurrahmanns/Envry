import type { Delivery, MessageSummary } from '@/lib/api'

/** A row in a message list: a captured message, or one mailbox's copy of it. */
export type ListItem = MessageSummary | Delivery

export function isDelivery(item: ListItem): item is Delivery {
  return 'deliveryId' in item
}
