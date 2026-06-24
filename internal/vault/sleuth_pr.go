package vault

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Khan/genqlient/graphql"

	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// metadataFileName is the asset descriptor that lives at the zip root. The
// server stores it out of band (it isn't an AssetFile), so we exclude it from
// the pull request's file changes to mirror how a direct upload is unpacked.
const metadataFileName = "metadata.toml"

// OpenAssetPullRequest proposes the uploaded asset as a pull request against an
// existing skill, used when a direct publish was blocked by the server's edit
// gate (see docs/rbac.md). It creates the PR for assetGID, then adds one file
// change per file in the zip. The asset isn't published until a maintainer
// merges the PR. Returns the PR's source URL.
//
// assetGID comes straight from the upload's 403 response (AssetEditPermissionError.
// AssetGID), so no asset lookup is needed here.
func (s *SleuthVault) OpenAssetPullRequest(ctx context.Context, assetGID, title, body string, zipData []byte) (PRResult, error) {
	if assetGID == "" {
		return PRResult{}, errors.New("cannot open pull request: missing asset id")
	}

	client := s.gqlClient()

	description := body
	created, err := vaultgql.CreateAssetPullRequest(ctx, client, vaultgql.CreateAssetPullRequestInput{
		AssetId:     assetGID,
		Title:       title,
		Description: &description,
	})
	if err != nil {
		return PRResult{}, fmt.Errorf("failed to create pull request: %w", err)
	}
	if created.CreateAssetPullRequest == nil {
		return PRResult{}, errors.New("failed to create pull request: empty response")
	}
	if err := gqlMutationErrors(created.CreateAssetPullRequest.Errors); err != nil {
		return PRResult{}, fmt.Errorf("failed to create pull request: %w", err)
	}
	pr := created.CreateAssetPullRequest.PullRequest
	if pr == nil {
		return PRResult{}, errors.New("failed to create pull request: server returned no pull request")
	}

	if err := s.addPullRequestFiles(ctx, client, pr.Id, zipData); err != nil {
		return PRResult{}, err
	}

	return PRResult{URL: pr.SourceUrl, Created: true}, nil
}

// addPullRequestFiles unpacks zipData and records each file as a change on the
// pull request. Files are added with action ADD: the server accepts ADD for both
// new and existing files (and still computes a diff against the current version),
// whereas MODIFY/DELETE require the file to already exist — so ADD is the safe,
// uniform choice for "here is my proposed content". metadata.toml is skipped to
// match how a direct upload is unpacked.
func (s *SleuthVault) addPullRequestFiles(ctx context.Context, client graphql.Client, prID string, zipData []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("failed to read asset archive: %w", err)
	}

	for _, f := range reader.File {
		if f.FileInfo().IsDir() || f.Name == metadataFileName {
			continue
		}
		content, err := readZipEntry(f)
		if err != nil {
			return fmt.Errorf("failed to read %q from asset archive: %w", f.Name, err)
		}
		path, name := splitFilePath(f.Name)

		resp, err := vaultgql.AddAssetPullRequestFileChange(ctx, client, vaultgql.AddAssetPullRequestFileChangeInput{
			PullRequestId: prID,
			Name:          name,
			Path:          path,
			Action:        vaultgql.SkillPullRequestFileActionEnumAdd,
			Content:       &content,
		})
		if err != nil {
			return fmt.Errorf("failed to add %q to pull request: %w", f.Name, err)
		}
		if resp.AddAssetPullRequestFileChange == nil {
			return fmt.Errorf("failed to add %q to pull request: empty response", f.Name)
		}
		if err := gqlMutationErrors(resp.AddAssetPullRequestFileChange.Errors); err != nil {
			return fmt.Errorf("failed to add %q to pull request: %w", f.Name, err)
		}
	}
	return nil
}

// readZipEntry returns the full uncompressed contents of a zip entry as a string.
func readZipEntry(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// splitFilePath splits a zip entry path into (directory, filename), mirroring the
// server's split_file_path so the change targets the same (name, path) the asset
// already stores. A file at the root has a nil directory (sent as null), not "".
func splitFilePath(filePath string) (*string, string) {
	if i := strings.LastIndex(filePath, "/"); i >= 0 {
		dir := filePath[:i]
		return &dir, filePath[i+1:]
	}
	return nil, filePath
}
