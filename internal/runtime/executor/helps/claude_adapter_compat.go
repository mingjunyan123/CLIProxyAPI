package helps

import (
	"strings"

	"github.com/tidwall/gjson"
)

// claudeAdapterNativeEntrypoint extends native pass-through without adding
// adapter-specific entrypoints to the upstream Claude client table.
func claudeAdapterNativeEntrypoint(entrypoint string) bool {
	switch entrypoint {
	case "local-agent", "claude-desktop-3p":
		return true
	default:
		return false
	}
}

// claudeAdapterDeviceID keeps the adapter-selected device identity when it is
// valid. OAuth credential metadata remains authoritative for account_uuid.
func claudeAdapterDeviceID(userID string) string {
	deviceID := strings.TrimSpace(gjson.Get(userID, "device_id").String())
	if !claudeMetadataDeviceIDPattern.MatchString(deviceID) {
		return ""
	}
	return deviceID
}
