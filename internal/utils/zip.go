package utils

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// ZipMagicBytes are the first 4 bytes of a ZIP file
var ZipMagicBytes = []byte{0x50, 0x4B, 0x03, 0x04}

// IsZipFile checks if data starts with ZIP magic bytes
func IsZipFile(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return bytes.Equal(data[:4], ZipMagicBytes)
}

// HasContentFiles returns true if the zip contains files beyond metadata.toml.
// This is used to distinguish packaged assets (with server code) from config-only assets.
func HasContentFiles(zipData []byte) (bool, error) {
	files, err := ListZipFiles(zipData)
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if f != "metadata.toml" && !strings.HasSuffix(f, "/") {
			return true, nil
		}
	}
	return false, nil
}

// ExtractZip extracts a zip file to a target directory
func ExtractZip(zipData []byte, targetDir string) error {
	if !IsZipFile(zipData) {
		return errors.New("invalid zip file: missing magic bytes")
	}

	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("failed to read zip: %w", err)
	}

	for _, file := range reader.File {
		if err := extractZipFile(file, targetDir); err != nil {
			return fmt.Errorf("failed to extract %s: %w", file.Name, err)
		}
	}

	return nil
}

// extractZipFile extracts a single file from a zip archive
func extractZipFile(file *zip.File, targetDir string) error {
	// Prevent zip slip vulnerability
	targetPath := filepath.Join(targetDir, file.Name)
	if !strings.HasPrefix(targetPath, filepath.Clean(targetDir)+string(os.PathSeparator)) {
		return fmt.Errorf("illegal file path: %s", file.Name)
	}

	if file.FileInfo().IsDir() {
		return os.MkdirAll(targetPath, file.Mode())
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	// Extract file
	outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
	if err != nil {
		return err
	}
	defer outFile.Close()

	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	_, err = io.Copy(outFile, rc)
	return err
}

// ReadZipFile reads a specific file from a zip archive without extracting
func ReadZipFile(zipData []byte, filename string) ([]byte, error) {
	if !IsZipFile(zipData) {
		return nil, errors.New("invalid zip file: missing magic bytes")
	}

	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to read zip: %w", err)
	}

	for _, file := range reader.File {
		if file.Name == filename {
			rc, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open file in zip: %w", err)
			}
			defer rc.Close()

			data, err := io.ReadAll(rc)
			if err != nil {
				return nil, fmt.Errorf("failed to read file in zip: %w", err)
			}
			return data, nil
		}
	}

	return nil, fmt.Errorf("file not found in zip: %s", filename)
}

// ListZipFiles returns a list of all files in a zip archive
func ListZipFiles(zipData []byte) ([]string, error) {
	if !IsZipFile(zipData) {
		return nil, errors.New("invalid zip file: missing magic bytes")
	}

	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to read zip: %w", err)
	}

	var files []string
	for _, file := range reader.File {
		files = append(files, file.Name)
	}

	return files, nil
}

// CreateZip creates a zip archive from a directory
func CreateZip(sourceDir string) ([]byte, error) {
	buf := new(bytes.Buffer)
	writer := zip.NewWriter(buf)

	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if path == sourceDir {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// Create zip header
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)

		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}

		// Write header
		w, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}

		// Write file content if not a directory
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(w, file)
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create zip: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zip writer: %w", err)
	}

	return buf.Bytes(), nil
}

// CreateZipFromContent creates a zip archive containing a single file with the given content
func CreateZipFromContent(filename string, content []byte) ([]byte, error) {
	buf := new(bytes.Buffer)
	writer := zip.NewWriter(buf)

	w, err := writer.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create file in zip: %w", err)
	}

	if _, err := w.Write(content); err != nil {
		return nil, fmt.Errorf("failed to write content to zip: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zip writer: %w", err)
	}

	return buf.Bytes(), nil
}

// AddFileToZip adds or updates a file in a zip archive
func AddFileToZip(zipData []byte, filename string, content []byte) ([]byte, error) {
	if !IsZipFile(zipData) {
		return nil, errors.New("invalid zip file: missing magic bytes")
	}

	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to read zip: %w", err)
	}

	buf := new(bytes.Buffer)
	writer := zip.NewWriter(buf)

	// Copy existing files except the one we're replacing
	for _, file := range reader.File {
		if file.Name == filename {
			continue // Skip, we'll add the new version
		}

		if err := copyZipFile(writer, file); err != nil {
			return nil, fmt.Errorf("failed to copy file %s: %w", file.Name, err)
		}
	}

	// Add the new file
	w, err := writer.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create file in zip: %w", err)
	}

	if _, err := w.Write(content); err != nil {
		return nil, fmt.Errorf("failed to write file to zip: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zip writer: %w", err)
	}

	return buf.Bytes(), nil
}

// copyZipFile copies a file from one zip archive to another
func copyZipFile(writer *zip.Writer, file *zip.File) error {
	// Read the file data first
	r, err := file.Open()
	if err != nil {
		return err
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	// Create a new header based on the original, but let the writer compute checksums
	header := &zip.FileHeader{
		Name:     file.Name,
		Method:   file.Method,
		Modified: file.Modified,
	}

	// If it's a directory, ensure trailing slash
	if file.FileInfo().IsDir() {
		header.Name = strings.TrimSuffix(header.Name, "/") + "/"
	}

	w, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = w.Write(data)
	return err
}

// ComputeZipHash computes an MD5 hash of all files in a zip archive
// Files are hashed individually, then combined in alphabetical order by filename
func ComputeZipHash(zipData []byte) ([]byte, error) {
	if !IsZipFile(zipData) {
		return nil, errors.New("invalid zip file: missing magic bytes")
	}

	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to read zip: %w", err)
	}

	// Create a map of filename -> hash for sorting
	fileHashes := make(map[string][]byte)

	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		// Open and hash the file
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s in zip: %w", file.Name, err)
		}

		h := md5.New()
		if _, err := io.Copy(h, rc); err != nil {
			rc.Close()
			return nil, fmt.Errorf("failed to hash file %s: %w", file.Name, err)
		}
		rc.Close()

		fileHashes[file.Name] = h.Sum(nil)
	}

	// Sort filenames for consistent ordering
	filenames := make([]string, 0, len(fileHashes))
	for name := range fileHashes {
		filenames = append(filenames, name)
	}
	sort.Strings(filenames)

	// Combine all hashes in sorted order
	combined := md5.New()
	for _, name := range filenames {
		// Include filename in hash to detect renames
		combined.Write([]byte(name))
		combined.Write(fileHashes[name])
	}

	return combined.Sum(nil), nil
}

// RemoveFileFromZip removes a file from a zip archive
func RemoveFileFromZip(zipData []byte, filename string) ([]byte, error) {
	if !IsZipFile(zipData) {
		return nil, errors.New("invalid zip file: missing magic bytes")
	}

	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to read zip: %w", err)
	}

	buf := new(bytes.Buffer)
	writer := zip.NewWriter(buf)

	// Copy all files except the one to remove
	for _, file := range reader.File {
		if file.Name == filename {
			continue // Skip this file
		}

		if err := copyZipFile(writer, file); err != nil {
			return nil, fmt.Errorf("failed to copy file %s: %w", file.Name, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zip writer: %w", err)
	}

	return buf.Bytes(), nil
}

// ReplaceFileInZip replaces an existing file in a zip archive
// This is an alias for AddFileToZip which already handles replacement
func ReplaceFileInZip(zipData []byte, filename string, content []byte) ([]byte, error) {
	return AddFileToZip(zipData, filename, content)
}

// CompareZipContents compares two zip files by computing and comparing their hashes
// Excludes metadata.toml from comparison to focus on actual content
func CompareZipContents(zipData1, zipData2 []byte) (bool, error) {
	// Normalize both zips: strip the version from metadata.toml so that
	// version differences don't affect comparison. This handles both:
	// - Content zips: content files match, metadata version irrelevant
	// - Metadata-only zips (config-only assets): everything except version is compared
	norm1, err := normalizeZipForComparison(zipData1)
	if err != nil {
		return false, fmt.Errorf("failed to normalize first zip: %w", err)
	}

	norm2, err := normalizeZipForComparison(zipData2)
	if err != nil {
		return false, fmt.Errorf("failed to normalize second zip: %w", err)
	}

	hash1, err := ComputeZipHash(norm1)
	if err != nil {
		return false, fmt.Errorf("failed to compute hash for first zip: %w", err)
	}

	hash2, err := ComputeZipHash(norm2)
	if err != nil {
		return false, fmt.Errorf("failed to compute hash for second zip: %w", err)
	}

	return bytes.Equal(hash1, hash2), nil
}

// normalizeZipForComparison strips the version field from metadata.toml
// so version differences don't affect content comparison.
func normalizeZipForComparison(zipData []byte) ([]byte, error) {
	metaBytes, err := ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		// No metadata.toml, return as-is
		return zipData, nil
	}

	// Zero out the version line in the raw TOML
	normalized := stripVersionFromTOML(metaBytes)

	return ReplaceFileInZip(zipData, "metadata.toml", normalized)
}

// stripVersionFromTOML zeroes out the [asset].version field in TOML bytes
// so version differences don't affect content hashing.
func stripVersionFromTOML(data []byte) []byte {
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return data // Can't parse, return as-is
	}

	if asset, ok := doc["asset"].(map[string]any); ok {
		asset["version"] = ""
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return data
	}
	return buf.Bytes()
}
