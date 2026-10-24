import { areaClass } from '@/components/classes'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import type { OpenClawDraft } from '@/lib/openclawForm'

export function OpenClawBlock({
  draft,
  onChange,
}: {
  draft: OpenClawDraft
  onChange: (draft: OpenClawDraft) => void
}) {
  return (
    <fieldset className="grid gap-4">
      <legend className="text-sm font-semibold">OpenClaw</legend>
      <div className="grid gap-2">
        <Label htmlFor="openclaw-allowed">Allowed</Label>
        <div className="flex h-11 items-center">
          <Switch
            id="openclaw-allowed"
            checked={draft.allowed}
            onCheckedChange={(allowed) => onChange({ ...draft, allowed })}
          />
        </div>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="openclaw-enter">Enter phrases</Label>
        <Textarea
          id="openclaw-enter"
          className={areaClass}
          value={draft.enterPhrases}
          onChange={(event) => onChange({ ...draft, enterPhrases: event.target.value })}
          placeholder="One phrase per line"
        />
      </div>
      <div className="grid gap-2">
        <Label htmlFor="openclaw-exit">Exit phrases</Label>
        <Textarea
          id="openclaw-exit"
          className={areaClass}
          value={draft.exitPhrases}
          onChange={(event) => onChange({ ...draft, exitPhrases: event.target.value })}
          placeholder="One phrase per line"
        />
      </div>
      <p className="text-sm text-muted-foreground">
        The endpoint token is the server environment variable ENDPOINT_AUTH_TOKEN.
      </p>
    </fieldset>
  )
}
