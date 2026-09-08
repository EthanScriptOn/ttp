import { useEffect, useMemo, useRef, useState } from 'react'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'

function finiteNumber(value) {
  return typeof value === 'number' && Number.isFinite(value)
}

function numberText(value, precision = 1) {
  if (!finiteNumber(value)) return '--'
  return value.toLocaleString('zh-CN', { maximumFractionDigits: precision, minimumFractionDigits: precision })
}

function valueText(value, line) {
  if (!finiteNumber(value)) return '暂无'
  if (typeof line.format === 'function') return line.format(value)
  return `${numberText(value, line.precision ?? 1)}${line.unit || ''}`
}

function timeText(value, detailed = false) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '--'
  return date.toLocaleString('zh-CN', detailed
    ? { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }
    : { hour: '2-digit', minute: '2-digit' })
}

function niceMaximum(value) {
  if (!finiteNumber(value) || value <= 0) return 1
  const padded = value * 1.12
  const magnitude = 10 ** Math.floor(Math.log10(padded))
  const normalized = padded / magnitude
  const step = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10
  return step * magnitude
}

function validTimestamp(value) {
  const timestamp = new Date(value).getTime()
  return Number.isFinite(timestamp) ? timestamp : null
}

function zoomAroundPointer(plot, event) {
  const scale = plot.scales.x
  const domain = plot.__metricChartDomain
  if (!scale || !domain || !finiteNumber(scale.min) || !finiteNumber(scale.max) || scale.max <= scale.min) return

  const rect = plot.over.getBoundingClientRect()
  const pointer = Math.max(0, Math.min(plot.width, event.clientX - rect.left))
  const value = plot.posToVal(pointer, 'x')
  const factor = event.deltaY < 0 ? 0.8 : 1.25
  const min = value - (value - scale.min) * factor
  const max = value + (scale.max - value) * factor
  plot.setScale('x', {
    min: Math.max(domain.min, min),
    max: Math.min(domain.max, max),
  })
}

export default function MetricChart({ title, description, series = [], lines = [], min = 0, max, height = 224, emptyText = '暂无历史数据' }) {
  const chartRef = useRef(null)
  const chartInstance = useRef(null)
  const [hiddenKeys, setHiddenKeys] = useState(() => new Set())
  const [hoverIndex, setHoverIndex] = useState(null)

  const points = useMemo(() => (Array.isArray(series) ? series
    .map((point) => ({ point, timestamp: validTimestamp(point?.timestamp) }))
    .filter((item) => item.point && item.timestamp !== null) : []), [series])

  const availableLines = useMemo(() => lines.filter((line) => points.some(({ point }) => finiteNumber(point[line.key]))), [lines, points])
  const available = availableLines.length > 0
  const chartHeight = Math.max(260, height + 42)

  useEffect(() => {
    setHiddenKeys((current) => {
      const allowed = new Set(lines.map((line) => line.key))
      const next = new Set([...current].filter((key) => allowed.has(key)))
      return next.size === current.size ? current : next
    })
  }, [lines])

  const chartModel = useMemo(() => {
    if (!available) return null

    const timestamps = points.map(({ timestamp }) => timestamp / 1000)
    const values = []
    points.forEach(({ point }) => availableLines.forEach((line) => {
      if (finiteNumber(point[line.key])) values.push(point[line.key])
    }))
    const low = finiteNumber(min) ? min : 0
    const high = finiteNumber(max) ? max : niceMaximum(Math.max(...values, low + 1))
    const xMin = timestamps[0]
    const xMax = timestamps[timestamps.length - 1] > xMin ? timestamps[timestamps.length - 1] : xMin + 1
    const yValues = availableLines.map((line) => points.map(({ point }) => finiteNumber(point[line.key]) ? point[line.key] : null))

    return {
      data: [timestamps, ...yValues],
      domain: { min: xMin, max: xMax },
      options: {
        width: 0,
        height: chartHeight,
        padding: [8, 12, 0, 0],
        scales: {
          x: { time: true, min: xMin, max: xMax },
          y: { auto: false, min: low, max: high },
        },
        axes: [
          {
            stroke: '#9aa7ba',
            grid: { show: false },
            ticks: { show: false },
            size: 30,
            gap: 8,
            font: '10px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
            values: (plot, values) => values.map((value) => timeText(value * 1000)),
          },
          {
            stroke: '#9aa7ba',
            grid: { stroke: '#edf0f5', width: 1 },
            ticks: { show: false },
            size: 42,
            gap: 6,
            font: '10px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
            values: (plot, values) => values.map((value) => numberText(value, value >= 10 ? 0 : 1)),
          },
        ],
        series: [
          {},
          ...availableLines.map((line, index) => ({
            class: '',
            label: line.label,
            show: !hiddenKeys.has(line.key),
            stroke: line.color,
            width: index === 0 ? 2.2 : 1.7,
            cap: 'round',
            points: { show: false, size: 7, width: 2, stroke: line.color, fill: '#fff' },
            value: (plot, value) => valueText(value, line),
          })),
        ],
        legend: { show: false },
        cursor: {
          x: true,
          y: false,
          drag: { x: true, y: false, setScale: true },
          points: { show: false },
        },
        hooks: {
          setCursor: [(plot) => setHoverIndex(plot.cursor.idx ?? null)],
        },
      },
    }
  }, [available, availableLines, chartHeight, hiddenKeys, max, min, points])

  useEffect(() => {
    if (!chartModel || !chartRef.current) return undefined

    const host = chartRef.current
    const width = Math.max(240, host.clientWidth)
    const plot = new uPlot({ ...chartModel.options, width }, chartModel.data, host)
    plot.__metricChartDomain = chartModel.domain
    chartInstance.current = plot
    setHoverIndex(null)

    const resize = () => {
      if (!chartRef.current || !chartInstance.current) return
      chartInstance.current.setSize({ width: Math.max(240, chartRef.current.clientWidth), height: chartHeight })
    }
    const observer = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(resize) : null
    observer?.observe(host)
    if (!observer) window.addEventListener('resize', resize)

    return () => {
      observer?.disconnect()
      window.removeEventListener('resize', resize)
      plot.destroy()
      chartInstance.current = null
    }
  }, [chartHeight, chartModel])

  const activePoint = hoverIndex === null ? null : points[hoverIndex]

  function toggleLine(key) {
    setHiddenKeys((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else if (next.size < availableLines.length - 1) next.add(key)
      return next
    })
  }

  function resetZoom() {
    const plot = chartInstance.current
    const domain = chartModel?.domain
    if (plot && domain) plot.setScale('x', domain)
  }

  function handleWheel(event) {
    if (!chartInstance.current || !chartModel?.domain || points.length < 2) return
    event.preventDefault()
    zoomAroundPointer(chartInstance.current, event.nativeEvent)
  }

  return <section className="metric-chart">
    <div className="metric-chart-header">
      <div className="metric-chart-title"><strong>{title}</strong>{description && <span>{description}</span>}</div>
      {available && <button type="button" className="metric-chart-reset" onClick={resetZoom}>重置缩放</button>}
    </div>
    {!available
      ? <div className="metric-chart-empty">{emptyText}</div>
      : <>
        <div className="metric-chart-legend" aria-label={`${title}指标`}>
          {availableLines.map((line) => {
            const value = activePoint ? activePoint.point[line.key] : null
            const hidden = hiddenKeys.has(line.key)
            return <button type="button" className={`metric-chart-legend-item${hidden ? ' is-hidden' : ''}`} key={line.key} onClick={() => toggleLine(line.key)} aria-pressed={!hidden}>
              <span className="metric-chart-legend-dot" style={{ backgroundColor: line.color }} />
              <span>{line.label}</span>
              {activePoint && <strong>{valueText(value, line)}</strong>}
            </button>
          })}
          {activePoint && <time dateTime={new Date(activePoint.timestamp).toISOString()}>{timeText(activePoint.timestamp, true)}</time>}
        </div>
        <div className="metric-chart-canvas" style={{ height: `${chartHeight}px` }} onWheel={handleWheel} onDoubleClick={resetZoom}>
          <div ref={chartRef} role="img" aria-label={`${title}折线图`} />
        </div>
      </>}
  </section>
}
