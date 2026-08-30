export default function BrandLogo({ tone = 'light', compact = false, className = '' }) {
  const classes = ['brand-lockup', `brand-lockup-${tone}`, compact ? 'is-compact' : '', className].filter(Boolean).join(' ')

  return (
    <div className={classes} role="img" aria-label="TTP · The Turbocharged Platform">
      <svg className="brand-symbol" viewBox="0 0 48 48" aria-hidden="true">
        <path className="brand-symbol-plate" d="M14 4h20c6.6 0 10 3.4 10 10v20c0 6.6-3.4 10-10 10H14C7.4 44 4 40.6 4 34V14C4 7.4 7.4 4 14 4Z" />
        <path className="brand-symbol-t" d="M11 11.5h23.5v6h-8.5v19h-6.5v-19H11Z" />
        <path className="brand-symbol-arrow" d="M29 25h7.2l-3.1-3.1 2.4-2.4 7.2 7.2-7.2 7.2-2.4-2.4 3.1-3.1H29Z" />
      </svg>
      <span className="brand-wordmark">
        <strong>TTP</strong>
        {!compact && <span className="brand-full-name">The Turbocharged Platform</span>}
      </span>
    </div>
  )
}
