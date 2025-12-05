package bot

import (
	"context"
	"encoding/json"
	"log"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
)

// VisionProcessor interface for Gemini API
type VisionProcessor interface {
	AnalyzeImage(ctx context.Context, dataURL string) (string, error)
	AddContext(ctx context.Context, dataURL string) error
	Chat(ctx context.Context, dataURL string, userMessage string) (string, error)
}

// ChatRequest represents a chat request with image
type ChatRequest struct {
	ImageData   string
	UserMessage string
}

// Bot represents the AI Vision Bot
type Bot struct {
	room            *lksdk.Room
	visionProcessor VisionProcessor

	// Channel-based goroutine communication
	chatChan   chan ChatRequest // User chat requests
	resultChan chan string      // Gemini analysis results

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBot creates a new Bot instance
func NewBot(room *lksdk.Room, visionProcessor VisionProcessor) *Bot {
	ctx, cancel := context.WithCancel(context.Background())
	return &Bot{
		room:            room,
		visionProcessor: visionProcessor,
		chatChan:        make(chan ChatRequest, 10),
		resultChan:      make(chan string, 10),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Stop gracefully shuts down the bot
func (b *Bot) Stop() {
	log.Println("🛑 Stopping Bot goroutines...")
	b.cancel()
	close(b.chatChan)
	close(b.resultChan)
	log.Println("✅ Bot stopped")
}

// DataChannelMessage represents incoming/outgoing messages
type DataChannelMessage struct {
	Type    string `json:"type"`    // "chat"
	Data    string `json:"data"`    // base64 image
	Text    string `json:"text"`    // user message or result text
	Message string `json:"message"` // alias for Text (frontend compatibility)
}

// Start initializes the bot and starts all goroutines
func (b *Bot) Start() {
	log.Println("🎬 Starting Bot goroutines...")

	// Goroutine #1: Process user chat
	go b.processChat()

	// Goroutine #2: Send results
	go b.sendResults()

	log.Println("✅ Bot is ready to process chat")
}

// HandleDataPacket receives chat from frontend via DataChannel
func (b *Bot) HandleDataPacket(data lksdk.DataPacket, params lksdk.DataReceiveParams) {
	userPacket, ok := data.(*lksdk.UserDataPacket)
	if !ok {
		return
	}

	var msg DataChannelMessage
	if err := json.Unmarshal(userPacket.Payload, &msg); err != nil {
		log.Printf("❌ Failed to parse message: %v", err)
		return
	}

	// Only process chat type
	if msg.Type != "chat" {
		return
	}

	participantID := "unknown"
	if params.SenderIdentity != "" {
		participantID = string(params.SenderIdentity)
	}

	userMsg := msg.Text
	if userMsg == "" {
		userMsg = msg.Message
	}

	log.Printf("💬 Chat from %s: %s", participantID, userMsg)

	select {
	case b.chatChan <- ChatRequest{ImageData: msg.Data, UserMessage: userMsg}:
	default:
		log.Println("⚠️  Chat channel full, dropping request")
	}
}

// processChat handles user chat requests
func (b *Bot) processChat() {
	for {
		select {
		case <-b.ctx.Done():
			log.Println("🛑 processChat goroutine stopped")
			return
		case req, ok := <-b.chatChan:
			if !ok {
				return
			}

			log.Printf("🤖 Processing chat: %s", req.UserMessage)

			apiCtx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
			result, err := b.visionProcessor.Chat(apiCtx, req.ImageData, req.UserMessage)
			cancel()

			if err != nil {
				log.Printf("❌ Chat error: %v", err)
				continue
			}

			log.Printf("💬 Chat response: %s", result)

			select {
			case b.resultChan <- result:
			case <-b.ctx.Done():
				return
			default:
				log.Println("⚠️  Result channel full, dropping result")
			}
		}
	}
}

// sendResults sends results to frontend via DataChannel
func (b *Bot) sendResults() {
	for {
		select {
		case <-b.ctx.Done():
			log.Println("🛑 sendResults goroutine stopped")
			return
		case result, ok := <-b.resultChan:
			if !ok {
				log.Println("🛑 resultChan closed, sendResults exiting")
				return
			}

			msg := DataChannelMessage{
				Type: "chat",
				Text: result,
			}

			data, err := json.Marshal(msg)
			if err != nil {
				log.Printf("❌ Failed to marshal result: %v", err)
				continue
			}

			// Send via LiveKit DataChannel
			err = b.room.LocalParticipant.PublishDataPacket(
				lksdk.UserData(data),
				lksdk.WithDataPublishReliable(true),
			)
			if err != nil {
				log.Printf("❌ Failed to send result: %v", err)
				continue
			}

			log.Printf("📤 Result sent to frontend")
		}
	}
}
