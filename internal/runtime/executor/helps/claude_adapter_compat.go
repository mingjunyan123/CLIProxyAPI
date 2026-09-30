package helps

import (
	"encoding/json"
	"strings"
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

// claudeAdapterInboundDeviceID returns the caller's device_id when that field
// is a non-empty string. Missing, blank, and non-string values return "" so
// the credential pool still supplies the id.
func claudeAdapterInboundDeviceID(existingUserID string) string {
	raw := strings.TrimSpace(existingUserID)
	if raw == "" {
		return ""
	}
	var fields struct {
		DeviceID json.RawMessage `json:"device_id"`
	}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || len(fields.DeviceID) == 0 {
		return ""
	}
	var deviceID string
	if err := json.Unmarshal(fields.DeviceID, &deviceID); err != nil {
		return ""
	}
	if strings.TrimSpace(deviceID) == "" {
		return ""
	}
	return deviceID
}
