package database

import "fmt"

func apiKeyMigration(dialect string) []string {
	timestamp := "TIMESTAMPTZ"
	if dialect == "mysql" {
		timestamp = "DATETIME(6)"
	}
	return []string{
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS api_keys (id VARCHAR(30) PRIMARY KEY, user_id VARCHAR(30) NOT NULL, name VARCHAR(100) NOT NULL, prefix VARCHAR(16) NOT NULL, token_hash VARCHAR(64) NOT NULL UNIQUE, status VARCHAR(20) NOT NULL, expires_at %s NULL, delivery_expires_at %s NULL, replaces_key_id VARCHAR(30) NULL, activate_on_confirm BOOLEAN NOT NULL, created_at %s NOT NULL, updated_at %s NOT NULL, CONSTRAINT fk_api_keys_user FOREIGN KEY(user_id) REFERENCES users(id), CONSTRAINT fk_api_keys_replaces FOREIGN KEY(replaces_key_id) REFERENCES api_keys(id))", timestamp, timestamp, timestamp, timestamp),
		"CREATE TABLE IF NOT EXISTS api_key_models (key_id VARCHAR(30) NOT NULL, model_id VARCHAR(30) NOT NULL, PRIMARY KEY(key_id,model_id), CONSTRAINT fk_key_models_key FOREIGN KEY(key_id) REFERENCES api_keys(id), CONSTRAINT fk_key_models_model FOREIGN KEY(model_id) REFERENCES models(id))",
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS audit_events (id VARCHAR(30) PRIMARY KEY, actor_id VARCHAR(30) NOT NULL, action VARCHAR(80) NOT NULL, resource_type VARCHAR(40) NOT NULL, resource_id VARCHAR(30) NOT NULL, created_at %s NOT NULL)", timestamp),
	}
}
