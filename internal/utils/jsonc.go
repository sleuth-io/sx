package utils

import (
	"encoding/json"

	"github.com/tailscale/hujson"
)

// UnmarshalJSONC parses JSONC (JSON with Comments and trailing commas) into v.
// VS Code-based editors (Cursor, Kiro, Cline, etc.) treat JSON config files as
// JSONC, so user-edited config files may contain comments and trailing commas
// that Go's encoding/json rejects. This function standardizes the input first.
// For valid JSON input the standardize step is a no-op.
func UnmarshalJSONC(data []byte, v any) error {
	standardized, err := hujson.Standardize(data)
	if err != nil {
		// If standardization fails, fall back to raw unmarshal so the caller
		// gets the more specific json.Unmarshal error (e.g., syntax position).
		return json.Unmarshal(data, v)
	}
	return json.Unmarshal(standardized, v)
}
