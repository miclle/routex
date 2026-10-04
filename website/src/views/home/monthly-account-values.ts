import type { MonthlyAccount } from '@/types/overview'

// Only the bounded progress-bar value becomes a Number. All quota arithmetic
// and displayed counts keep their exact integer/decimal strings.
export function tokenPercentage(account: MonthlyAccount) {
  const usage = account.usage
  if (!usage || !usage.covered || usage.tokens_unknown !== '0' || account.tokens_month === null)
    return null
  const limit = BigInt(account.tokens_month)
  if (limit === 0n) return null
  const basisPoints = (BigInt(usage.tokens_used) * 10000n) / limit
  return {
    whole: basisPoints / 100n,
    fraction: basisPoints % 100n,
    bar: Number(basisPoints > 10000n ? 10000n : basisPoints) / 100,
  }
}

export function exactInteger(value: string, language: string) {
  return new Intl.NumberFormat(language).format(BigInt(value))
}
export function exactDecimal(value: string, language: string) {
  const [whole, fraction] = value.split('.')
  const separator =
    new Intl.NumberFormat(language).formatToParts(1.1).find((part) => part.type === 'decimal')
      ?.value ?? '.'
  return exactInteger(whole, language) + (fraction === undefined ? '' : separator + fraction)
}
