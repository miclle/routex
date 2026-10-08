export type CodeToken = { text: string; className?: string }
export type CodeLanguage = 'curl' | 'python' | 'javascript'

// Presentation only: preserve every character and leave here-document bodies opaque.
// Tokens are rendered as React text nodes, never interpreted as HTML or shell input.
export function bashTokens(source: string): CodeToken[] {
  const tokens: CodeToken[] = []
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

const keywords = {
  python: new Set([
    'and',
    'as',
    'assert',
    'async',
    'await',
    'break',
    'class',
    'continue',
    'def',
    'del',
    'elif',
    'else',
    'except',
    'False',
    'finally',
    'for',
    'from',
    'if',
    'import',
    'in',
    'is',
    'lambda',
    'None',
    'not',
    'or',
    'pass',
    'raise',
    'return',
    'True',
    'try',
    'while',
    'with',
    'yield',
  ]),
  javascript: new Set([
    'async',
    'await',
    'break',
    'case',
    'catch',
    'class',
    'const',
    'continue',
    'default',
    'delete',
    'do',
    'else',
    'export',
    'extends',
    'false',
    'finally',
    'for',
    'function',
    'if',
    'import',
    'in',
    'instanceof',
    'let',
    'new',
    'null',
    'of',
    'return',
    'super',
    'switch',
    'throw',
    'true',
    'try',
    'typeof',
    'undefined',
    'var',
    'void',
    'while',
    'yield',
  ]),
}

// Presentation only, not a parser: quoted/template bodies remain opaque. Preserve
// all source text, including malformed input, and render only inert React text.
export function codeTokens(source: string, language: CodeLanguage): CodeToken[] {
  if (language === 'curl') return bashTokens(source)
  const tokens: CodeToken[] = []
  const word = /[A-Za-z_$][A-Za-z_0-9$]*/y
  const number = /(?:0[xX][0-9a-fA-F]+|\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/y
  let offset = 0
  function append(text: string, className?: string) {
    const previous = tokens.at(-1)
    if (previous && previous.className === className) previous.text += text
    else tokens.push({ text, ...(className ? { className } : {}) })
  }
  while (offset < source.length) {
    const start = offset
    const char = source[offset]
    const lineComment = language === 'python' ? char === '#' : source.startsWith('//', offset)
    if (lineComment) {
      const end = source.indexOf('\n', offset)
      offset = end < 0 ? source.length : end
      append(source.slice(start, offset), 'text-muted-foreground')
    } else if (language === 'javascript' && source.startsWith('/*', offset)) {
      const end = source.indexOf('*/', offset + 2)
      offset = end < 0 ? source.length : end + 2
      append(source.slice(start, offset), 'text-muted-foreground')
    } else if (char === "'" || char === '"' || (language === 'javascript' && char === '`')) {
      const delimiter =
        language === 'python' && source.startsWith(char.repeat(3), offset) ? char.repeat(3) : char
      offset += delimiter.length
      while (offset < source.length) {
        if (source[offset] === '\\') offset = Math.min(offset + 2, source.length)
        else if (source.startsWith(delimiter, offset)) {
          offset += delimiter.length
          break
        } else offset++
      }
      append(source.slice(start, offset), 'text-primary')
    } else {
      const pattern = /[0-9]/.test(char) ? number : word
      pattern.lastIndex = offset
      const match = pattern.exec(source)
      if (match) {
        offset += match[0].length
        append(
          match[0],
          pattern === number
            ? 'text-primary'
            : keywords[language].has(match[0])
              ? 'font-semibold'
              : undefined,
        )
      } else {
        append(char)
        offset++
      }
    }
  }
  return tokens
}
