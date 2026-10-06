export interface ProviderModelBindingModel {
  id: string
  name: string | null
}

export interface ProviderModelBindingItem {
  provider_model_id: string
  connection_id: string
  binding_count: number
  models: ProviderModelBindingModel[]
}

export interface ProviderModelBindings {
  provider_id: string
  items: ProviderModelBindingItem[]
}
