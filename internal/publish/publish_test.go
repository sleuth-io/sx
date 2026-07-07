package publish

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
)

func zipWith(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type fakeVersionReader struct {
	versions []string
	byVer    map[string][]byte
	fetchErr error
}

func (f *fakeVersionReader) GetVersionList(context.Context, string) ([]string, error) {
	return f.versions, nil
}

func (f *fakeVersionReader) GetAssetByVersion(_ context.Context, _, ver string) ([]byte, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return f.byVer[ver], nil
}

func TestSuggestVersionNewAsset(t *testing.T) {
	v, identical, err := SuggestVersion(context.Background(), &fakeVersionReader{}, "a", zipWith(t, map[string]string{"SKILL.md": "x"}))
	if err != nil || v != "1" || identical {
		t.Fatalf("got (%q, %v, %v), want (\"1\", false, nil)", v, identical, err)
	}
}

func TestSuggestVersionIdenticalContent(t *testing.T) {
	data := zipWith(t, map[string]string{"SKILL.md": "same"})
	reader := &fakeVersionReader{
		versions: []string{"1", "2"},
		byVer:    map[string][]byte{"2": zipWith(t, map[string]string{"SKILL.md": "same"})},
	}
	v, identical, err := SuggestVersion(context.Background(), reader, "a", data)
	if err != nil || v != "2" || !identical {
		t.Fatalf("got (%q, %v, %v), want (\"2\", true, nil)", v, identical, err)
	}
}

func TestSuggestVersionChangedContentIncrements(t *testing.T) {
	reader := &fakeVersionReader{
		versions: []string{"1", "2"},
		byVer:    map[string][]byte{"2": zipWith(t, map[string]string{"SKILL.md": "old"})},
	}
	v, identical, err := SuggestVersion(context.Background(), reader, "a", zipWith(t, map[string]string{"SKILL.md": "new"}))
	if err != nil || v != "3" || identical {
		t.Fatalf("got (%q, %v, %v), want (\"3\", false, nil)", v, identical, err)
	}
}

func TestSuggestVersionFetchErrorStillIncrements(t *testing.T) {
	reader := &fakeVersionReader{
		versions: []string{"4"},
		fetchErr: errors.New("offline"),
	}
	v, identical, err := SuggestVersion(context.Background(), reader, "a", zipWith(t, map[string]string{"SKILL.md": "x"}))
	if err != nil || v != "5" || identical {
		t.Fatalf("got (%q, %v, %v), want (\"5\", false, nil)", v, identical, err)
	}
}

func TestDetectNameAndTypeMetadataWins(t *testing.T) {
	data := zipWith(t, map[string]string{
		"metadata.toml": "metadata_version = \"1\"\n[asset]\nname = \"declared\"\nversion = \"1\"\ntype = \"rule\"\n",
		"RULE.md":       "# rule",
	})
	name, typ, hasMeta, err := DetectNameAndType(data, "fallback")
	if err != nil {
		t.Fatalf("DetectNameAndType: %v", err)
	}
	if name != "declared" || typ.Key != asset.TypeRule.Key || !hasMeta {
		t.Fatalf("got (%q, %q, %v), want declared metadata", name, typ.Key, hasMeta)
	}
}

func TestDetectNameAndTypeFallsBackToDetection(t *testing.T) {
	data := zipWith(t, map[string]string{"SKILL.md": "# skill"})
	name, typ, hasMeta, err := DetectNameAndType(data, "fallback")
	if err != nil {
		t.Fatalf("DetectNameAndType: %v", err)
	}
	if name != "fallback" || typ.Key != asset.TypeSkill.Key || hasMeta {
		t.Fatalf("got (%q, %q, %v), want detected skill", name, typ.Key, hasMeta)
	}
}

func TestDetectNameAndTypeCorruptMetadataErrors(t *testing.T) {
	data := zipWith(t, map[string]string{"metadata.toml": "not [valid toml"})
	if _, _, _, err := DetectNameAndType(data, "fallback"); err == nil {
		t.Fatal("want parse error for corrupt metadata.toml")
	}
}
