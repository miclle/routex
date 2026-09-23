package entity

const ProtocolOpenAIResponses = "openai_responses"

// SupportedNativeProtocol is the implemented gateway protocol allowlist.
func SupportedNativeProtocol(protocol string) bool {
	return protocol == ProtocolOpenAIChat || protocol == ProtocolOpenAIResponses
}
