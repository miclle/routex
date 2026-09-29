import { describe, expect, it } from 'vitest'
import type { Attachment } from '@/types/attachments'
import {
  buildChatAttachmentContent,
  buildGeminiAttachmentParts,
  buildMessagesAttachmentContent,
  buildResponsesAttachmentContent,
} from './playground-attachments'

const attachments: Attachment[] = [
  {
    id: 'obj_01arz3ndektsv4rrffq69g5fav',
    name: 'diagram.png',
    mime: 'image/png',
    size: 8,
    state: 'ready',
    created_at: '2026-09-29T12:00:00Z',
  },
  {
    id: 'obj_01arz3ndektsv4rrffq69g5faw',
    name: 'photo.jpg',
    mime: 'image/jpeg',
    size: 9,
    state: 'ready',
    created_at: '2026-09-29T12:01:00Z',
  },
  {
    id: 'obj_01arz3ndektsv4rrffq69g5fax',
    name: 'report.pdf',
    mime: 'application/pdf',
    size: 10,
    state: 'ready',
    created_at: '2026-09-29T12:02:00Z',
  },
]

const imageReference = 'routex://attachments/obj_01arz3ndektsv4rrffq69g5fav'
const jpegReference = 'routex://attachments/obj_01arz3ndektsv4rrffq69g5faw'
const pdfReference = 'routex://attachments/obj_01arz3ndektsv4rrffq69g5fax'

describe('Playground attachment payloads', () => {
  it('builds OpenAI Chat content with text first and stable media order', () => {
    expect(buildChatAttachmentContent('Describe these files.', attachments)).toEqual([
      { type: 'text', text: 'Describe these files.' },
      { type: 'image_url', image_url: { url: imageReference } },
      { type: 'image_url', image_url: { url: jpegReference } },
      {
        type: 'file',
        file: { file_data: pdfReference, filename: 'report.pdf' },
      },
    ])
  })

  it('builds OpenAI Responses content with text first and native input blocks', () => {
    expect(buildResponsesAttachmentContent('Describe these files.', attachments)).toEqual([
      { type: 'input_text', text: 'Describe these files.' },
      { type: 'input_image', image_url: imageReference },
      { type: 'input_image', image_url: jpegReference },
      { type: 'input_file', file_data: pdfReference, filename: 'report.pdf' },
    ])
  })

  it('builds Anthropic Messages blocks with declared media types', () => {
    expect(buildMessagesAttachmentContent('Describe these files.', attachments)).toEqual([
      { type: 'text', text: 'Describe these files.' },
      {
        type: 'image',
        source: { type: 'base64', media_type: 'image/png', data: imageReference },
      },
      {
        type: 'image',
        source: { type: 'base64', media_type: 'image/jpeg', data: jpegReference },
      },
      {
        type: 'document',
        source: { type: 'base64', media_type: 'application/pdf', data: pdfReference },
      },
    ])
  })

  it('builds Gemini parts with text first and inline data references', () => {
    expect(buildGeminiAttachmentParts('Describe these files.', attachments)).toEqual([
      { text: 'Describe these files.' },
      { inlineData: { mimeType: 'image/png', data: imageReference } },
      { inlineData: { mimeType: 'image/jpeg', data: jpegReference } },
      { inlineData: { mimeType: 'application/pdf', data: pdfReference } },
    ])
  })

  it('never serializes browser File objects or attachment bytes', () => {
    const payloads = [
      buildChatAttachmentContent('Prompt', attachments),
      buildResponsesAttachmentContent('Prompt', attachments),
      buildMessagesAttachmentContent('Prompt', attachments),
      buildGeminiAttachmentParts('Prompt', attachments),
    ]

    for (const payload of payloads) {
      const serialized = JSON.stringify(payload)
      expect(serialized).not.toContain('blob:')
      expect(serialized).not.toContain('data:image')
      expect(serialized).not.toContain('base64,')
      expect(serialized.match(/routex:\/\/attachments\//g)).toHaveLength(3)
    }
  })

  it('rejects unsupported attachment media instead of guessing a native shape', () => {
    const unsupported = [{ ...attachments[0], mime: 'image/gif' }]

    expect(() => buildChatAttachmentContent('Prompt', unsupported)).toThrow(
      'Unsupported attachment MIME type: image/gif',
    )
  })
})
