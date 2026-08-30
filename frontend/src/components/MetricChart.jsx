import { DatePicker, Select } from 'antd'
import { DataZoomComponent, GridComponent, LegendComponent, ToolboxComponent, TooltipComponent } from 'echarts/components'
import { useEffect, useMemo, useRef, useState } from 'react'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart } from 'echarts/charts'
import * as echarts from 'echarts/core'

echarts.use([LineChart, GridComponent, TooltipComponent, LegendComponent, DataZoomComponent, ToolboxComponent, CanvasRenderer])

function finiteNumber(value) {
  return typeof value === 'number' && Number.isFinite(value)
}

function timeText(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '--'
  return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

export default function MetricChart({ title, description, series = [], lines = [], height = 300, emptyText = '刷新后开始记录' }) {
  const chartElementRef = useRef(null)
  const chartInstanceRef = useRef(null)
  const [selectedKeys, setSelectedKeys] = useState(() => lines.slice(0, 2).map((line) => line.key))
  const [timeRange, setTimeRange] = useState('1h')
  const [customRange, setCustomRange] = useState(null)
  const points = useMemo(() => (Array.isArray(series) ? series.filter((point) => point?.timestamp) : []), [series])
  const visiblePoints = useMemo(() => {
    if (timeRange === 'custom') {
      const start = customRange?.[0]?.valueOf()
      const end = customRange?.[1]?.valueOf()
      if (!start || !end) return []
      return points.filter((point) => {
        const timestamp = new Date(point.timestamp).getTime()
        return timestamp >= start && timestamp <= end
      })
    }
    const duration = ({ '15m': 15, '1h': 60, '6h': 360, '24h': 1440 })[timeRange] || 60
    const start = Date.now() - duration * 60 * 1000
    return points.filter((point) => new Date(point.timestamp).getTime() >= start)
  }, [customRange, points, timeRange])
  const availableLines = useMemo(() => lines.filter((line) => points.some((point) => finiteNumber(point[line.key]))), [lines, points])
  const selectedLines = useMemo(() => availableLines.filter((line) => selectedKeys.includes(line.key)), [availableLines, selectedKeys])

  useEffect(() => {
    setSelectedKeys((current) => {
      const valid = current.filter((key) => lines.some((line) => line.key === key))
      if (valid.length) return valid
      return lines.slice(0, 2).map((line) => line.key)
    })
  }, [lines])

  useEffect(() => {
    if (!chartElementRef.current) return undefined
    const chart = echarts.init(chartElementRef.current, null, { renderer: 'canvas' })
    chartInstanceRef.current = chart
    const observer = new ResizeObserver(() => {
      if (!chart.isDisposed()) chart.resize()
    })
    observer.observe(chartElementRef.current)
    return () => {
      observer.disconnect()
      chart.dispose()
      chartInstanceRef.current = null
    }
  }, [])

  useEffect(() => {
    const chart = chartInstanceRef.current
    if (!chart || chart.isDisposed()) return
    const selectedNames = selectedLines.map((line) => line.label)
    const option = {
      animation: false,
      color: selectedLines.map((line) => line.color),
      grid: { top: 58, right: 20, bottom: 62, left: 48, containLabel: false },
      legend: {
        top: 8,
        left: 12,
        right: 118,
        type: 'scroll',
        itemWidth: 10,
        itemHeight: 6,
        itemGap: 13,
        textStyle: { color: '#657278', fontSize: 11 },
        data: availableLines.map((line) => line.label),
        selected: Object.fromEntries(availableLines.map((line) => [line.label, selectedNames.includes(line.label)])),
        selectedMode: 'multiple',
      },
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'cross', lineStyle: { color: '#9aaba4', type: 'dashed' } },
        backgroundColor: 'rgba(255,255,255,.98)',
        borderColor: '#d8e7df',
        borderWidth: 1,
        textStyle: { color: '#1f2a2e', fontSize: 11 },
        formatter(params) {
          if (!params?.length) return ''
          const timestamp = params[0].value?.[0]
          const rows = params.map((item) => {
            const line = lines.find((candidate) => candidate.label === item.seriesName)
            const value = item.value?.[1]
            const formatted = finiteNumber(value)
              ? `${value.toLocaleString('zh-CN', { maximumFractionDigits: line?.precision ?? 0 })}${line?.unit || ''}`
              : '--'
            return `<div style="display:flex;align-items:center;gap:6px;min-width:150px"><i style="width:7px;height:7px;border-radius:50%;background:${line?.color || '#8a9993'}"></i><span>${item.seriesName}</span><strong style="margin-left:auto">${formatted}</strong></div>`
          }).join('')
          return `<div style="margin-bottom:5px;color:#657278">${timeText(timestamp)}</div>${rows}`
        },
      },
      toolbox: {
        right: 9,
        top: 3,
        itemSize: 14,
        iconStyle: { borderColor: '#7c9188' },
        emphasis: { iconStyle: { borderColor: '#167c72' } },
        feature: {
          dataZoom: { yAxisIndex: 'none', title: { zoom: '框选缩放', back: '还原缩放' } },
          restore: { title: '还原' },
          saveAsImage: { title: '导出图片', pixelRatio: 2 },
        },
      },
      xAxis: {
        type: 'time',
        boundaryGap: false,
        axisLine: { lineStyle: { color: '#dfe8e4' } },
        axisTick: { show: false },
        axisLabel: { color: '#9aa7a1', fontSize: 10, hideOverlap: true },
        splitLine: { show: false },
      },
      yAxis: {
        type: 'value',
        min: 0,
        minInterval: 1,
        splitNumber: 4,
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: '#9aa7a1', fontSize: 10 },
        splitLine: { lineStyle: { color: '#edf1ef' } },
      },
      dataZoom: [
        { type: 'inside', xAxisIndex: 0, filterMode: 'none', zoomOnMouseWheel: true, moveOnMouseMove: true, moveOnMouseWheel: true },
        {
          type: 'slider',
          xAxisIndex: 0,
          bottom: 15,
          height: 15,
          borderColor: '#e3ece7',
          backgroundColor: '#f5f8f6',
          fillerColor: 'rgba(47,143,104,.18)',
          handleStyle: { color: '#5db08a', borderColor: '#5db08a' },
          moveHandleStyle: { color: '#9bd5b9' },
          textStyle: { color: '#9aa7a1', fontSize: 9 },
          dataBackground: { lineStyle: { color: '#b9daca' }, areaStyle: { color: '#e8f4ee' } },
        },
      ],
      series: availableLines.map((line) => ({
        name: line.label,
        type: 'line',
        smooth: true,
        showSymbol: points.length < 20,
        symbol: 'circle',
        symbolSize: 6,
        connectNulls: false,
        emphasis: { focus: 'series' },
        lineStyle: { width: 2, color: line.color },
        itemStyle: { color: line.color, borderColor: '#fff', borderWidth: 1 },
        data: visiblePoints.map((point) => {
          const time = new Date(point.timestamp).getTime()
          return [time, finiteNumber(point[line.key]) ? point[line.key] : null]
        }),
      })),
    }
    chart.setOption(option, true)
  }, [availableLines, description, height, lines, selectedLines, visiblePoints])

  const handleLegendChange = (event) => {
    const line = lines.find((candidate) => candidate.label === event.name)
    if (!line) return
    setSelectedKeys((current) => event.selected[event.name]
      ? [...new Set([...current, line.key])]
      : current.filter((key) => key !== line.key))
  }

  useEffect(() => {
    const chart = chartInstanceRef.current
    if (!chart || chart.isDisposed()) return undefined
    chart.on('legendselectchanged', handleLegendChange)
    return () => {
      if (!chart.isDisposed()) chart.off('legendselectchanged', handleLegendChange)
    }
  })

  return <section className="metric-chart">
    <div className="metric-chart-header">
      <div className="metric-chart-title"><strong>{title}</strong>{description && <span>{description}</span>}</div>
      <div className="metric-chart-filters">
        <Select
          className="metric-chart-range-select"
          size="small"
          value={timeRange}
          options={[{ value: '15m', label: '近15分钟' }, { value: '1h', label: '近1小时' }, { value: '6h', label: '近6小时' }, { value: '24h', label: '近24小时' }, { value: 'custom', label: '自定义时间' }]}
          onChange={(value) => { setTimeRange(value); if (value !== 'custom') setCustomRange(null) }}
          aria-label="时间范围"
        />
        {timeRange === 'custom' && <DatePicker.RangePicker
          className="metric-chart-custom-range"
          size="small"
          showTime={{ format: 'HH:mm' }}
          format="MM-DD HH:mm"
          value={customRange}
          onChange={setCustomRange}
          placeholder={['开始时间', '结束时间']}
          aria-label="自定义时间范围"
        />}
        <Select
          className="metric-chart-select"
          mode="multiple"
          size="small"
          value={selectedKeys}
          options={lines.map((line) => ({ value: line.key, label: line.label }))}
          onChange={setSelectedKeys}
          maxTagCount={2}
          placeholder="选择指标"
          aria-label="选择指标"
        />
      </div>
    </div>
    <div className="metric-chart-stage">
      <div ref={chartElementRef} className="metric-chart-canvas" style={{ height }} role="img" aria-label={`${title}折线图`} />
      {(!visiblePoints.length || !availableLines.length || !selectedLines.length) && <div className="metric-chart-empty">{!selectedLines.length && availableLines.length ? '至少选择一个指标' : timeRange === 'custom' && !customRange ? '请选择起止时间' : emptyText}</div>}
    </div>
  </section>
}
