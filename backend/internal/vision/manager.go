package vision

import (
	"context"
	"fmt"
	"sync"

	"llm-eyes/internal/bot"
)

// ModelInfo represents a model's metadata
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Description string `json:"description"`
}

// AvailableModels lists all supported models
var AvailableModels = []ModelInfo{
	{
		ID:          "groq-llama4-scout",
		Name:        "Llama 4 Scout",
		Provider:    "Groq",
		Description: "Fast vision model (~200-400ms)",
	},
	{
		ID:          "gemini-2.0-flash",
		Name:        "Gemini 2.0 Flash",
		Provider:    "Google",
		Description: "Stable and reliable (~1.5-2s)",
	},
	{
		ID:          "gemini-live",
		Name:        "Gemini Live",
		Provider:    "Google",
		Description: "WebSocket single-shot (~2-3s)",
	},
	{
		ID:          "gemini-live-streaming",
		Name:        "Gemini Live Streaming",
		Provider:    "Google",
		Description: "Continuous 1FPS stream (~200-500ms)",
	},
}

// ModelManager manages vision processors and allows switching
type ModelManager struct {
	mu              sync.RWMutex
	currentModel    string
	currentProcessor bot.VisionProcessor
	groqKey         string
	geminiKey       string
}

// NewModelManager creates a new ModelManager
func NewModelManager(groqKey, geminiKey string) *ModelManager {
	return &ModelManager{
		groqKey:   groqKey,
		geminiKey: geminiKey,
	}
}

// GetAvailableModels returns models that have API keys configured
func (m *ModelManager) GetAvailableModels() []ModelInfo {
	var available []ModelInfo
	for _, model := range AvailableModels {
		switch model.ID {
		case "groq-llama4-scout":
			if m.groqKey != "" {
				available = append(available, model)
			}
		case "gemini-2.0-flash", "gemini-live", "gemini-live-streaming":
			if m.geminiKey != "" {
				available = append(available, model)
			}
		}
	}
	return available
}

// GetCurrentModel returns the current model ID
func (m *ModelManager) GetCurrentModel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentModel
}

// GetProcessor returns the current vision processor
func (m *ModelManager) GetProcessor() bot.VisionProcessor {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentProcessor
}

// SwitchModel switches to a different model (clears history)
func (m *ModelManager) SwitchModel(ctx context.Context, modelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Close existing processor if any
	if m.currentProcessor != nil {
		if closer, ok := m.currentProcessor.(interface{ Close() error }); ok {
			closer.Close()
		}
	}

	var processor bot.VisionProcessor
	var err error

	switch modelID {
	case "groq-llama4-scout":
		if m.groqKey == "" {
			return fmt.Errorf("GROQ_API_KEY not configured")
		}
		processor, err = NewGroqProcessor(m.groqKey)
		if err != nil {
			return fmt.Errorf("failed to create Groq processor: %w", err)
		}

	case "gemini-2.0-flash":
		if m.geminiKey == "" {
			return fmt.Errorf("GEMINI_API_KEY not configured")
		}
		processor, err = NewGeminiProcessor(ctx, m.geminiKey)
		if err != nil {
			return fmt.Errorf("failed to create Gemini processor: %w", err)
		}

	case "gemini-live":
		if m.geminiKey == "" {
			return fmt.Errorf("GEMINI_API_KEY not configured")
		}
		processor, err = NewGeminiLiveProcessor(m.geminiKey)
		if err != nil {
			return fmt.Errorf("failed to create Gemini Live processor: %w", err)
		}

	case "gemini-live-streaming":
		if m.geminiKey == "" {
			return fmt.Errorf("GEMINI_API_KEY not configured")
		}
		processor, err = NewGeminiLiveStreamingProcessor(m.geminiKey)
		if err != nil {
			return fmt.Errorf("failed to create Gemini Live Streaming processor: %w", err)
		}

	default:
		return fmt.Errorf("unknown model: %s", modelID)
	}

	m.currentProcessor = processor
	m.currentModel = modelID
	return nil
}

// InitDefault initializes with the first available model
func (m *ModelManager) InitDefault(ctx context.Context) error {
	// Prefer Groq for speed
	if m.groqKey != "" {
		return m.SwitchModel(ctx, "groq-llama4-scout")
	}
	if m.geminiKey != "" {
		return m.SwitchModel(ctx, "gemini-2.0-flash")
	}
	return fmt.Errorf("no API keys configured")
}
