import { useState, type FormEvent } from 'react'
import { actionClass, controlClass } from '@/components/classes'
import { CommandStatus } from '@/components/CommandStatus'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { useCommand } from '@/hooks/useCommand'
import { commandBody, injectPayload } from '@/lib/commands'
import type { Device } from '@/lib/records'

export function SendMessage({ device }: { device: Device }) {
  const [text, setText] = useState('')
  const [speak, setSpeak] = useState(true)
  const command = useCommand()
  const busy =
    command.sending || command.command?.status === 'pending' || command.command?.status === 'running'
  const messageId = `message-${device.id}`
  const speakId = `speak-${device.id}`

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const message = text.trim()
    if (message === '') return
    void command.send(
      commandBody({
        type: 'inject_msg',
        deviceId: device.id,
        payload: injectPayload(device.deviceId, message, speak),
      }),
    )
  }

  return (
    <form className="grid gap-3 border-t border-border pt-3" onSubmit={onSubmit}>
      <div className="grid gap-2">
        <Label htmlFor={messageId}>Message</Label>
        <Input
          id={messageId}
          className={controlClass}
          value={text}
          placeholder={speak ? 'Text the device will speak' : 'Text to send into the chat'}
          onChange={(event) => setText(event.target.value)}
        />
      </div>
      <div className="flex items-center justify-between gap-4">
        <Label htmlFor={speakId}>Speak this text</Label>
        <Switch id={speakId} checked={speak} onCheckedChange={setSpeak} />
      </div>
      <div className="flex items-center justify-between gap-3">
        <CommandStatus command={command.command} error={command.error} />
        <Button className={actionClass} type="submit" disabled={busy || text.trim() === ''}>
          {busy ? 'Sending…' : 'Send'}
        </Button>
      </div>
    </form>
  )
}
