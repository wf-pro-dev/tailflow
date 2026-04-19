interface LiveUpdatesBannerProps {
  message: string
}

export function LiveUpdatesBanner(props: LiveUpdatesBannerProps) {
  return (
    <div className="border-b border-amber-200 bg-amber-50 px-6 py-2">
      <p className="text-[12px] font-medium uppercase tracking-[0.18em] text-amber-700">
        {props.message}
      </p>
    </div>
  )
}
