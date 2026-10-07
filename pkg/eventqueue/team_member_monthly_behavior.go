package eventqueue

import (
	"encoding/base32"
	"strings"
)

func teamMemberBehaviorAccount(account string) bool {
	value, ok := strings.CutPrefix(account, "team_member_")
	if !ok {
		return false
	}
	codec := base32.StdEncoding.WithPadding(base32.NoPadding)
	raw, err := codec.DecodeString(value)
	return err == nil && len(raw) == 32 && codec.EncodeToString(raw) == value
}
