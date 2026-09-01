package helps

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
