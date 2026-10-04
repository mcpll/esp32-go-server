import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { KnownProviderFields } from '@/components/KnownProviderFields'
import { ProviderSelect } from '@/components/ProviderSelect'
import { RawProviderConfig } from '@/components/RawProviderConfig'
import type { ProviderDraft, Stage } from '@/lib/providerForm'

export function StageEditor({
  title,
  stage,
  draft,
  onChange,
}: {
  title: string
  stage: Stage
  draft: ProviderDraft
  onChange: (draft: ProviderDraft) => void
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4">
        <ProviderSelect stage={stage} draft={draft} onChange={onChange} />
        {draft.kind === 'known' ? (
          <KnownProviderFields stage={stage} draft={draft} onChange={onChange} />
        ) : (
          <RawProviderConfig stage={stage} draft={draft} onChange={onChange} />
        )}
      </CardContent>
    </Card>
  )
}
