package vault

import (
	"testing"
)

func TestVerifyHashesEmpty(t *testing.T) {
	handler := NewHTTPSourceHandler("")

	tests := []struct {
		name    string
		hashes  map[string]string
		wantErr bool
	}{
		{
			name:    "nil hashes skips verification",
			hashes:  nil,
			wantErr: false,
		},
		{
			name:    "empty hashes skips verification",
			hashes:  map[string]string{},
			wantErr: false,
		},
		{
			name:    "invalid hash fails verification",
			hashes:  map[string]string{"sha256": "badhash"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.verifyHashes([]byte("test data"), tt.hashes)
			if (err != nil) != tt.wantErr {
				t.Errorf("verifyHashes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
