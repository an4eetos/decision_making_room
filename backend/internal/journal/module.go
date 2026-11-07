package journal

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"go.uber.org/fx"

	"github.com/an4eetos/decision-room/internal/infra/config"
	journalfs "github.com/an4eetos/decision-room/internal/journal/adapters/driven/fs"
	journalusecase "github.com/an4eetos/decision-room/internal/journal/usecase"
	"github.com/an4eetos/decision-room/internal/memory/port"
	memusecase "github.com/an4eetos/decision-room/internal/memory/usecase"
)

var Module = fx.Module("journal",
	fx.Provide(
		provideSyncFile,
		provideCapture,
	),
	fx.Invoke(startWatcher),
)

type syncFileHolder struct {
	sync *journalusecase.SyncFile
}

func provideSyncFile(cfg config.Config, repo port.MemoryRepository, ingest *memusecase.Ingest) (syncFileHolder, error) {
	if !cfg.JournalWatchEnabled || cfg.JournalWatchDir == "" {
		return syncFileHolder{}, nil
	}

	root, err := filepath.Abs(cfg.JournalWatchDir)
	if err != nil {
		return syncFileHolder{}, err
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return syncFileHolder{}, err
	}

	return syncFileHolder{sync: journalusecase.NewSyncFile(repo, ingest, root)}, nil
}

func startWatcher(lc fx.Lifecycle, holder syncFileHolder) {
	if holder.sync == nil {
		return
	}

	watcher, err := journalfs.NewWatcher(holder.sync)
	if err != nil {
		log.Printf("journal watcher disabled: %v", err)
		return
	}

	var cancel context.CancelFunc

	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			var runCtx context.Context
			runCtx, cancel = context.WithCancel(context.Background())
			log.Printf("watching journal at %s", holder.sync.Root())
			return watcher.Start(runCtx)
		},
		OnStop: func(_ context.Context) error {
			if cancel != nil {
				cancel()
			}
			return watcher.Stop()
		},
	})
}

// provideCapture is available even when the watcher is disabled: appending to a
// file still works, it just will not be ingested until a sync runs.
func provideCapture(cfg config.Config) *journalusecase.Capture {
	root, err := filepath.Abs(cfg.JournalWatchDir)
	if err != nil {
		log.Printf("quick capture disabled: %v", err)
		return journalusecase.NewCapture("")
	}
	return journalusecase.NewCapture(root)
}
