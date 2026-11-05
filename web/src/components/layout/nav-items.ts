import { Inbox, Send, Users, type LucideIcon } from 'lucide-react'

export type NavItem = {
  to: string
  label: string
  description: string
  icon: LucideIcon
}

export const navItems: NavItem[] = [
  {
    to: '/sent',
    label: 'Sent',
    description: 'Every message your app sent',
    icon: Send,
  },
  {
    to: '/receivers',
    label: 'Receivers',
    description: 'What each recipient got',
    icon: Users,
  },
  {
    to: '/inbox',
    label: 'Inbox',
    description: 'Your IMAP and POP3 test accounts',
    icon: Inbox,
  },
]
