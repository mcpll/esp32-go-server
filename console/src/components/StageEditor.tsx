import { KnownProviderFields } from '@/components/KnownProviderFields'
import { ProviderSelect } from '@/components/ProviderSelect'
import { ProviderTestButton } from '@/components/ProviderTestButton'
import { RawProviderConfig } from '@/components/RawProviderConfig'
import type { ProviderDraft, Stage } from '@/lib/providerForm'

export function StageEditor({
  stage,
  draft,
  agentId,
  onChange,
}: {
  stage: Stage
  draft: ProviderDraft
  agentId: string
  onChange: (draft: ProviderDraft) => void
}) {
  return (
    <div className="grid gap-4">
      <ProviderSelect stage={stage} draft={draft} onChange={onChange} />
      {draft.kind === 'known' ? (
        <KnownProviderFields stage={stage} draft={draft} onChange={onChange} />
      ) : (
        <RawProviderConfig stage={stage} draft={draft} onChange={onChange} />
      )}
      <ProviderTestButton stage={stage} draft={draft} agentId={agentId} />
    </div>
  )
}
