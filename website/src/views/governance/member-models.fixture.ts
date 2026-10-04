import type { MemberModelRow, MemberModelsWorkspace } from '@/types/member-models'
export const modelsActor = 'usr_01aaaaaaaaaaaaaaaaaaaaaaaa'
export const modelsTarget = 'usr_01bbbbbbbbbbbbbbbbbbbbbbbb'
export const firstModel = 'mdl_01aaaaaaaaaaaaaaaaaaaaaaaa'
export const secondModel = 'mdl_01bbbbbbbbbbbbbbbbbbbbbbbb'
export function modelRow(id = firstModel, name = 'recorded-model'): MemberModelRow {
  return {
    id,
    name,
    status: 'active',
    type: null,
    providers: null,
    protocols: ['openai_chat'],
    availability: 'ready',
    input_price: { state: 'priced', rate: { amount: '0', unit: '1M_TOKEN', currency: 'USD' } },
    output_price: {
      state: 'disabled',
      rate: { amount: '9007199254740993.000000000000000001', unit: '1M_TOKEN', currency: 'EUR' },
    },
    created_at: '2026-10-04T00:00:00Z',
    updated_at: null,
    selectable: false,
  }
}
export function modelsFixture(): MemberModelsWorkspace {
  return {
    user_id: modelsTarget,
    observed_at: '2026-10-05T00:00:00Z',
    etag: 'a'.repeat(64),
    can_edit: true,
    personal_models: [modelRow()],
    available_models: [{ ...modelRow(secondModel, 'available-model'), selectable: true }],
    runtime_applied: true,
    application_status: 'applied',
  }
}
