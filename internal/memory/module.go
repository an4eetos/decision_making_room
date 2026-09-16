package memory

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/an4eetos/decision-room/internal/infra/config"
	"github.com/an4eetos/decision-room/internal/infra/gemini"
	"github.com/an4eetos/decision-room/internal/infra/ollama"
	memfs "github.com/an4eetos/decision-room/internal/memory/adapters/driven/fs"
	mempostgres "github.com/an4eetos/decision-room/internal/memory/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/usecase"
)

var Module = fx.Module("memory",
	fx.Provide(
		provideAI,
		provideLLM,
		provideToolLLM,
		provideEmbedder,
		provideRepository,
		provideChatRepository,
		provideInitialContext,
		provideRetrieve,
		provideMemoryTools,
		provideIngest,
		provideSearch,
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

func provideLLM(ai aiBundle) port.LLM           { return ai.LLM }
func provideToolLLM(ai aiBundle) port.ToolLLM   { return ai.ToolLLM }
func provideEmbedder(ai aiBundle) port.Embedder { return ai.Embedder }

func provideRepository(pool *pgxpool.Pool) port.MemoryRepository {
	return mempostgres.NewRepository(pool)
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

func provideIngest(repo port.MemoryRepository, embedder port.Embedder) *usecase.Ingest {
	return usecase.NewIngest(repo, embedder)
}

func provideSearch(repo port.MemoryRepository, retriever *usecase.Retrieve) *usecase.Search {
	return usecase.NewSearch(repo, retriever)
}

func provideMemoryTools(retriever *usecase.Retrieve, repo port.MemoryRepository, cfg config.Config) *usecase.MemoryToolExecutor {
	return usecase.NewMemoryToolExecutor(retriever, repo, cfg.RetrievalTopK)
}

func provideConsult(
	repo port.MemoryRepository,
	retriever *usecase.Retrieve,
	llm port.LLM,
	toolLLM port.ToolLLM,
	initialContext port.InitialContextReader,
	tools *usecase.MemoryToolExecutor,
	cfg config.Config,
) *usecase.Consult {
	return usecase.NewConsult(
		repo,
		retriever,
		llm,
		toolLLM,
		initialContext,
		tools,
		cfg.RetrievalTopK,
		cfg.AgenticRAGEnabled,
		cfg.AgentMaxToolRounds,
	)
}

func provideChat(repo port.ChatRepository, llm port.LLM, consult *usecase.Consult) *usecase.Chat {
	return usecase.NewChat(repo, llm, consult)
}
