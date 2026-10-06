type BashToken = { text: string; className?: string }

// Presentation only: preserve every character and leave here-document bodies opaque.
// Tokens are rendered as React text nodes, never interpreted as HTML or shell input.
export function bashTokens(source: string): BashToken[] {
  const tokens: BashToken[] = []
  const word = /[A-Za-z_][A-Za-z_0-9]*/y
  const option = /--?[A-Za-z][A-Za-z_0-9-]*/y
  const variable = /\$(?:[A-Za-z_][A-Za-z_0-9]*|\{[A-Za-z_][A-Za-z_0-9]*\})/y
  let offset = 0
  let heredoc: string | undefined
  function append(text: string, className?: string) {
    const previous = tokens.at(-1)
    if (previous && previous.className === className) previous.text += text
    else tokens.push({ text, ...(className ? { className } : {}) })
  }
  while (offset < source.length) {
    const lineEnd = source.indexOf('\n', offset)
    const end = lineEnd < 0 ? source.length : lineEnd + 1
    const line = source.slice(offset, end)
    if (heredoc) {
      append(line)
      if (line.replace(/\r?\n$/, '') === heredoc) heredoc = undefined
      offset = end
      continue
    }
    const delimiter = /<<['"]?([A-Za-z_][A-Za-z_0-9]*)['"]?/.exec(line)?.[1]
    while (offset < end) {
      const start = offset
      const char = source[offset]
      if (char === '#' && (offset === 0 || /\s/.test(source[offset - 1]))) {
        append(source.slice(offset, end), 'text-muted-foreground')
        offset = end
      } else if (char === "'" || char === '"') {
        offset++
        while (offset < source.length) {
          const next = source[offset++]
          if (next === '\\' && char === '"') offset = Math.min(offset + 1, source.length)
          else if (next === char) break
        }
        append(source.slice(start, offset), 'text-primary')
      } else {
        const pattern = char === '$' ? variable : char === '-' ? option : word
        pattern.lastIndex = offset
        const match = pattern.exec(source)
        if (match) {
          offset += match[0].length
          const className =
            pattern === variable
              ? 'text-primary font-medium'
              : pattern === option
                ? 'text-muted-foreground'
                : ['curl', 'python3', 'export', 'read', 'printf'].includes(match[0])
                  ? 'font-semibold'
                  : undefined
          append(match[0], className)
        } else {
          append(char)
          offset++
        }
      }
    }
    if (delimiter) heredoc = delimiter
  }
  return tokens
}
