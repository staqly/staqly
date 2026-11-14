import { IntegrationsRedirect } from '@/components/integrations/pages'

// The per-client pages became tabs of /integrations; old links land on the right tab.
export default function Page() {
  return <IntegrationsRedirect tab="n8n" />
}
