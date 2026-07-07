package vault

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const metadataTOML = `[asset]
name = "docs-tone"
version = "1"
type = "skill"
description = "Docs tone guide"

[skill]
prompt-file = "SKILL.md"
`

func assetArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"metadata.toml": metadataTOML,
		"SKILL.md":      "# docs-tone\n",
	} {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// app.skills.new answers the metadata.toml path with the whole asset
// archive; GetMetadata must unwrap it instead of feeding zip bytes to the
// TOML parser (which errored, and callers that tolerate metadata failures
// then produced a typeless asset — "unknown asset type" on publish).
func TestSleuthVault_GetMetadata_UnwrapsArchiveResponse(t *testing.T) {
	for name, body := range map[string][]byte{
		"bare toml": []byte(metadataTOML),
		"archive":   assetArchive(t),
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/skills/assets/docs-tone/1/metadata.toml" {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write(body)
			}))
			defer srv.Close()

			v := NewSleuthVault(srv.URL, "token")
			meta, err := v.GetMetadata(context.Background(), "docs-tone", "1")
			if err != nil {
				t.Fatalf("GetMetadata: %v", err)
			}
			if meta.Asset.Type.Key != "skill" || meta.Asset.Name != "docs-tone" {
				t.Fatalf("metadata = %+v, want skill/docs-tone", meta.Asset)
			}
		})
	}
}
