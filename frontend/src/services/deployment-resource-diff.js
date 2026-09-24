export function buildLineDiff(environmentContent, globalContent) {
  const left = String(environmentContent || '').replaceAll('\r\n', '\n').replaceAll('\r', '\n').split('\n')
  const right = String(globalContent || '').replaceAll('\r\n', '\n').replaceAll('\r', '\n').split('\n')
  const lcs = Array.from({ length: left.length + 1 }, () => Array(right.length + 1).fill(0))
  for (let leftIndex = left.length - 1; leftIndex >= 0; leftIndex -= 1) {
    for (let rightIndex = right.length - 1; rightIndex >= 0; rightIndex -= 1) {
      lcs[leftIndex][rightIndex] = left[leftIndex] === right[rightIndex]
        ? lcs[leftIndex + 1][rightIndex + 1] + 1
        : Math.max(lcs[leftIndex + 1][rightIndex], lcs[leftIndex][rightIndex + 1])
    }
  }

  const rows = []
  let leftIndex = 0
  let rightIndex = 0
  while (leftIndex < left.length || rightIndex < right.length) {
    if (leftIndex < left.length && rightIndex < right.length && left[leftIndex] === right[rightIndex]) {
      rows.push({
        type: 'same',
        environment: left[leftIndex],
        environmentLine: leftIndex + 1,
        global: right[rightIndex],
        globalLine: rightIndex + 1,
      })
      leftIndex += 1
      rightIndex += 1
    } else if (rightIndex < right.length && (leftIndex === left.length || lcs[leftIndex][rightIndex + 1] >= lcs[leftIndex + 1][rightIndex])) {
      rows.push({ type: 'global', environment: '', global: right[rightIndex], globalLine: rightIndex + 1 })
      rightIndex += 1
    } else {
      rows.push({ type: 'environment', environment: left[leftIndex], environmentLine: leftIndex + 1, global: '' })
      leftIndex += 1
    }
  }

  const aligned = []
  for (let index = 0; index < rows.length;) {
    if (rows[index].type === 'same') {
      aligned.push(rows[index])
      index += 1
      continue
    }
    const environmentRows = []
    const globalRows = []
    while (index < rows.length && rows[index].type !== 'same') {
      if (rows[index].type === 'environment') environmentRows.push(rows[index])
      if (rows[index].type === 'global') globalRows.push(rows[index])
      index += 1
    }
    const changedLineCount = Math.max(environmentRows.length, globalRows.length)
    for (let changedIndex = 0; changedIndex < changedLineCount; changedIndex += 1) {
      const environmentRow = environmentRows[changedIndex]
      const globalRow = globalRows[changedIndex]
      aligned.push({
        type: environmentRow && globalRow ? 'changed' : environmentRow ? 'environment' : 'global',
        environment: environmentRow?.environment || '',
        environmentLine: environmentRow?.environmentLine,
        global: globalRow?.global || '',
        globalLine: globalRow?.globalLine,
      })
    }
  }
  return aligned
}
