import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { StageEditor } from '@/components/StageEditor'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useAgent, useSaveAgent } from '@/hooks/useAgents'
import { errorMessage } from '@/lib/errors'
import { AGENTS_PATH } from '@/lib/gate'
import { draftFromConfig } from '@/lib/providerForm'

export function AgentForm({ id }: { id: string }) {
  const { data: agent } = useAgent(id)
  const save = useSaveAgent()
  const [name, setName] = useState(agent.name)
  const [prompt, setPrompt] = useState(agent.prompt)
  const [asr, setAsr] = useState(() => draftFromConfig('asr', agent.asrProvider, agent.asrConfig))
  const [llm, setLlm] = useState(() => draftFromConfig('llm', agent.llmProvider, agent.llmConfig))
  const [tts, setTts] = useState(() => draftFromConfig('tts', agent.ttsProvider, agent.ttsConfig))
  const message = save.error === null ? null : errorMessage(save.error)

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    save.mutate({ id: agent.id, name, prompt, asr, llm, tts })
  }

  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-xl font-semibold">{agent.name}</h1>
        <Link className="text-sm text-muted-foreground underline-offset-4 hover:underline" to={AGENTS_PATH}>
          All agents
        </Link>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="agent-name">Name</Label>
        <Input id="agent-name" value={name} onChange={(event) => setName(event.target.value)} />
      </div>
      <div className="grid gap-2">
        <Label htmlFor="agent-prompt">Prompt</Label>
        <Textarea
          id="agent-prompt"
          rows={5}
          value={prompt}
          onChange={(event) => setPrompt(event.target.value)}
        />
      </div>
      <StageEditor title="ASR" stage="asr" draft={asr} onChange={setAsr} />
      <StageEditor title="LLM" stage="llm" draft={llm} onChange={setLlm} />
      <StageEditor title="TTS" stage="tts" draft={tts} onChange={setTts} />
      <p className="text-sm text-muted-foreground">Applies on the next session. API keys stay in the server config.</p>
      {message !== null ? (
        <p className="text-destructive" role="alert">
          {message}
        </p>
      ) : null}
      <Button type="submit" disabled={save.isPending}>
        {save.isPending ? 'Saving…' : 'Save'}
      </Button>
    </form>
  )
}
