import { Component, type ErrorInfo, type ReactNode } from 'react'
import { ErrorState } from './ErrorState'

interface ErrorBoundaryProps {
  children: ReactNode
  fallback?: ReactNode
  onError?: (error: Error, info: ErrorInfo) => void
}

interface ErrorBoundaryState {
  hasError: boolean
  message: string | null
  componentStack: string | null
}

export class ErrorBoundary extends Component<
  ErrorBoundaryProps,
  ErrorBoundaryState
> {
  state: ErrorBoundaryState = {
    hasError: false,
    message: null,
    componentStack: null,
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return {
      hasError: true,
      message: error.message,
      componentStack: null,
    }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('Tailflow UI render error', error, info)
    this.setState({
      componentStack: info.componentStack || null,
    })
    this.props.onError?.(error, info)
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback
      }

      return (
        <ErrorState
          title="The UI hit a render error."
          description="Refresh the page to recover. The shell-level error boundary caught the failure before the entire app crashed."
          details={
            [this.state.message, this.state.componentStack]
              .filter(Boolean)
              .join('\n\n') || undefined
          }
        />
      )
    }

    return this.props.children
  }
}
