package database

import "fmt"

// catalogMigration is immutable schema version 3. Versions 1 and 2 are unchanged.
func catalogMigration(dialect string) []string {
	timestamp, modelName, upstreamName := "TIMESTAMPTZ", "VARCHAR(128)", "VARCHAR(255)"
	if dialect == "mysql" {
		timestamp = "DATETIME(6)"
		modelName += " CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"
		upstreamName += " CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"
	}
	return []string{
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS providers (id VARCHAR(30) PRIMARY KEY, name VARCHAR(100) NOT NULL, created_at %s NOT NULL)", timestamp),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS provider_connections (id VARCHAR(30) PRIMARY KEY, provider_id VARCHAR(30) NOT NULL, name VARCHAR(100) NOT NULL, base_url VARCHAR(2048) NOT NULL, protocol VARCHAR(30) NOT NULL, created_at %s NOT NULL, CONSTRAINT fk_connections_provider FOREIGN KEY (provider_id) REFERENCES providers(id))", timestamp),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS provider_credentials (id VARCHAR(30) PRIMARY KEY, connection_id VARCHAR(30) NOT NULL, name VARCHAR(100) NOT NULL, ciphertext TEXT NOT NULL, priority INTEGER NOT NULL DEFAULT 0, enabled BOOLEAN NOT NULL DEFAULT FALSE, verification_status VARCHAR(20) NOT NULL, verified_at %s, created_at %s NOT NULL, CONSTRAINT fk_credentials_connection FOREIGN KEY (connection_id) REFERENCES provider_connections(id))", timestamp, timestamp),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS provider_models (id VARCHAR(30) PRIMARY KEY, connection_id VARCHAR(30) NOT NULL, upstream_name %s NOT NULL, created_at %s NOT NULL, CONSTRAINT uq_provider_model_name UNIQUE(connection_id, upstream_name), CONSTRAINT fk_provider_models_connection FOREIGN KEY (connection_id) REFERENCES provider_connections(id))", upstreamName, timestamp),
		"CREATE TABLE IF NOT EXISTS credential_model_accesses (credential_id VARCHAR(30) NOT NULL, provider_model_id VARCHAR(30) NOT NULL, PRIMARY KEY (credential_id, provider_model_id), CONSTRAINT fk_access_credential FOREIGN KEY (credential_id) REFERENCES provider_credentials(id), CONSTRAINT fk_access_provider_model FOREIGN KEY (provider_model_id) REFERENCES provider_models(id))",
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS models (id VARCHAR(30) PRIMARY KEY, status VARCHAR(20) NOT NULL, created_at %s NOT NULL)", timestamp),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS model_names (name %s PRIMARY KEY, model_id VARCHAR(30) NOT NULL, current_model_id VARCHAR(30) UNIQUE, expires_at %s, created_at %s NOT NULL, CONSTRAINT fk_names_model FOREIGN KEY (model_id) REFERENCES models(id), CONSTRAINT fk_names_current_model FOREIGN KEY (current_model_id) REFERENCES models(id), CONSTRAINT ck_names_current CHECK (current_model_id IS NULL OR current_model_id = model_id))", modelName, timestamp, timestamp),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS model_provider_bindings (id VARCHAR(30) PRIMARY KEY, model_id VARCHAR(30) NOT NULL, provider_model_id VARCHAR(30) NOT NULL, weight INTEGER NOT NULL DEFAULT 0, created_at %s NOT NULL, CONSTRAINT uq_model_binding UNIQUE(model_id, provider_model_id), CONSTRAINT ck_binding_weight CHECK (weight >= 0 AND weight <= 100), CONSTRAINT fk_bindings_model FOREIGN KEY (model_id) REFERENCES models(id), CONSTRAINT fk_bindings_provider_model FOREIGN KEY (provider_model_id) REFERENCES provider_models(id))", timestamp),
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS user_model_grants (user_id VARCHAR(30) NOT NULL, model_id VARCHAR(30) NOT NULL, created_at %s NOT NULL, PRIMARY KEY (user_id, model_id), CONSTRAINT fk_grants_user FOREIGN KEY (user_id) REFERENCES users(id), CONSTRAINT fk_grants_model FOREIGN KEY (model_id) REFERENCES models(id))", timestamp),
	}
}
