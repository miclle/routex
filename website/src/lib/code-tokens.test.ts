import { expect, it } from 'vitest'
import { bashTokens, codeTokens, type CodeLanguage } from './code-tokens'

const sources = [
  '',
  '\r\n\t 雪🧩e\u0301\u0000\n',
  '"<script>alert(1)</script>" <img src=x onerror=never()>',
  '# opaque\r\n// opaque\n/* no end',
  '"escaped \\" quotes" \'endless\n下一行',
  "'''multiline\n# not comment\n雪'''\nreturn 1",
  '`template ${never()}\n// not comment`',
  '0 123 0xff 12.25 2e+30\nconst val = true; return None',
  "python3 - <<'END'\nprint('$never')\n# Python not Bash\nEND\ncurl --version\n",
  "'" + 'é"$#\\'.repeat(20_000),
]
for (const language of ['curl', 'python', 'javascript'] as const) {
  it.each(sources)(`preserves exact ${language} presentation text: %j`, (source) => {
    const tokens = codeTokens(source, language)
    expect(tokens.map((token) => token.text).join('')).toBe(source)
    expect(tokens.every((token) => token.text.length > 0)).toBe(true)
  })
}
it.each([
  ['python', 'import os\n# comment\nvalue = "ROUTEX_PASSWORD"\nreturn 2048\n'],
  ['javascript', 'const value = "ROUTEX_API_KEY"; // comment\nreturn 2048;'],
] as const)('marks %s keywords, quoted placeholders, comments and numbers', (language, source) => {
  const tokens = codeTokens(source, language)
  expect(tokens.some((token) => token.className === 'font-semibold')).toBe(true)
  expect(
    tokens.some((token) => token.className === 'text-primary' && token.text.includes('ROUTEX_')),
  ).toBe(true)
  expect(tokens.some((token) => token.className === 'text-primary' && token.text === '2048')).toBe(
    true,
  )
  expect(
    tokens.some(
      (token) => token.className === 'text-muted-foreground' && token.text.includes('comment'),
    ),
  ).toBe(true)
})
it.each([
  ['python', "'''line\n# opaque\nreturn None'''"],
  ['javascript', '`line\n// opaque ${never()}\nconst value`'],
] as [CodeLanguage, string][])('keeps %s multiline quoted bodies opaque', (language, source) => {
  expect(codeTokens(source, language)).toEqual([{ text: source, className: 'text-primary' }])
})
it('preserves original Bash tokens and opaque Team here-document bodies', () => {
  const source = "python3 - <<'END'\nprint('$never')\n# Python not Bash\nEND\ncurl --version\n"
  const tokens = codeTokens(source, 'curl')
  expect(tokens).toEqual(bashTokens(source))
  expect(tokens.find((token) => token.text.includes("print('$never')"))?.className).toBeUndefined()
  expect(
    tokens.some(
      (token) => token.text === '--version' && token.className === 'text-muted-foreground',
    ),
  ).toBe(true)
})
