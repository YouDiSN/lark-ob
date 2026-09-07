package media

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/youdisn/lark-ob/internal/store"
)

var (
	messageIDPattern = regexp.MustCompile(`^om_[A-Za-z0-9_-]+$`)
	imageKeyPattern  = regexp.MustCompile(`^img_[A-Za-z0-9_-]+$`)
)

type downloader interface {
	DownloadImage(context.Context, string, string, string, string) error
}

type Service struct {
	store      *store.Store
	downloader downloader
	cacheDir   string
	mu         sync.Mutex
}

func New(st *store.Store, dl downloader, cacheDir string) *Service {
	return &Service{store: st, downloader: dl, cacheDir: cacheDir}
}

func DefaultCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "lark-ob", "message-images")
	}
	return filepath.Join("data", "message-images")
}

func (s *Service) Image(ctx context.Context, messageID, imageKey string) (string, string, error) {
	if !messageIDPattern.MatchString(messageID) || !imageKeyPattern.MatchString(imageKey) {
		return "", "", fmt.Errorf("无效的图片资源")
	}
	allowed, err := s.store.MessageContainsResource(ctx, messageID, imageKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", os.ErrNotExist
		}
		return "", "", err
	}
	if !allowed {
		return "", "", os.ErrNotExist
	}
	digest := sha256.Sum256([]byte(messageID + "\x00" + imageKey))
	filename := fmt.Sprintf("%x", digest[:])
	path := filepath.Join(s.cacheDir, filename)
	if normalizeExisting(path) {
		return path, detectContentType(path), nil
	}
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return path, detectContentType(path), nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return path, detectContentType(path), nil
	}
	if err := os.MkdirAll(s.cacheDir, 0o700); err != nil {
		return "", "", err
	}
	if err := s.downloader.DownloadImage(ctx, messageID, imageKey, s.cacheDir, filename); err != nil {
		return "", "", err
	}
	return path, detectContentType(path), nil
}

func normalizeExisting(path string) bool {
	matches, _ := filepath.Glob(path + ".*")
	for _, candidate := range matches {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			if os.Rename(candidate, path) == nil {
				return true
			}
		}
	}
	return false
}

func detectContentType(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()
	buffer := make([]byte, 512)
	n, _ := file.Read(buffer)
	return http.DetectContentType(buffer[:n])
}
