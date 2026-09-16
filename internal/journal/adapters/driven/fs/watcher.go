package fs

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	journalusecase "github.com/an4eetos/decision-room/internal/journal/usecase"
)

const debounceDelay = 400 * time.Millisecond

type Watcher struct {
	syncFile *journalusecase.SyncFile
	watcher  *fsnotify.Watcher
	pending  map[string]*time.Timer
	mu       sync.Mutex
}

func NewWatcher(syncFile *journalusecase.SyncFile) (*Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		syncFile: syncFile,
		watcher:  watcher,
		pending:  make(map[string]*time.Timer),
	}, nil
}

func (w *Watcher) Start(ctx context.Context) error {
	root := w.syncFile.Root()
	if err := w.addRecursive(root); err != nil {
		return err
	}

	// Initial sync can be slow (embedding + DB writes) so it must not block
	// app startup. We run it in the background and keep watching immediately.
	go func() {
		if err := w.syncFile.SyncAll(ctx); err != nil {
			log.Printf("journal initial sync: %v", err)
		}
	}()

	go w.loop(ctx)
	return nil
}

func (w *Watcher) Stop() error {
	w.mu.Lock()
	for path, timer := range w.pending {
		timer.Stop()
		delete(w.pending, path)
	}
	w.mu.Unlock()
	return w.watcher.Close()
}

func (w *Watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		return w.watcher.Add(path)
	})
}

func (w *Watcher) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(ctx, event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("journal watcher error: %v", err)
		}
	}
}

func (w *Watcher) handleEvent(ctx context.Context, event fsnotify.Event) {
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if err := w.watcher.Add(event.Name); err != nil {
				log.Printf("journal watch add dir %s: %v", event.Name, err)
			}
		}
	}

	switch {
	case event.Has(fsnotify.Remove), event.Has(fsnotify.Rename):
		w.schedule(ctx, event.Name, func(runCtx context.Context) {
			if err := w.syncFile.RemovePath(runCtx, event.Name); err != nil {
				log.Printf("journal remove %s: %v", event.Name, err)
			}
		})
	case event.Has(fsnotify.Write), event.Has(fsnotify.Create):
		w.schedule(ctx, event.Name, func(runCtx context.Context) {
			if err := w.syncFile.SyncPath(runCtx, event.Name); err != nil {
				log.Printf("journal sync %s: %v", event.Name, err)
			}
		})
	}
}

func (w *Watcher) schedule(ctx context.Context, path string, fn func(context.Context)) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if timer, ok := w.pending[path]; ok {
		timer.Stop()
	}

	w.pending[path] = time.AfterFunc(debounceDelay, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()
		fn(ctx)
	})
}
