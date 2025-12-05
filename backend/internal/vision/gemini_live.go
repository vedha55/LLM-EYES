package vision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	geminiLiveWSURL = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"
	geminiLiveModel = "gemini-2.0-flash-live-001"
)

// GeminiLiveProcessor handles Gemini Live API via WebSocket
type GeminiLiveProcessor struct {
	apiKey   string
	conn     *websocket.Conn
	mu       sync.Mutex
	setupDone bool
}

// Setup message structure
type liveSetupMessage struct {
	Setup *liveSetup `json:"setup"`
}

type liveSetup struct {
	Model              string              `json:"model"`
	GenerationConfig   *liveGenerationConfig `json:"generationConfig,omitempty"`
	SystemInstruction  *liveContent        `json:"systemInstruction,omitempty"`
}

type liveGenerationConfig struct {
	ResponseModalities []string `json:"responseModalities,omitempty"`
	Temperature        float64  `json:"temperature,omitempty"`
	MaxOutputTokens    int      `json:"maxOutputTokens,omitempty"`
}

type liveContent struct {
	Parts []livePart `json:"parts"`
}

type livePart struct {
	Text string `json:"text,omitempty"`
}

// Client content message
type liveClientContent struct {
	ClientContent *clientContentData `json:"clientContent"`
}

type clientContentData struct {
	Turns    []liveTurn `json:"turns"`
	TurnComplete bool   `json:"turnComplete"`
}

type liveTurn struct {
	Role  string      `json:"role"`
	Parts []livePart  `json:"parts"`
}

// Realtime input message (for video/images)
type liveRealtimeInput struct {
	RealtimeInput *realtimeInputData `json:"realtimeInput"`
}

type realtimeInputData struct {
	Video *blob `json:"video,omitempty"` // Use video field instead of deprecated mediaChunks
}

type blob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
}

// Client content with inline image
type liveClientContentWithImage struct {
	ClientContent *clientContentWithImageData `json:"clientContent"`
}

type clientContentWithImageData struct {
	Turns        []liveTurnWithImage `json:"turns"`
	TurnComplete bool                `json:"turnComplete"`
}

type liveTurnWithImage struct {
	Role  string          `json:"role"`
	Parts []livePartMulti `json:"parts"`
}

type livePartMulti struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inlineData,omitempty"`
}

type inlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// Server response structures
type liveServerMessage struct {
	SetupComplete    *struct{} `json:"setupComplete,omitempty"`
	ServerContent    *serverContent `json:"serverContent,omitempty"`
	ToolCall         *struct{} `json:"toolCall,omitempty"`
}

type serverContent struct {
	ModelTurn *modelTurn `json:"modelTurn,omitempty"`
	TurnComplete bool `json:"turnComplete,omitempty"`
}

type modelTurn struct {
	Parts []serverPart `json:"parts"`
}

type serverPart struct {
	Text string `json:"text,omitempty"`
}

// NewGeminiLiveProcessor creates a new Gemini Live processor
func NewGeminiLiveProcessor(apiKey string) (*GeminiLiveProcessor, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is required")
	}

	processor := &GeminiLiveProcessor{
		apiKey: apiKey,
	}

	// Connect immediately
	if err := processor.connect(); err != nil {
		return nil, err
	}

	return processor, nil
}

// connect establishes WebSocket connection
func (g *GeminiLiveProcessor) connect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Close existing connection if any
	if g.conn != nil {
		g.conn.Close()
	}

	// Connect with API key in URL
	url := fmt.Sprintf("%s?key=%s", geminiLiveWSURL, g.apiKey)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to Gemini Live: %w", err)
	}
	g.conn = conn
	g.setupDone = false

	// Send setup message
	setup := liveSetupMessage{
		Setup: &liveSetup{
			Model: fmt.Sprintf("models/%s", geminiLiveModel),
			GenerationConfig: &liveGenerationConfig{
				ResponseModalities: []string{"TEXT"},
				Temperature:        0.4,
				MaxOutputTokens:    200,
			},
			SystemInstruction: &liveContent{
				Parts: []livePart{
					{Text: "너는 실시간 영상을 보고 있는 AI 어시스턴트야. " +
						"주기적으로 이미지가 들어오면 컨텍스트로 기억해. " +
						"유저가 질문하면 지금까지 본 이미지들의 맥락을 바탕으로 한국어로 익살스럽고 유머있게 대답해."},
				},
			},
		},
	}

	if err := conn.WriteJSON(setup); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send setup: %w", err)
	}

	// Wait for setup complete
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			conn.Close()
			return fmt.Errorf("failed to read setup response: %w", err)
		}

		var resp liveServerMessage
		if err := json.Unmarshal(message, &resp); err != nil {
			continue
		}

		if resp.SetupComplete != nil {
			g.setupDone = true
			log.Println("✅ Gemini Live session established")
			break
		}
	}

	return nil
}

// ensureConnected reconnects if needed
func (g *GeminiLiveProcessor) ensureConnected() error {
	g.mu.Lock()
	if g.conn != nil && g.setupDone {
		g.mu.Unlock()
		return nil
	}
	g.mu.Unlock()
	return g.connect()
}

// Chat handles user chat with current image
func (g *GeminiLiveProcessor) Chat(ctx context.Context, dataURL string, userMessage string) (string, error) {
	if err := g.ensureConnected(); err != nil {
		return "", err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Parse image
	imageData, mimeType, err := g.parseDataURL(dataURL)
	if err != nil {
		return "", err
	}

	// Send image + text together in clientContent with inlineData
	msg := liveClientContentWithImage{
		ClientContent: &clientContentWithImageData{
			Turns: []liveTurnWithImage{
				{
					Role: "user",
					Parts: []livePartMulti{
						{InlineData: &inlineData{MimeType: mimeType, Data: imageData}},
						{Text: userMessage},
					},
				},
			},
			TurnComplete: true,
		},
	}
	if err := g.conn.WriteJSON(msg); err != nil {
		g.conn = nil
		return "", fmt.Errorf("failed to send message: %w", err)
	}

	// Read response with timeout
	var result strings.Builder
	deadline := time.Now().Add(30 * time.Second)
	g.conn.SetReadDeadline(deadline)

	for {
		_, message, err := g.conn.ReadMessage()
		if err != nil {
			if result.Len() > 0 {
				break
			}
			g.conn = nil
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		var resp liveServerMessage
		if err := json.Unmarshal(message, &resp); err != nil {
			continue
		}

		if resp.ServerContent != nil {
			if resp.ServerContent.ModelTurn != nil {
				for _, part := range resp.ServerContent.ModelTurn.Parts {
					if part.Text != "" {
						result.WriteString(part.Text)
					}
				}
			}
			if resp.ServerContent.TurnComplete {
				break
			}
		}
	}

	// Reset deadline
	g.conn.SetReadDeadline(time.Time{})

	if result.Len() == 0 {
		return "", fmt.Errorf("no response from Gemini Live")
	}

	return result.String(), nil
}

// AddContext adds an image to context (simplified for Live API)
func (g *GeminiLiveProcessor) AddContext(ctx context.Context, dataURL string) error {
	// Live API maintains context automatically within the session
	return nil
}

// AnalyzeImage analyzes an image (legacy method)
func (g *GeminiLiveProcessor) AnalyzeImage(ctx context.Context, dataURL string) (string, error) {
	return g.Chat(ctx, dataURL, "What do you see? Describe briefly.")
}

// parseDataURL extracts base64 data and mime type from data URL
func (g *GeminiLiveProcessor) parseDataURL(dataURL string) (string, string, error) {
	// Format: data:image/jpeg;base64,/9j/4AAQ...
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid data URL format")
	}

	// Parse mime type from header
	header := parts[0] // "data:image/jpeg;base64"
	mimeType := "image/jpeg"
	if strings.Contains(header, "image/png") {
		mimeType = "image/png"
	} else if strings.Contains(header, "image/webp") {
		mimeType = "image/webp"
	}

	// Validate base64
	data := parts[1]
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return "", "", fmt.Errorf("invalid base64 data: %w", err)
	}

	return data, mimeType, nil
}

// Close closes the WebSocket connection
func (g *GeminiLiveProcessor) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.conn != nil {
		err := g.conn.Close()
		g.conn = nil
		return err
	}
	return nil
}
