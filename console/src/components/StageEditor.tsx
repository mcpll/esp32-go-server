import { KnownProviderFields } from '@/components/KnownProviderFields'
import { ProviderSelect } from '@/components/ProviderSelect'
import { RawProviderConfig } from '@/components/RawProviderConfig'
import type { ProviderDraft, Stage } from '@/lib/providerForm'

export function StageEditor({
  stage,
  draft,
  onChange,
}: {
  stage: Stage
  draft: ProviderDraft
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
    </div>
  )
}
