package config

// forkV8Paths holds fork-only legacy-to-v8 path aliases.
// buildV8Paths appends these after the upstream prefix list.
func forkV8Paths() []configPath {
	return []configPath{
		{"claude-oauth-outbound-log", "observability.logs.claude-oauth-outbound-log"},
	}
}
