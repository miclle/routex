import { expect, it } from 'vitest'
import { bashTokens } from './bash-tokens'

it.each([
  '',
  'curl https://example.test/v1 \\\n  -H "Authorization: Bearer $ROUTEX_API_KEY" \\\n  -d \'{"model":"模型<safe>","text":"a\\nb"}\'',
  '# comment\r\nexport KEY=${ROUTEX_API_KEY}\r\nread -r value\n',
  'printf "escaped \\" quote" # tail\n',
  "curl -d 'a'\\''b'",
  "curl -d 'unterminated\n下一行",
  'curl <script>alert("unsafe")</script> & $(never_execute) `never_execute`',
  "python3 - <<'ROUTEX_TEAM_EXAMPLE'\nprint('<script>')\n# opaque Python body\nROUTEX_TEAM_EXAMPLE\n",
])('preserves exact source bytes for presentation: %j', (source) => {
  const tokens = bashTokens(source)
  expect(tokens.map((token) => token.text).join('')).toBe(source)
  expect(tokens.every((token) => token.text.length > 0)).toBe(true)
})
it('highlights commands, options, strings, variables and comments without consuming whitespace', () => {
  const source = 'curl --request POST "$URL" $ROUTEX_API_KEY # comment\n'
  const tokens = bashTokens(source)
  expect(tokens.find((token) => token.text === 'curl')?.className).toBe('font-semibold')
  expect(tokens.find((token) => token.text === '--request')?.className).toBe(
    'text-muted-foreground',
  )
  expect(tokens.find((token) => token.text === '"$URL"')?.className).toBe('text-primary')
  expect(tokens.find((token) => token.text === '$ROUTEX_API_KEY')?.className).toBe(
    'text-primary font-medium',
  )
  expect(tokens.map((token) => token.text).join('')).toBe(source)
})
it('leaves the Team authentication here-document body opaque and resumes Bash after its exact terminator', () => {
  const body = 'print("$never")\n# Python, not Bash\n'
  const tokens = bashTokens(`python3 - <<'END'\n${body}END\ncurl --version\n`)
  const opaque = tokens.find((token) => token.text.includes(body))!
  expect(opaque.className).toBeUndefined()
  expect(tokens.at(-2)?.text).toBe('--version')
})
it('handles a long unclosed quoted input without dropping or repeatedly scanning characters', () => {
  const source = "curl -d '" + 'é"$#\\'.repeat(20000)
  expect(
    bashTokens(source)
      .map((token) => token.text)
      .join(''),
  ).toBe(source)
})
