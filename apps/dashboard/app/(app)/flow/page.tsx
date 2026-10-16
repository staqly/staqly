import { redirect } from 'next/navigation'

// Flow is part of the inbox now: its steps are the inbox's tabs.
export default function Page() {
  redirect('/inbox')
}
