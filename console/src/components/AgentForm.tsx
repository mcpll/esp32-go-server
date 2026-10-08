import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { actionClass, areaClass, controlClass } from '@/components/classes'
import { MemoryModeField } from '@/components/MemoryModeField'
import { StageEditor } from '@/components/StageEditor'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useAgent, useSaveAgent } from '@/hooks/useAgents'
import { useSettings } from '@/hooks/useSettings'
import { errorMessage } from '@/lib/errors'
import { AGENTS_PATH } from '@/lib/gate'
import { redisEnabledFromSettings, type MemoryMode } from '@/lib/memoryMode'
import { draftFromConfig, type Stage } from '@/lib/providerForm'
import { cn } from '@/lib/utils'

const STAGES: { id: Stage; label: string }[] = [
  { id: 'asr', label: 'ASR' },
  { id: 'llm', label: 'LLM' },
  { id: 'tts', label: 'TTS' },
]

export function AgentForm({ id }: { id: string }) {
  const { data: agent } = useAgent(id)
  const { data: settings } = useSettings()
  const save = useSaveAgent()
  const redisEnabled = redisEnabledFromSettings(settings)
  const [name, setName] = useState(agent.name)
  const [prompt, setPrompt] = useState(agent.prompt)
  const [memoryMode, setMemoryMode] = useState<MemoryMode>(agent.memoryMode)
  const [asr, setAsr] = useState(() => draftFromConfig('asr', agent.asrProvider, agent.asrConfig))
  const [llm, setLlm] = useState(() => draftFromConfig('llm', agent.llmProvider, agent.llmConfig))
  const [tts, setTts] = useState(() => draftFromConfig('tts', agent.ttsProvider, agent.ttsConfig))
  const [stage, setStage] = useState<Stage>('asr')
  const message = save.error === null ? null : errorMessage(save.error)
  const drafts = { asr, llm, tts }
  const setDraft = { asr: setAsr, llm: setLlm, tts: setTts }

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    save.mutate({ id: agent.id, name, prompt, memoryMode, asr, llm, tts })
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-border px-3">
        <Link
          to={AGENTS_PATH}
          aria-label="All agents"
          className="flex size-9 items-center justify-center rounded-full text-muted-foreground hover:bg-muted focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          <ArrowLeft className="size-4" />
        </Link>
        <h1 className="min-w-0 flex-1 truncate text-sm font-semibold">{agent.name}</h1>
        {message !== null ? (
          <p className="max-w-48 truncate text-sm text-destructive" role="alert">
            {message}
          </p>
        ) : null}
        <Button className={actionClass} type="submit" form="agent-form" disabled={save.isPending}>
          {save.isPending ? 'Saving…' : 'Save'}
        </Button>
      </header>
      <form
        id="agent-form"
        className="flex min-h-0 flex-1 flex-col overflow-y-auto lg:grid lg:grid-cols-2 lg:overflow-hidden"
        onSubmit={onSubmit}
      >
        <section className="flex flex-col gap-4 px-6 py-6 lg:min-h-0 lg:overflow-y-auto lg:border-r lg:border-border">
          <div className="grid gap-2">
            <Label htmlFor="agent-name">Name</Label>
            <Input
              id="agent-name"
              className={controlClass}
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </div>
          <div className="grid min-h-64 flex-1 grid-rows-[auto_minmax(16rem,1fr)] gap-2">
            <Label htmlFor="agent-prompt">Prompt</Label>
            <Textarea
              id="agent-prompt"
              className={`${areaClass} h-full min-h-64 resize-none field-sizing-fixed`}
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
            />
          </div>
          <MemoryModeField mode={memoryMode} redisEnabled={redisEnabled} onChange={setMemoryMode} />
        </section>
        <section className="flex flex-col gap-4 border-t border-border px-6 py-6 lg:min-h-0 lg:overflow-y-auto lg:border-t-0">
          <div role="tablist" aria-label="Pipeline" className="flex gap-1 rounded-full bg-muted p-1">
            {STAGES.map((item) => (
              <button
                key={item.id}
                type="button"
                role="tab"
                id={`stage-tab-${item.id}`}
                aria-selected={stage === item.id}
                aria-controls={`stage-panel-${item.id}`}
                className={cn(
                  'flex-1 rounded-full px-3 py-1.5 text-sm font-medium focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50',
                  stage === item.id ? 'bg-card text-foreground shadow-soft' : 'text-muted-foreground hover:text-foreground',
                )}
                onClick={() => setStage(item.id)}
              >
                {item.label}
              </button>
            ))}
          </div>
          <div role="tabpanel" id={`stage-panel-${stage}`} aria-labelledby={`stage-tab-${stage}`}>
            <StageEditor stage={stage} draft={drafts[stage]} onChange={setDraft[stage]} />
          </div>
          <p className="text-sm text-muted-foreground">Applies on the next session. API keys stay in the server config.</p>
        </section>
      </form>
    </div>
  )
}
