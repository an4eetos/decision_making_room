package memory

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	genport "github.com/an4eetos/decision-room/internal/generals/port"
	"github.com/an4eetos/decision-room/internal/infra/config"
	"github.com/an4eetos/decision-room/internal/infra/gemini"
	"github.com/an4eetos/decision-room/internal/infra/ollama"
	memfs "github.com/an4eetos/decision-room/internal/memory/adapters/driven/fs"
	mempostgres "github.com/an4eetos/decision-room/internal/memory/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/usecase"
	modeservice "github.com/an4eetos/decision-room/internal/modes/service"
)

var Module = fx.Module("memory",
	fx.Provide(
		provideAI,
		provideLLM,
		provideToolLLM,
		provideEmbedder,
		provideRepository,
		provideDoctrineStore,
		provideDoctrineIndex,
		provideChatRepository,
		provideInitialContext,
		provideRetrieve,
		provideMemoryTools,
		provideIngest,
		provideReindex,
		provideSearch,
		provideDelete,
		provideConsult,
		provideChat,
	),
)

// aiBundle exists because one client implements all three ports; fx needs them
// separated to inject them independently.
type aiBundle struct {
	LLM      port.LLM
	ToolLLM  port.ToolLLM
	Embedder port.Embedder
}

func provideAI(cfg config.Config) (aiBundle, error) {
	switch cfg.LLMProvider {
	case "gemini":
		client := gemini.NewClient(
			cfg.GeminiBaseURL,
			cfg.GeminiAPIKey,
			cfg.GeminiChatModels,
			cfg.GeminiEmbedModel,
		)
		return aiBundle{LLM: client, ToolLLM: client, Embedder: client}, nil
	default:
		client := ollama.NewClient(cfg.OllamaBaseURL, cfg.OllamaChatModel, cfg.OllamaEmbedModel)
		return aiBundle{LLM: client, ToolLLM: client, Embedder: client}, nil
	}
}

func provideLLM(ai aiBundle) port.LLM         { return ai.LLM }
func provideToolLLM(ai aiBundle) port.ToolLLM { return ai.ToolLLM }

// provideEmbedder memoises, so the doctrine index reuses the question vector
// retrieval just computed instead of embedding the same text twice.
func provideEmbedder(ai aiBundle) port.Embedder {
	return usecase.NewMemoEmbedder(ai.Embedder, 32)
}

func provideRepository(pool *pgxpool.Pool) port.MemoryRepository {
	return mempostgres.NewRepository(pool)
}

func provideDoctrineStore(pool *pgxpool.Pool) port.DoctrineVectorStore {
	return mempostgres.NewDoctrineRepository(pool)
}

// doctrineBuildTimeout bounds the first-run embedding of the roster. Later
// starts load from Postgres and finish in milliseconds.
const doctrineBuildTimeout = 3 * time.Minute

// provideDoctrineIndex builds in the background. Boot does not wait on a
// hundred-odd embedding calls: until the vectors land, passage selection falls
// back to word overlap and every answer still gets its doctrine.
func provideDoctrineIndex(
	lc fx.Lifecycle,
	registry genport.Registry,
	embedder port.Embedder,
	store port.DoctrineVectorStore,
) *usecase.DoctrineIndex {
	index := usecase.NewDoctrineIndex(registry, embedder, store)

	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				buildCtx, done := context.WithTimeout(ctx, doctrineBuildTimeout)
				defer done()
				if err := index.Build(buildCtx); err != nil {
					log.Printf("doctrine: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
	return index
}

func provideChatRepository(pool *pgxpool.Pool) port.ChatRepository {
	return mempostgres.NewChatRepository(pool)
}

func provideInitialContext(cfg config.Config) port.InitialContextReader {
	return memfs.NewInitialContextReader(cfg.AboutMeFile, cfg.ContextDir, cfg.InitialContextMaxRunes)
}

func provideRetrieve(repo port.MemoryRepository, embedder port.Embedder, cfg config.Config) *usecase.Retrieve {
	return usecase.NewRetrieve(repo, embedder, cfg.RetrievalCandidates, cfg.RetrievalTopK)
}

func provideReindex(repo port.MemoryRepository, embedder port.Embedder) *usecase.Reindex {
	return usecase.NewReindex(repo, embedder)
}

func provideIngest(repo port.MemoryRepository, embedder port.Embedder) *usecase.Ingest {
	return usecase.NewIngest(repo, embedder)
}

func provideSearch(repo port.MemoryRepository, retriever *usecase.Retrieve) *usecase.Search {
	return usecase.NewSearch(repo, retriever)
}

func provideDelete(repo port.MemoryRepository) *usecase.Delete {
	return usecase.NewDelete(repo)
}

func provideMemoryTools(retriever *usecase.Retrieve, repo port.MemoryRepository, generals genport.Registry) *usecase.MemoryToolExecutor {
	return usecase.NewMemoryToolExecutor(retriever, repo, generals)
}

func provideConsult(
	repo port.MemoryRepository,
	retriever *usecase.Retrieve,
	llm port.LLM,
	toolLLM port.ToolLLM,
	initialContext port.InitialContextReader,
	tools *usecase.MemoryToolExecutor,
	registry genport.Registry,
	detector *modeservice.Detector,
	doctrine *usecase.DoctrineIndex,
	cfg config.Config,
) *usecase.Consult {
	return usecase.NewConsult(
		repo,
		retriever,
		llm,
		toolLLM,
		initialContext,
		tools,
		registry,
		detector,
		doctrine,
		domain.ParseTier(cfg.DefaultTier, domain.TierStandard),
		domain.ParseTier(cfg.MaxTier, domain.TierDeep),
	)
}

type chatParams struct {
	fx.In

	Repo      port.ChatRepository
	LLM       port.LLM
	Consult   *usecase.Consult
	Observers []port.TurnObserver `group:"turn_observers"`
}

// provideChat collects every turn observer from the "turn_observers" group, so a
// module can react to conversation without chat importing it.
func provideChat(p chatParams) *usecase.Chat {
	return usecase.NewChat(p.Repo, p.LLM, p.Consult, p.Observers...)
}
