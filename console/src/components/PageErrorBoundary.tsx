import { Component, type ReactNode } from 'react'
import { actionClass } from '@/components/classes'
import { Button } from '@/components/ui/button'
import { errorMessage } from '@/lib/errors'

type Props = { children: ReactNode }
type State = { message: string | null }

export class PageErrorBoundary extends Component<Props, State> {
  state: State = { message: null }

  static getDerivedStateFromError(error: unknown): State {
    return { message: errorMessage(error) }
  }

  render() {
    if (this.state.message === null) return this.props.children
    return (
      <div className="flex flex-1 flex-col items-start gap-3 p-6">
        <p className="text-sm" role="alert">
          {this.state.message}
        </p>
        <Button className={actionClass} type="button" onClick={() => this.setState({ message: null })}>
          Try again
        </Button>
      </div>
    )
  }
}
