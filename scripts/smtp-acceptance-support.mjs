import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { lstat, readdir, readFile } from 'node:fs/promises'
import { join } from 'node:path'

const hash = (bytes) => createHash('sha256').update(bytes).digest('hex')

export function requireRootKey(value) {
  assert.equal(typeof value, 'string', 'Root key must use canonical Base64')
  const bytes = Buffer.from(value, 'base64')
  assert.ok(
    bytes.length === 32 && bytes.toString('base64') === value,
    'Root key must be exactly 32 bytes in canonical Base64',
  )
}

export async function assetsDigest(repo) {
  const root = await lstat(join(repo, 'website/build'))
  assert.ok(root.isDirectory() && !root.isSymbolicLink(), 'Invalid embedded asset root')
  const rows = []
  const walk = async (relative) => {
    for (const name of (await readdir(join(repo, relative))).sort()) {
      const path = `${relative}/${name}`
      const info = await lstat(join(repo, path))
      if (info.isDirectory() && !info.isSymbolicLink()) await walk(path)
      else {
        assert.ok(info.isFile() && !info.isSymbolicLink(), 'Invalid embedded asset')
        rows.push([path, info.mode & 0o777, hash(await readFile(join(repo, path)))])
      }
    }
  }
  await walk('website/build')
  assert.ok(
    rows.length > 1 && rows.some(([path]) => path === 'website/build/index.html'),
    'Embedded asset set incomplete',
  )
  return { digest: hash(Buffer.from(JSON.stringify(rows))), rows }
}

export function requireInputs(expected, current) {
  for (const field of ['source_digest', 'binary_sha256', 'helper_sha256', 'assets_digest']) {
    assert.match(expected?.[field] ?? '', /^[a-f0-9]{64}$/, 'Invalid input receipt')
    assert.equal(current?.[field], expected[field], `Qualified ${field} changed`)
  }
}

export function requireQualification(qualification, inputs) {
  requireInputs(qualification, inputs)
  for (const field of ['mandatory_check', 'ordinary_tests', 'production_build'])
    assert.equal(qualification[field], 'passed', 'Source qualification is incomplete')
}

export async function closePeer(server, milliseconds) {
  assert.ok(Number.isFinite(milliseconds) && milliseconds > 0, 'Peer cleanup deadline expired')
  await new Promise((done, reject) => {
    const timer = setTimeout(() => reject(new Error('Peer cleanup timed out')), milliseconds)
    try {
      server.closeAllConnections()
      server.close((error) => {
        clearTimeout(timer)
        if (error) reject(error)
        else done()
      })
    } catch (error) {
      clearTimeout(timer)
      reject(error)
    }
  })
}

export const sender = 'smtp-sender@example.invalid'
export const recipient = 'smtp-recipient@example.invalid'
export const replyTo = 'smtp-reply@example.invalid'
export const alertKind = 'credential_verification_failure'

export function requireMessage(message, headers, { requestID, operational }) {
  assert.equal(message?.From?.Address, sender, 'Captured sender changed')
  assert.deepEqual(
    message.To?.map((value) => value.Address),
    [recipient],
    'Captured recipient changed',
  )
  assert.deepEqual(
    message.ReplyTo?.map((value) => value.Address),
    [replyTo],
    'Captured reply-to changed',
  )
  assert.equal(message.ReturnPath, sender, 'SMTP MAIL FROM receipt changed')
  const received = headers?.Received
  assert.ok(
    Array.isArray(received) && received.some((value) => value.includes(`for <${recipient}>;`)),
    'SMTP RCPT TO receipt is missing',
  )
  assert.equal(message.MessageID, `routex-${requestID}@routex.invalid`, 'Message identity changed')
  assert.equal(
    message.Subject,
    operational ? 'RouteX operational alert: high' : 'RouteX SMTP test',
    'Captured subject changed',
  )
  assert.equal(
    message.Text?.replace(/\r\n/g, '\n').trimEnd(),
    operational
      ? `Kind: ${alertKind}\nDetail: verification_failed\nSeverity: high`
      : 'This is a RouteX SMTP configuration test.\nAcceptance by the configured relay does not confirm inbox delivery.',
    'Captured server-owned body changed',
  )
  assert.equal(message.HTML, '', 'Unexpected HTML payload')
  assert.equal(message.Attachments?.length ?? 0, 0, 'Unexpected mail attachment')
}

export function requireMailbox(page, expected) {
  assert.equal(page?.total, expected, 'Relay capture count changed')
  assert.equal(page.messages?.length, expected, 'Relay capture is incomplete')
  const ids = page.messages.map((value) => value.ID)
  assert.equal(new Set(ids).size, expected, 'Relay receipt identity duplicated')
  const messageIDs = page.messages.map((value) => value.MessageID)
  assert.equal(new Set(messageIDs).size, expected, 'Duplicate SMTP submission was captured')
  return ids.sort()
}

export function requireNoticeState(notice, { alertID, id, read }) {
  assert.ok(
    notice && notice.id === id && notice.alert_id === alertID,
    'Notification identity changed',
  )
  assert.equal(notice.kind, alertKind, 'Manual verification alert was mixed with inference')
  assert.equal(notice.severity, 'high', 'Notification severity changed')
  assert.equal(notice.detail_code, 'verification_failed', 'Notification detail changed')
  assert.equal(notice.occurrence_count, 1, 'Source occurrence duplicated')
  assert.equal(notice.read, read, 'Notification read state changed')
}

export function requireNotice(notice, expected) {
  requireNoticeState(notice, expected)
  assert.equal(notice.delivery_status, 'accepted', 'Operational SMTP acceptance is missing')
  assert.equal(notice.delivery_code, 'accepted', 'Operational delivery result changed')
  assert.equal(notice.delivery_attempts, 1, 'Operational SMTP submission was repeated')
}

export function requireSnapshot(value, { actor, credentialID, smtpETag, testID, noticeID }) {
  assert.equal(value?.audits?.length, 1, 'Failed verification source census changed')
  const audit = value.audits[0]
  assert.equal(audit.Action, 'credential.verify.failed', 'Manual check source changed')
  assert.equal(audit.ResourceType, 'credential', 'Manual check target kind changed')
  assert.equal(audit.ActorID, actor, 'Verification actor changed')
  assert.equal(audit.ResourceID, credentialID, 'Verification Credential changed')
  assert.equal(value.occurrences?.length, 1, 'Occurrence census changed')
  const occurrence = value.occurrences[0]
  assert.equal(occurrence.SourceID, audit.ID, 'Occurrence is not bound to the exact audit')
  assert.equal(occurrence.SourceType, 'credential_verification', 'Occurrence source changed')
  assert.equal(value.notices?.length, 1, 'Recipient birth census changed')
  const notice = value.notices[0]
  assert.equal(notice.AlertID, occurrence.AlertID, 'Inbox alert source changed')
  assert.equal(notice.ID, noticeID, 'Private inbox identity changed')
  assert.equal(notice.RecipientID, actor, 'Private inbox recipient changed')
  assert.equal(notice.LatestOccurrenceID, occurrence.ID, 'Inbox birth receipt changed')
  assert.equal(notice.Read, true, 'Read receipt is missing')
  assert.equal(value.deliveries?.length, 1, 'Delivery intent duplicated')
  const intent = value.deliveries[0]
  assert.equal(intent.Kind, alertKind, 'Manual-check delivery kind changed')
  assert.equal(intent.Severity, 'high', 'Delivery severity changed')
  assert.equal(intent.DetailCode, 'verification_failed', 'Delivery source detail changed')
  assert.equal(intent.OccurrenceID, occurrence.ID, 'Delivery source changed')
  assert.equal(intent.RecipientID, actor, 'Delivery recipient changed')
  assert.equal(intent.RecipientEmail, recipient, 'Captured preference address changed')
  assert.equal(intent.SMTPETag, smtpETag, 'Delivery transport revision changed')
  assert.equal(intent.Status, 'accepted', 'Immutable delivery did not finish')
  assert.equal(intent.Attempts, 1, 'Delivery intent was submitted again')
  assert.equal(intent.ResultCode, 'accepted', 'Delivery receipt changed')
  assert.ok(intent.CompletedAt, 'Delivery completion time missing')
  assert.equal(value.tests?.length, 1, 'SMTP test intent duplicated')
  assert.equal(value.tests[0].RequestID, testID, 'SMTP test identity changed')
  assert.equal(value.tests[0].ActorID, actor, 'SMTP test actor changed')
  assert.equal(value.tests[0].ConfigETag, smtpETag, 'SMTP test transport changed')
  assert.equal(value.tests[0].Status, 'accepted', 'SMTP test receipt missing')
  assert.equal(value.settings?.length, 1, 'Recipient preference missing')
  assert.equal(value.settings[0].UserID, actor, 'Recipient preference owner changed')
  assert.equal(value.settings[0].EmailHigh, true, 'High-severity opt-in is missing')
  assert.equal(value.settings[0].EmailMedium, false, 'Independent severity preference changed')
  assert.equal(value.settings[0].ExternalEmail, recipient, 'Recipient preference changed')
  assert.deepEqual(
    value.native_counts,
    { keys: 0, calls: 0, attempts: 0 },
    'Manual check created inference facts',
  )
  return intent.ID
}
