import { useQueryClient } from '@tanstack/react-query'
import { useRef, useState, type ChangeEvent } from 'react'
import { Link } from 'react-router-dom'
import { actionClass, controlClass } from '@/components/classes'
import { CommandStatus } from '@/components/CommandStatus'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useCommand } from '@/hooks/useCommand'
import {
  useCreateSpeakerGroup,
  useDeleteSpeakerGroup,
  useDeleteSpeakerSample,
  useSpeakerGroups,
  useSpeakerSamples,
  useUpdateSpeakerGroup,
  useUploadSpeakerSample,
} from '@/hooks/useSpeakers'
import {
  commandBody,
  type Command,
  speakerDeleteGroupPayload,
  speakerDeleteSamplePayload,
  speakerEnrollPayload,
} from '@/lib/commands'
import { errorMessage } from '@/lib/errors'
import { SETTINGS_PATH } from '@/lib/gate'
import type { SpeakerChatMode, SpeakerGroup, SpeakerSample } from '@/lib/speakers'

type VoiceprintPanelProps = {
  agentId: string
  voiceprintEnabled: boolean
  speakerChatMode: SpeakerChatMode
  onVoiceprintEnabledChange: (value: boolean) => void
  onSpeakerChatModeChange: (value: SpeakerChatMode) => void
}

export function VoiceprintPanel({
  agentId,
  voiceprintEnabled,
  speakerChatMode,
  onVoiceprintEnabledChange,
  onSpeakerChatModeChange,
}: VoiceprintPanelProps) {
  const { data: groups } = useSpeakerGroups(agentId)
  const createGroup = useCreateSpeakerGroup(agentId)
  const queryClient = useQueryClient()
  const { command, error: commandError, sending, send } = useCommand()
  const [newGroupName, setNewGroupName] = useState('')
  const [localError, setLocalError] = useState<string | null>(null)

  async function runCommand(body: ReturnType<typeof commandBody>) {
    setLocalError(null)
    const finished = await send(body)
    if (finished?.status === 'done') {
      await queryClient.invalidateQueries({ predicate: (query) => query.queryKey[0] === 'speaker-samples' })
    }
    return finished
  }

  async function onCreateGroup() {
    setLocalError(null)
    try {
      await createGroup.mutateAsync({ agentId, name: newGroupName, prompt: '', voice: '' })
      setNewGroupName('')
    } catch (caught) {
      setLocalError(errorMessage(caught))
    }
  }

  return (
    <section className="grid gap-4 border-t border-border pt-4">
      <div className="grid gap-1">
        <h2 className="text-sm font-medium">Voiceprint</h2>
        <p className="text-sm text-muted-foreground">
          Register who may speak. Turn on the voice server under{' '}
          <Link className="text-primary underline-offset-2 hover:underline" to={SETTINGS_PATH}>
            Settings → Voiceprint
          </Link>
          , then save this agent with voiceprint enabled.
        </p>
      </div>

      <div className="flex items-center justify-between gap-3">
        <Label htmlFor="agent-voiceprint" className="text-sm font-normal">
          Voiceprint for this agent
        </Label>
        <Switch
          id="agent-voiceprint"
          checked={voiceprintEnabled}
          onCheckedChange={onVoiceprintEnabledChange}
        />
      </div>

      <div className="grid gap-2">
        <Label htmlFor="speaker-chat-mode">Chat access</Label>
        <Select
          value={speakerChatMode}
          onValueChange={(value) => onSpeakerChatModeChange(value as SpeakerChatMode)}
          items={{
            off: 'Anyone may chat',
            identified_only: 'Only enrolled voices',
          }}
        >
          <SelectTrigger id="speaker-chat-mode" className={controlClass}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="off">Anyone may chat</SelectItem>
            <SelectItem value="identified_only">Only enrolled voices</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="grid gap-2 sm:grid-cols-[1fr_auto] sm:items-end">
        <div className="grid gap-2">
          <Label htmlFor="new-speaker-group">New speaker group</Label>
          <Input
            id="new-speaker-group"
            className={controlClass}
            placeholder="e.g. Matteo"
            value={newGroupName}
            onChange={(event) => setNewGroupName(event.target.value)}
          />
        </div>
        <Button
          className={actionClass}
          type="button"
          disabled={createGroup.isPending || newGroupName.trim() === ''}
          onClick={() => void onCreateGroup()}
        >
          Add group
        </Button>
      </div>

      {groups.length === 0 ? (
        <p className="text-sm text-muted-foreground">No speaker groups yet.</p>
      ) : (
        <ul className="grid gap-4">
          {groups.map((group) => (
            <SpeakerGroupCard
              key={group.id}
              agentId={agentId}
              group={group}
              busy={sending}
              runCommand={runCommand}
            />
          ))}
        </ul>
      )}

      <CommandStatus command={command} error={localError ?? commandError} />
    </section>
  )
}

function SpeakerGroupCard({
  agentId,
  group,
  busy,
  runCommand,
}: {
  agentId: string
  group: SpeakerGroup
  busy: boolean
  runCommand: (body: ReturnType<typeof commandBody>) => Promise<Command | null>
}) {
  const { data: samples } = useSpeakerSamples(group.id)
  const updateGroup = useUpdateSpeakerGroup(agentId)
  const deleteGroup = useDeleteSpeakerGroup(agentId)
  const upload = useUploadSpeakerSample(group.id)
  const [name, setName] = useState(group.name)
  const [prompt, setPrompt] = useState(group.prompt)
  const [voice, setVoice] = useState(group.voice)
  const [error, setError] = useState<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  const dirty = name !== group.name || prompt !== group.prompt || voice !== group.voice

  async function onSaveGroup() {
    setError(null)
    try {
      await updateGroup.mutateAsync({ id: group.id, name, prompt, voice })
    } catch (caught) {
      setError(errorMessage(caught))
    }
  }

  async function onDeleteGroup() {
    setError(null)
    try {
      const finished = await runCommand(
        commandBody({
          type: 'speaker_delete',
          agentId,
          payload: speakerDeleteGroupPayload(group.id),
        }),
      )
      if (finished?.status !== 'done') return
      await deleteGroup.mutateAsync(group.id)
    } catch (caught) {
      setError(errorMessage(caught))
    }
  }

  async function onPickFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (file === undefined) return
    setError(null)
    try {
      await upload.mutateAsync(file)
    } catch (caught) {
      setError(errorMessage(caught))
    }
  }

  return (
    <li className="grid gap-3 rounded-xl border border-border p-4">
      <div className="grid gap-2 sm:grid-cols-2">
        <div className="grid gap-2">
          <Label>Name</Label>
          <Input className={controlClass} value={name} onChange={(event) => setName(event.target.value)} />
        </div>
        <div className="grid gap-2">
          <Label>TTS voice (optional)</Label>
          <Input className={controlClass} value={voice} onChange={(event) => setVoice(event.target.value)} />
        </div>
      </div>
      <div className="grid gap-2">
        <Label>Extra prompt for this speaker</Label>
        <Textarea
          className={`${controlClass} min-h-20 resize-y`}
          value={prompt}
          onChange={(event) => setPrompt(event.target.value)}
        />
      </div>
      <div className="flex flex-wrap gap-2">
        <Button className={actionClass} type="button" disabled={!dirty || updateGroup.isPending} onClick={() => void onSaveGroup()}>
          Save group
        </Button>
        <Button className={actionClass} type="button" variant="outline" disabled={busy || deleteGroup.isPending} onClick={() => void onDeleteGroup()}>
          Delete group
        </Button>
      </div>

      <div className="grid gap-2 border-t border-border pt-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-sm font-medium">Voice samples (WAV)</p>
          <Button className={actionClass} type="button" disabled={upload.isPending} onClick={() => fileRef.current?.click()}>
            Upload WAV
          </Button>
          <input ref={fileRef} type="file" accept="audio/wav,audio/wave" className="hidden" onChange={(event) => void onPickFile(event)} />
        </div>
        {samples.length === 0 ? (
          <p className="text-sm text-muted-foreground">Upload a short clip, then enroll it on the voice server.</p>
        ) : (
          <ul className="grid gap-2">
            {samples.map((sample) => (
              <SampleRow
                key={sample.id}
                agentId={agentId}
                groupId={group.id}
                sample={sample}
                busy={busy}
                runCommand={runCommand}
              />
            ))}
          </ul>
        )}
      </div>
      {error !== null ? <p className="text-sm text-destructive" role="alert">{error}</p> : null}
    </li>
  )
}

function SampleRow({
  agentId,
  groupId,
  sample,
  busy,
  runCommand,
}: {
  agentId: string
  groupId: string
  sample: SpeakerSample
  busy: boolean
  runCommand: (body: ReturnType<typeof commandBody>) => Promise<Command | null>
}) {
  const remove = useDeleteSpeakerSample(groupId)
  const label = sample.fileName === '' ? sample.id : sample.fileName

  async function onRemove() {
    if (sample.status === 'enrolled') {
      await runCommand(
        commandBody({
          type: 'speaker_delete',
          agentId,
          payload: speakerDeleteSamplePayload(sample.id),
        }),
      )
      return
    }
    await remove.mutateAsync(sample.id)
  }

  async function onEnroll() {
    await runCommand(
      commandBody({
        type: 'speaker_enroll',
        agentId,
        payload: speakerEnrollPayload(sample.id),
      }),
    )
  }

  return (
    <li className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-muted/40 px-3 py-2 text-sm">
      <span className="min-w-0 truncate font-mono text-xs">{label}</span>
      <span className="text-muted-foreground">{statusLabel(sample.status)}</span>
      <div className="flex gap-2">
        {sample.status !== 'enrolled' ? (
          <Button className={actionClass} type="button" size="sm" disabled={busy} onClick={() => void onEnroll()}>
            Enroll
          </Button>
        ) : null}
        <Button className={actionClass} type="button" size="sm" variant="outline" disabled={busy || remove.isPending} onClick={() => void onRemove()}>
          Remove
        </Button>
      </div>
    </li>
  )
}

function statusLabel(status: SpeakerSample['status']): string {
  switch (status) {
    case 'pending':
      return 'Not enrolled'
    case 'enrolled':
      return 'Enrolled'
    case 'error':
      return 'Enroll failed'
  }
}
