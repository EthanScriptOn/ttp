export default function BrandLogo({ tone = 'light', compact = false, className = '' }) {
  const classes = ['brand-lockup', `brand-lockup-${tone}`, compact ? 'is-compact' : '', className].filter(Boolean).join(' ')

  return (
    <div className={classes} role="img" aria-label="TTP">
      <svg className="brand-symbol" viewBox="0 0 64 64" aria-hidden="true">
        <circle className="brand-symbol-disc" cx="32" cy="32" r="30" />
        <path className="brand-symbol-t" d="M10 12h35v10H34v31H22V22H10z" />
        <path className="brand-symbol-p" d="M33 12h11.5C53.5 12 59 16.3 59 23.5S53.5 35 44.5 35H43v18H33V12z" />
        <path className="brand-symbol-p-counter" d="M43 21h1.5c2.9 0 4.5 1.3 4.5 3.5S47.4 28 44.5 28H43z" />
      </svg>
      <span className="brand-wordmark">
        <strong>TTP</strong>
      </span>
    </div>
  )
}
