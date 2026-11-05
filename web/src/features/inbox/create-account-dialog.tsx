import { useState, type FormEvent } from 'react'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useCreateAccount } from '@/lib/queries'

export function CreateAccountDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated?: (address: string) => void
}) {
  const create = useCreateAccount()
  const [address, setAddress] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const addr = address.trim().toLowerCase()
    create.mutate(
      { address: addr, displayName: displayName.trim() || undefined, password: password || undefined },
      {
        onSuccess: () => {
          toast.success(`Account ${addr} created`)
          onOpenChange(false)
          setAddress('')
          setDisplayName('')
          setPassword('')
          onCreated?.(addr)
        },
        onError: (err) => toast.error("Couldn't create account", { description: err.message }),
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit} className="grid gap-5">
          <DialogHeader>
            <DialogTitle>New test account</DialogTitle>
            <DialogDescription>
              Log in to IMAP or POP3 with this address to read its mail from a real client.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="account-address">Email address</Label>
            <Input
              id="account-address"
              type="email"
              required
              autoFocus
              placeholder="dev@test.local"
              value={address}
              onChange={(e) => setAddress(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="account-name">Display name</Label>
            <Input
              id="account-name"
              placeholder="Optional"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="account-password">Password</Label>
            <Input
              id="account-password"
              type="password"
              autoComplete="new-password"
              placeholder="Optional; any password works unless strict auth is on"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending && <Loader2 className="animate-spin" />}
              Create account
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
