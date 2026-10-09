export type ProviderModelStateInput = { etag: string } & (
  | {
      enabled: boolean
      capability_review_etag?: never
      supports_image_input?: never
      supports_pdf_input?: never
    }
  | {
      capability_review_etag: string
      supports_image_input: boolean
      supports_pdf_input: boolean
      enabled?: boolean
    }
)
