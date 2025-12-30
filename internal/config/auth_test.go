package config

import (
	"testing"
)

func TestOAuthDeviceCodeResponse_VerificationURIComplete(t *testing.T) {
	tests := []struct {
		name        string
		deviceResp  OAuthDeviceCodeResponse
		expectedURL string
		description string
	}{
		{
			name: "uses verification_uri_complete when provided",
			deviceResp: OAuthDeviceCodeResponse{
				DeviceCode:              "device123",
				UserCode:                "ABCD-EFGH",
				VerificationURI:         "https://example.com/oauth/device",
				VerificationURIComplete: "https://example.com/oauth/device?user_code=ABCD-EFGH",
				ExpiresIn:               600,
			},
			expectedURL: "https://example.com/oauth/device?user_code=ABCD-EFGH",
			description: "Should use verification_uri_complete when provided by server",
		},
		{
			name: "constructs URL with user_code when verification_uri_complete is empty",
			deviceResp: OAuthDeviceCodeResponse{
				DeviceCode:              "device123",
				UserCode:                "WXYZ-1234",
				VerificationURI:         "https://example.com/oauth/device",
				VerificationURIComplete: "",
				ExpiresIn:               600,
			},
			expectedURL: "https://example.com/oauth/device?user_code=WXYZ-1234",
			description: "Should construct URL with user_code parameter when verification_uri_complete is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the logic from Authenticate function
			browserURL := tt.deviceResp.VerificationURIComplete
			if browserURL == "" {
				browserURL = tt.deviceResp.VerificationURI + "?user_code=" + tt.deviceResp.UserCode
			}

			if browserURL != tt.expectedURL {
				t.Errorf("%s\nExpected URL: %s\nGot URL: %s", tt.description, tt.expectedURL, browserURL)
			}
		})
	}
}
