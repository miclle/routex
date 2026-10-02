import {
  modelCatalogProtocols,
  type ModelAccessSource,
  type ModelCatalogRecord,
} from '@/types/model-catalog'

export function modelSourceKey(source: ModelAccessSource) {
  return source.type === 'personal' ? 'personal' : `team:${source.team_id}`
}

export function orderedModelSources(sources: ModelAccessSource[], selectedSource = 'all') {
  return [...sources].sort((left, right) => {
    const selected =
      Number(modelSourceKey(right) === selectedSource) -
      Number(modelSourceKey(left) === selectedSource)
    if (selected) return selected
    if (left.type !== right.type) return left.type === 'personal' ? -1 : 1
    return (
      (left.team_name ?? '').localeCompare(right.team_name ?? '', 'en') ||
      modelSourceKey(left).localeCompare(modelSourceKey(right), 'en')
    )
  })
}

export function knownModelProtocols(model: ModelCatalogRecord) {
  return model.status === 'active'
    ? [...new Set(model.protocols)].filter((protocol) =>
        modelCatalogProtocols.some((known) => known === protocol),
      )
    : []
}

export function personallyAvailable(model: ModelCatalogRecord) {
  return (
    model.personal_available &&
    model.sources.some((source) => source.type === 'personal' && source.invocation_supported) &&
    knownModelProtocols(model).length > 0
  )
}

export function declaredCapabilities(model: ModelCatalogRecord, protocol = 'all') {
  const protocols = knownModelProtocols(model).filter(
    (candidate) => protocol === 'all' || candidate === protocol,
  )
  return (['image', 'pdf'] as const).filter((capability) =>
    protocols.some((candidate) => model.input_capabilities[candidate]?.includes(capability)),
  )
}
