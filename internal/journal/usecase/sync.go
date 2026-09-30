package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/an4eetos/decision-room/internal/journal/service"
	"github.com/an4eetos/decision-room/internal/memory/port"
	memservice "github.com/an4eetos/decision-room/internal/memory/service"
	memusecase "github.com/an4eetos/decision-room/internal/memory/usecase"
)

type SyncFile struct {
	repo   port.MemoryRepository
	ingest *memusecase.Ingest
	root   string

	// pathLocks serialises SyncPath/RemovePath per source path. The startup
	// SyncAll() walk and the debounced fsnotify watcher can both act on the
	// same file concurrently (e.g. a capture lands mid-embedding of that same
	// file's initial sync); without this, both goroutines can pass the
	// content-hash guard in SyncPath before either commits its insert,
	// producing two rows for one file.
	mu        sync.Mutex
	pathLocks map[string]*sync.Mutex
}

func NewSyncFile(repo port.MemoryRepository, ingest *memusecase.Ingest, root string) *SyncFile {
	return &SyncFile{
		repo:      repo,
		ingest:    ingest,
		root:      root,
		pathLocks: make(map[string]*sync.Mutex),
	}
}

func (u *SyncFile) lockFor(sourcePath string) *sync.Mutex {
	u.mu.Lock()
	defer u.mu.Unlock()

	l, ok := u.pathLocks[sourcePath]
	if !ok {
		l = &sync.Mutex{}
		u.pathLocks[sourcePath] = l
	}
	return l
}

func (u *SyncFile) Root() string {
	return u.root
}

func (u *SyncFile) SyncPath(ctx context.Context, absPath string) error {
	sourcePath, ok := u.relativeSourcePath(absPath)
	if !ok {
		return nil
	}

	lock := u.lockFor(sourcePath)
	lock.Lock()
	defer lock.Unlock()

	if service.IsExcludedFromJournalSync(sourcePath) {
		return u.repo.DeleteBySourcePath(ctx, sourcePath)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read journal file %s: %w", sourcePath, err)
	}

	body := strings.TrimSpace(string(content))
	if body == "" {
		return u.repo.DeleteBySourcePath(ctx, sourcePath)
	}

	hash := hashContent(body)
	existing, found, err := u.repo.SourceContentHash(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("lookup source hash: %w", err)
	}
	if found && existing == hash {
		return nil
	}

	if err := u.repo.DeleteBySourcePath(ctx, sourcePath); err != nil {
		return fmt.Errorf("clear previous chunks: %w", err)
	}

	kind, tags := service.ClassifyRelativePath(sourcePath)
	title := service.TitleFromPath(sourcePath)

	_, err = u.ingest.Execute(ctx, memusecase.IngestInput{
		Kind:  kind,
		Title: title,
		Body:  body,
		Tags:  tags,
		Metadata: map[string]string{
			"source":       "filesystem",
			"source_path":  sourcePath,
			"content_hash": hash,
		},
	})
	if err != nil {
		return fmt.Errorf("ingest journal file %s: %w", sourcePath, err)
	}

	return nil
}

func (u *SyncFile) RemovePath(ctx context.Context, absPath string) error {
	sourcePath, ok := u.relativeSourcePath(absPath)
	if !ok {
		return nil
	}

	lock := u.lockFor(sourcePath)
	lock.Lock()
	defer lock.Unlock()

	if err := u.repo.DeleteBySourcePath(ctx, sourcePath); err != nil {
		return fmt.Errorf("remove journal file %s: %w", sourcePath, err)
	}

	return nil
}

func (u *SyncFile) SyncAll(ctx context.Context) error {
	info, err := os.Stat(u.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat journal root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("journal watch path is not a directory: %s", u.root)
	}

	return filepath.WalkDir(u.root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !service.IsJournalFile(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(u.root, path)
		if err != nil {
			return err
		}
		if service.IsExcludedFromJournalSync(filepath.ToSlash(rel)) {
			return nil
		}
		if err := u.SyncPath(ctx, path); err != nil {
			return err
		}
		return nil
	})
}

func (u *SyncFile) relativeSourcePath(absPath string) (string, bool) {
	if !service.IsJournalFile(absPath) {
		return "", false
	}

	rel, err := filepath.Rel(u.root, absPath)
	if err != nil {
		return "", false
	}
	if strings.HasPrefix(rel, "..") {
		return "", false
	}

	return filepath.ToSlash(rel), true
}

// hashContent folds the ingest version into the hash so that improving the
// chunker invalidates every stored file.
//
// Without it the short-circuit in SyncPath sees an unchanged file, skips it,
// and the improvement is invisible on everything already saved — which looks
// exactly like the improvement not working.
func hashContent(body string) string {
	versioned := fmt.Sprintf("v%d\n%s", memservice.IngestVersion, body)
	sum := sha256.Sum256([]byte(versioned))
	return hex.EncodeToString(sum[:])
}
