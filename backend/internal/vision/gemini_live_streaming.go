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
	streamingWSURL = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"
	streamingModel = "gemini-2.0-flash-live-001"
)

// StreamChunk represents a streaming text chunk
type StreamChunk struct {
	Text    string
	IsFinal bool
}

// GeminiLiveStreamingProcessor handles continuous video streaming to Gemini Live
type GeminiLiveStreamingProcessor struct {
	apiKey string

	// Connection state
	conn      *websocket.Conn
	connMu    sync.Mutex
	setupDone bool

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc

	// Response handling
	responseChan chan string       // For legacy Chat method
	streamChan   chan StreamChunk  // For streaming mode
	setupChan    chan struct{}     // Signals setup complete
}

// NewGeminiLiveStreamingProcessor creates a new streaming processor
func NewGeminiLiveStreamingProcessor(apiKey string) (*GeminiLiveStreamingProcessor, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	processor := &GeminiLiveStreamingProcessor{
		apiKey:       apiKey,
		ctx:          ctx,
		cancel:       cancel,
		responseChan: make(chan string, 1),
		streamChan:   make(chan StreamChunk, 20),
		setupChan:    make(chan struct{}, 1),
	}

	// Connect
	if err := processor.connect(); err != nil {
		cancel()
		return nil, err
	}

	log.Println("🎬 Gemini Live Streaming processor initialized (1 FPS mode)")
	return processor, nil
}

// connect establishes WebSocket connection
func (g *GeminiLiveStreamingProcessor) connect() error {
	g.connMu.Lock()
	// Close existing connection
	if g.conn != nil {
		g.conn.Close()
		g.conn = nil
	}
	g.setupDone = false
	g.connMu.Unlock()

	// Drain setup channel
	select {
	case <-g.setupChan:
	default:
	}

	url := fmt.Sprintf("%s?key=%s", streamingWSURL, g.apiKey)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Send setup
	setup := liveSetupMessage{
		Setup: &liveSetup{
			Model: fmt.Sprintf("models/%s", streamingModel),
			GenerationConfig: &liveGenerationConfig{
				ResponseModalities: []string{"TEXT"},
				Temperature:        0.4,
				MaxOutputTokens:    200,
			},
			SystemInstruction: &liveContent{
				Parts: []livePart{
					{Text: "너는 실시간 영상을 보고 있는 AI 어시스턴트야. " +
						"비디오 프레임이 계속 들어오고 있어. 컨텍스트로 기억해. " +
						"유저가 질문하면 지금까지 본 영상의 맥락을 바탕으로 한국어로 익살스럽고 유머있게 대답해."},
				},
			},
		},
	}

	if err := conn.WriteJSON(setup); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send setup: %w", err)
	}

	// Set connection (not yet ready)
	g.connMu.Lock()
	g.conn = conn
	g.connMu.Unlock()

	// Start reader goroutine - it will handle setup complete
	// Pass conn directly to avoid race conditions
	go g.responseReader(conn)

	// Wait for setup complete with timeout
	select {
	case <-g.setupChan:
		log.Println("✅ Gemini Live Streaming session established")
		return nil
	case <-time.After(10 * time.Second):
		g.connMu.Lock()
		if g.conn != nil {
			g.conn.Close()
			g.conn = nil
		}
		g.connMu.Unlock()
		return fmt.Errorf("setup timeout")
	case <-g.ctx.Done():
		return g.ctx.Err()
	}
}

// responseReader reads responses in background (one instance per connection)
// Uses blocking read pattern - exits only when connection is closed or error occurs
func (g *GeminiLiveStreamingProcessor) responseReader(conn *websocket.Conn) {
	if conn == nil {
		return
	}

	defer func() {
		// Clean up: mark disconnected if this is still the active connection
		g.connMu.Lock()
		if g.conn == conn {
			g.conn = nil
			g.setupDone = false
		}
		g.connMu.Unlock()
	}()

	// Accumulate text across streaming chunks
	var accumulatedText strings.Builder

	for {
		// BLOCKING READ - no timeout
		// This blocks until: (1) message received, or (2) connection closed
		_, message, err := conn.ReadMessage()
		if err != nil {
			// ANY error means connection is dead - must exit immediately
			// Cannot call ReadMessage again after any error (gorilla/websocket limitation)
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				// Only log unexpected errors
				if !strings.Contains(err.Error(), "use of closed network connection") {
					log.Printf("⚠️ WebSocket read error: %v", err)
				}
			}
			return // EXIT - never continue after error
		}

		var resp liveServerMessage
		if err := json.Unmarshal(message, &resp); err != nil {
			continue // JSON parse error is OK to continue
		}

		// Handle setup complete
		if resp.SetupComplete != nil {
			g.connMu.Lock()
			g.setupDone = true
			g.connMu.Unlock()

			select {
			case g.setupChan <- struct{}{}:
			default:
			}
			continue
		}

		// Collect response text (streaming - send chunks immediately)
		if resp.ServerContent != nil {
			// Send each text chunk immediately to streamChan
			if resp.ServerContent.ModelTurn != nil {
				for _, part := range resp.ServerContent.ModelTurn.Parts {
					if part.Text != "" {
						accumulatedText.WriteString(part.Text)
						// Send chunk to stream channel
						select {
						case g.streamChan <- StreamChunk{Text: part.Text, IsFinal: false}:
						default:
							// Channel full, skip
						}
					}
				}
			}

			// When turn is complete, send final marker and accumulated text
			if resp.ServerContent.TurnComplete {
				// Send final marker to stream channel
				select {
				case g.streamChan <- StreamChunk{Text: "", IsFinal: true}:
				default:
				}

				// Also send to legacy responseChan for Chat() method
				if accumulatedText.Len() > 0 {
					result := accumulatedText.String()
					select {
					case g.responseChan <- result:
					default:
						select {
						case <-g.responseChan:
						default:
						}
						g.responseChan <- result
					}
					accumulatedText.Reset()
				}
			}
		}
	}
}

// ensureConnected reconnects if needed
func (g *GeminiLiveStreamingProcessor) ensureConnected() error {
	g.connMu.Lock()
	connected := g.conn != nil && g.setupDone
	g.connMu.Unlock()

	if connected {
		return nil
	}
	return g.connect()
}

// UpdateFrame sends a frame to Gemini (called at 1 FPS)
func (g *GeminiLiveStreamingProcessor) UpdateFrame(dataURL string) error {
	g.connMu.Lock()
	conn := g.conn
	connected := conn != nil && g.setupDone
	g.connMu.Unlock()

	if !connected {
		// Skip frame if not connected - Chat will reconnect
		return nil
	}

	imageData, mimeType, err := g.parseDataURL(dataURL)
	if err != nil {
		return err
	}

	frameMsg := liveRealtimeInput{
		RealtimeInput: &realtimeInputData{
			Video: &blob{
				MimeType: mimeType,
				Data:     imageData,
			},
		},
	}

	g.connMu.Lock()
	defer g.connMu.Unlock()

	if g.conn == nil {
		return nil // Skip silently
	}

	if err := g.conn.WriteJSON(frameMsg); err != nil {
		log.Printf("⚠️ Frame send failed: %v", err)
		g.conn = nil
		g.setupDone = false
		return err
	}

	return nil
}

// Chat sends text and waits for response
func (g *GeminiLiveStreamingProcessor) Chat(ctx context.Context, dataURL string, userMessage string) (string, error) {
	if err := g.ensureConnected(); err != nil {
		return "", err
	}

	// Send current frame with message
	imageData, mimeType, err := g.parseDataURL(dataURL)
	if err != nil {
		return "", err
	}

	g.connMu.Lock()
	if g.conn == nil {
		g.connMu.Unlock()
		return "", fmt.Errorf("not connected")
	}

	// Send image + text
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

	log.Printf("📤 Sending chat message: %s", userMessage)
	if err := g.conn.WriteJSON(msg); err != nil {
		log.Printf("❌ Failed to send message: %v", err)
		g.conn = nil
		g.setupDone = false
		g.connMu.Unlock()
		return "", err
	}
	g.connMu.Unlock()
	log.Println("📤 Message sent, waiting for response...")

	// Wait for response
	select {
	case result := <-g.responseChan:
		log.Printf("✅ Got response: %s", result)
		return result, nil
	case <-time.After(30 * time.Second):
		log.Println("❌ Response timeout")
		return "", fmt.Errorf("timeout")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// ChatStream sends chunks to the provided channel (implements StreamingVisionProcessor)
func (g *GeminiLiveStreamingProcessor) ChatStream(ctx context.Context, dataURL string, userMessage string, chunks chan<- string) error {
	if err := g.ensureConnected(); err != nil {
		return err
	}

	// Drain any old stream chunks
	for {
		select {
		case <-g.streamChan:
		default:
			goto sendMessage
		}
	}

sendMessage:
	// Send current frame with message
	imageData, mimeType, err := g.parseDataURL(dataURL)
	if err != nil {
		return err
	}

	g.connMu.Lock()
	if g.conn == nil {
		g.connMu.Unlock()
		return fmt.Errorf("not connected")
	}

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
		g.setupDone = false
		g.connMu.Unlock()
		return err
	}
	g.connMu.Unlock()

	// Read chunks from streamChan and forward to caller's channel
	for {
		select {
		case chunk := <-g.streamChan:
			if chunk.IsFinal {
				return nil // Done
			}
			if chunk.Text != "" {
				chunks <- chunk.Text
			}
		case <-time.After(30 * time.Second):
			return fmt.Errorf("stream timeout")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// AddContext implements interface
func (g *GeminiLiveStreamingProcessor) AddContext(ctx context.Context, dataURL string) error {
	return g.UpdateFrame(dataURL)
}

// AnalyzeImage implements interface
func (g *GeminiLiveStreamingProcessor) AnalyzeImage(ctx context.Context, dataURL string) (string, error) {
	return g.Chat(ctx, dataURL, "지금 뭐가 보여?")
}

// parseDataURL extracts base64 and mime type
func (g *GeminiLiveStreamingProcessor) parseDataURL(dataURL string) (string, string, error) {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid data URL")
	}

	mimeType := "image/jpeg"
	if strings.Contains(parts[0], "image/png") {
		mimeType = "image/png"
	}

	if _, err := base64.StdEncoding.DecodeString(parts[1]); err != nil {
		return "", "", fmt.Errorf("invalid base64")
	}

	return parts[1], mimeType, nil
}

// Close shuts down
func (g *GeminiLiveStreamingProcessor) Close() error {
	g.cancel()

	g.connMu.Lock()
	defer g.connMu.Unlock()

	if g.conn != nil {
		g.conn.Close()
		g.conn = nil
	}
	return nil
}
