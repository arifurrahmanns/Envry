import { Link, Navigate, Route, Routes, useParams } from 'react-router'
import { Compass } from 'lucide-react'

import { AppShell } from '@/components/layout/app-shell'
import { PageHeader } from '@/components/layout/page-header'
import { EmptyState } from '@/components/mail/empty-state'
import { Button } from '@/components/ui/button'
import { AccountMailboxPage, InboxPage } from '@/features/inbox/inbox-page'
import { ReceiverMailboxPage, ReceiversPage } from '@/features/receivers/receivers-page'
import { SentPage } from '@/features/sent/sent-page'

export default function App() {
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<Navigate to="/sent" replace />} />
        <Route path="sent" element={<SentPage />} />
        <Route path="receivers" element={<ReceiversPage />} />
        <Route path="receivers/:address" element={<ReceiverMailboxPage />} />
        <Route path="inbox" element={<InboxPage />} />
        <Route path="inbox/:account" element={<AccountMailboxPage />} />
        <Route path="message/:id" element={<MessageRedirect />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}

function MessageRedirect() {
  const { id = '' } = useParams()
  return <Navigate to={`/sent?m=${encodeURIComponent(id)}`} replace />
}

function NotFound() {
  return (
    <>
      <PageHeader title="Not found" />
      <EmptyState
        icon={Compass}
        title="This page doesn't exist"
        action={
          <Button asChild variant="outline" size="sm">
            <Link to="/sent">Go to Sent</Link>
          </Button>
        }
      />
    </>
  )
}
