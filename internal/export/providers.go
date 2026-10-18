package export

// StatusProviders is the provider summary block of the status file. It reuses
// the widget's provider snapshots (quota rows, plan, balance, daily tokens,
// reset credits, availability) and the reset-today summary; none of it holds
// credentials. A nil provider is omitted from the JSON.
type StatusProviders struct {
	Codex      *ProviderSnapshot   `json:"codex,omitempty"`
	Grok       *ProviderSnapshot   `json:"grok,omitempty"`
	Claude     *ProviderSnapshot   `json:"claude,omitempty"`
	Cursor     *ProviderSnapshot   `json:"cursor,omitempty"`
	Kimi       *ProviderSnapshot   `json:"kimi,omitempty"`
	Zai        *ProviderSnapshot   `json:"zai,omitempty"`
	Agy        *ProviderSnapshot   `json:"agy,omitempty"`
	ResetToday *ResetTodaySnapshot `json:"resetToday,omitempty"`
}
