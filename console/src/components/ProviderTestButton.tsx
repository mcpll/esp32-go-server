import { useState } from 'react'
import { actionClass } from '@/components/classes'
import { CommandStatus } from '@/components/CommandStatus'
import { Button } from '@/components/ui/button'
import { useCommand } from '@/hooks/useCommand'
import { commandBody, providerTestPayload } from '@/lib/commands'
import { errorMessage } from '@/lib/errors'
import { configFromDraft, type ProviderDraft, type Stage } from '@/lib/providerForm'

export function ProviderTestButton({
  stage,
  draft,
  agentId,
}: {
  stage: Stage
  draft: ProviderDraft
  agentId: string
}) {
  const command = useCommand()
  const [formError, setFormError] = useState<string | null>(null)
  const busy =
    command.sending || command.command?.status === 'pending' || command.command?.status === 'running'

  function onTest() {
    setFormError(null)
    let config
    try {
      config = configFromDraft(stage, draft)
    } catch (caught) {
      setFormError(errorMessage(caught))
      return
    }
    void command.send(
      commandBody({
        type: 'provider_test',
        agentId,
        payload: providerTestPayload(stage, draft.provider, config),
      }),
    )
  }

  return (
    <div className="flex items-center justify-between gap-3">
      <CommandStatus command={command.command} error={formError ?? command.error} />
      <Button className={actionClass} type="button" disabled={busy} onClick={onTest}>
        {busy ? 'Testing…' : 'Test'}
      </Button>
    </div>
  )
}
