package entity

const (
	AdapterNative             = "native"
	AdapterAzureOpenAIClassic = "azure_openai_classic"
)

// ConnectionAdapter retains the pre-V79 native meaning of historical blank rows.
func ConnectionAdapter(row ProviderConnection) string {
	if row.Adapter == "" {
		return AdapterNative
	}
	return row.Adapter
}

// CredentialDeploymentAttestation is reviewed operator evidence, never discovery.
// Historical rows retain their exact identities after target deletion; current
// eligibility additionally requires an exact live target and source identity.
type CredentialDeploymentAttestation struct {
	CredentialID    string `gorm:"primaryKey;size:30"`
	ProviderModelID string `gorm:"primaryKey;size:30"`
	Identity        string `gorm:"size:64;not null"`
}
