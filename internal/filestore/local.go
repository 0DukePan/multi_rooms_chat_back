package filestore

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// LocalFileStore manages file operations on the local file system.
type LocalFileStore struct {
	storagePath string
	baseURL     string
}

// NewLocalFileStore creates a new LocalFileStore instance.
func NewLocalFileStore(storagePath, baseURL string) (*LocalFileStore, error) {
	// Ensure the storage path exists
	if err := os.MkdirAll(storagePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory %s: %w", storagePath, err)
	}

	return &LocalFileStore{
		storagePath: storagePath,
		baseURL:     baseURL,
	}, nil
}

// SaveFile saves an uploaded file to local storage.
// It returns the file's unique key (path relative to storagePath) and its full URL.
func (l *LocalFileStore) SaveFile(reader io.Reader, filename string) (string, string, error) {
	// Generate a unique file key
	fileExtension := filepath.Ext(filename)
	uniqueFileName := fmt.Sprintf("%s%s", uuid.New().String(), fileExtension)
	fileKey := filepath.Join("uploads", time.Now().Format("2006/01/02"), uniqueFileName) // Organize by date

	fullPath := filepath.Join(l.storagePath, fileKey)

	// Ensure the directory for the file exists
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", "", fmt.Errorf("failed to create directory for file: %w", err)
	}

	outFile, err := os.Create(fullPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to create file %s: %w", fullPath, err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to write file %s: %w", fullPath, err)
	}

	fileURL := fmt.Sprintf("%s/%s", l.baseURL, fileKey)

	return fileKey, fileURL, nil
}
