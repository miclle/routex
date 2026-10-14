package deliverycontroller

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
)

type operation struct {
	SchemaVersion int                   `json:"schema_version"`
	RouteXURL     string                `json:"routex_url"`
	SlotID        string                `json:"slot_id"`
	Phase         string                `json:"phase"`
	Intent        acknowledgement       `json:"intent"`
	Replacement   *replacementOperation `json:"replacement,omitempty"`
}

func newOperation(cfg Config) operation {
	uuid := make([]byte, 16)
	if _, err := rand.Read(uuid); err != nil {
		// Go 1.27 crypto/rand.Read is documented to terminate the process if
		// secure randomness fails, but keep no insecure fallback in this path.
		return operation{}
	}
	uuid[6], uuid[8] = uuid[6]&0x0f|0x40, uuid[8]&0x3f|0x80
	text := hex.EncodeToString(uuid)
	return operation{
		SchemaVersion: 1, RouteXURL: cfg.RouteXURL, SlotID: cfg.SlotID, Phase: "prepared",
		Intent: acknowledgement{
			RequestID:          text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:],
			DescriptorRevision: cfg.DescriptorRevision, ConfigurationDigest: cfg.ConfigurationDigest,
			AgentState: "unknown", OutputState: "unknown",
		},
	}
}

func (op operation) validate(cfg Config) error {
	if (op.SchemaVersion != 1 && op.SchemaVersion != 2) || op.RouteXURL != cfg.RouteXURL || op.SlotID != cfg.SlotID || op.Intent.DescriptorRevision != cfg.DescriptorRevision || op.Intent.ConfigurationDigest != cfg.ConfigurationDigest || !uuidPattern.MatchString(op.Intent.RequestID) {
		return errors.New("saved operation does not match pinned target")
	}
	if op.Phase != "prepared" && op.Phase != "promote_pending" && op.Phase != "report_pending" && op.Phase != "reported" {
		return errors.New("invalid saved operation phase")
	}
	if op.Intent.AgentState != "unknown" && op.Intent.AgentState != "failed" || op.Intent.OutputState != "unknown" && op.Intent.OutputState != "written" && op.Intent.OutputState != "failed" {
		return errors.New("invalid saved controller observation")
	}
	if op.SchemaVersion == 1 && (op.Replacement != nil || op.Phase == "promote_pending") || op.SchemaVersion == 2 && (op.Replacement == nil || op.Replacement.validate(op) != nil) {
		return errors.New("invalid replacement operation")
	}
	if (op.Phase == "prepared" || op.Phase == "promote_pending") && (op.Intent.AgentState != "unknown" || op.Intent.OutputState != "unknown") {
		return errors.New("unconfirmed operation claims an observation")
	}
	return nil
}

func saveOperation(root *os.Root, op operation) error {
	raw, err := json.Marshal(op)
	if err != nil {
		return errors.New("encode controller operation failed")
	}
	return atomicWrite(root, "operation.json", raw)
}

func readOperation(root *os.Root, cfg Config) (op operation, runErr error) {
	file, err := openRegular(root, "operation.json", 0600, maxJSONBytes, false)
	if err != nil {
		return op, errors.New("read saved controller operation failed")
	}
	defer func() {
		if file.Close() != nil && runErr == nil {
			op = operation{}
			runErr = errors.New("close saved controller operation failed")
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(file, maxJSONBytes+1))
	if err != nil || len(raw) > maxJSONBytes || decodeJSON(raw, &op) != nil || op.validate(cfg) != nil {
		return operation{}, errors.New("saved controller operation rejected")
	}
	return op, nil
}
