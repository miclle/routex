package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/upstream"
)

func egressRevision(connection entity.ProviderConnection, setting entity.EgressSetting, row *entity.Egress) string {
	value := struct {
		ConnectionID, BaseURL, Mode                       string
		EgressID                                          *string
		DefaultETag, EgressETag                           string
		DefaultID                                         *string
		ProxyID, Kind, Host, SecretGeneration, Ciphertext string
		Port                                              int
		Enabled                                           bool
	}{ConnectionID: connection.ID, BaseURL: connection.BaseURL, Mode: connection.EgressMode, EgressID: connection.EgressID}
	if value.Mode == "" {
		value.Mode = "default"
	}
	if value.Mode == "default" {
		value.DefaultETag = setting.ETag
		value.DefaultID = setting.DefaultEgressID
	}
	if row != nil {
		value.EgressETag = row.ETag
		value.ProxyID, value.Kind, value.Host = row.ID, row.Kind, row.Host
		value.Port, value.Enabled = row.Port, row.Enabled
		value.SecretGeneration, value.Ciphertext = row.SecretGeneration, row.AuthCiphertext
	}
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
func (s *Service) resolveConnectionEgress(db *gorm.DB, connection entity.ProviderConnection) (string, *upstream.EgressConfig, string, error) {
	var setting entity.EgressSetting
	egressID := connection.EgressID
	switch connection.EgressMode {
	case "", "default":
		if err := db.First(&setting, 1).Error; err != nil {
			return "", nil, "", catalogError(err)
		}
		egressID = setting.DefaultEgressID
	case "direct":
		if egressID != nil {
			return "", nil, "", runtimeUnavailable
		}
	case "proxy":
		if egressID == nil {
			return "", nil, "", runtimeUnavailable
		}
	default:
		return "", nil, "", runtimeUnavailable
	}
	if egressID == nil {
		return "", nil, egressRevision(connection, setting, nil), nil
	}
	var row entity.Egress
	if err := db.First(&row, "id = ?", *egressID).Error; err != nil {
		return "", nil, "", catalogError(err)
	}
	if !row.Enabled {
		return "", nil, "", runtimeUnavailable
	}
	config, err := s.egressConfig(row)
	if err != nil {
		return "", nil, "", err
	}
	return row.ID, config, egressRevision(connection, setting, &row), nil
}
func (s *Service) clientForConnection(ctx context.Context, connection entity.ProviderConnection) (*http.Client, string, error) {
	if connection.EgressMode == "direct" && connection.EgressID == nil {
		return s.upstream, egressRevision(connection, entity.EgressSetting{}, nil), nil
	}
	_, config, revision, err := s.resolveConnectionEgress(s.authDB(ctx), connection)
	if err != nil {
		return nil, "", err
	}
	if config == nil {
		return s.upstream, revision, nil
	}
	client, err := upstream.NewEgressClient(s.allowPrivateUpstream, s.allowPrivateEgress, *config)
	if err != nil {
		return nil, "", runtimeUnavailable
	}
	return client, revision, nil
}
