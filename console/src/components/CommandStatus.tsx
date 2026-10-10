import { commandStatusText, type Command } from '@/lib/commands'

export function CommandStatus({
  command,
  error,
}: {
  command: Command | null
  error: string | null
}) {
  if (error !== null) {
    return (
      <p className="min-w-0 text-sm text-destructive" role="alert">
        {error}
      </p>
    )
  }
  if (command === null) return <span />
  const alert = command.status === 'error'
  return (
    <p
      className={alert ? 'min-w-0 text-sm text-destructive' : 'min-w-0 text-sm text-muted-foreground'}
      role={alert ? 'alert' : 'status'}
    >
      {commandStatusText(command)}
    </p>
  )
}
