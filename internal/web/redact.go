package web

import (
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// SanitizeSnapshot makes a response-only copy. Raw provider metadata is never
// part of the web DTO, while hide-costs is resolved against the original
// snapshot so plan-aware policy still sees its private signals.
func SanitizeSnapshot(snap core.UsageSnapshot, perAccount, global *bool) SnapshotDTO {
	hideCosts := core.ResolveHideCosts(snap, perAccount, global)
	clone := snap.DeepClone()

	return SnapshotDTO{
		ProviderID:  clone.ProviderID,
		AccountID:   clone.AccountID,
		Timestamp:   clone.Timestamp,
		Status:      clone.Status,
		Metrics:     clone.Metrics,
		Resets:      clone.Resets,
		Attributes:  sanitizeStringMap(clone.Attributes),
		Diagnostics: sanitizeStringMap(clone.Diagnostics),
		ModelUsage:  clone.ModelUsage,
		DailySeries: clone.DailySeries,
		Message:     clone.Message,
		HideCosts:   hideCosts,
	}
}

func sanitizeStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		if sensitiveMetadataKey(key) {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sensitiveMetadataKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return true
	}
	for _, marker := range []string{
		"api_key", "apikey", "authorization", "cookie", "credential", "password",
		"secret", "session_token", "access_token", "refresh_token", "bearer",
	} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}
