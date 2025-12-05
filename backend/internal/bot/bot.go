package bot

import (
	"container/heap"
	"context"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
)

// VisionProcessor interface for Gemini API
type VisionProcessor interface {
	AnalyzeImage(ctx context.Context, dataURL string) (string, error)
	AddContext(ctx context.Context, dataURL string) error
	Chat(ctx context.Context, dataURL string, userMessage string) (string, error)
}

// StreamingVisionProcessor extends VisionProcessor with streaming support
type StreamingVisionProcessor interface {
	VisionProcessor
	// ChatStream sends chunks to the provided channel, closes when done
	ChatStream(ctx context.Context, dataURL string, userMessage string, chunks chan<- string) error
}

// ChatRequest represents a chat request with image
type ChatRequest struct {
	SeqNum      uint64 // Sequence number for ordering
	ImageData   string
	UserMessage string
}

// ChatResult represents a processed result with sequence number
type ChatResult struct {
	SeqNum uint64
	Text   string
	IsError bool
}

// ResultHeap implements a min-heap for reordering results by SeqNum
type ResultHeap []ChatResult

func (h ResultHeap) Len() int           { return len(h) }
func (h ResultHeap) Less(i, j int) bool { return h[i].SeqNum < h[j].SeqNum }
func (h ResultHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *ResultHeap) Push(x any) {
	*h = append(*h, x.(ChatResult))
}

func (h *ResultHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

const (
	NumWorkers = 3 // Number of parallel workers
)

// VisionProcessorGetter allows dynamic processor switching
type VisionProcessorGetter interface {
	GetProcessor() VisionProcessor
}

// Bot represents the AI Vision Bot
type Bot struct {
	room              *lksdk.Room
	visionProcessor   VisionProcessor        // Static processor (legacy)
	processorGetter   VisionProcessorGetter  // Dynamic processor getter

	// Channel-based goroutine communication
	chatChan   chan ChatRequest // User chat requests
	resultChan chan ChatResult  // Gemini analysis results with SeqNum

	// Sequence number management
	seqCounter    uint64 // Atomic counter for incoming requests
	nextExpected  uint64 // Next expected SeqNum to send

	// Reorder buffer
	reorderBuffer *ResultHeap
	reorderMu     sync.Mutex

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

// NewBot creates a new Bot instance (legacy - static processor)
func NewBot(room *lksdk.Room, visionProcessor VisionProcessor) *Bot {
	ctx, cancel := context.WithCancel(context.Background())
	h := &ResultHeap{}
	heap.Init(h)
	return &Bot{
		room:            room,
		visionProcessor: visionProcessor,
		chatChan:        make(chan ChatRequest, 10),
		resultChan:      make(chan ChatResult, 10),
		seqCounter:      0,
		nextExpected:    1,
		reorderBuffer:   h,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// NewBotWithManager creates a new Bot with dynamic model switching
func NewBotWithManager(room *lksdk.Room, getter VisionProcessorGetter) *Bot {
	ctx, cancel := context.WithCancel(context.Background())
	h := &ResultHeap{}
	heap.Init(h)
	return &Bot{
		room:            room,
		processorGetter: getter,
		chatChan:        make(chan ChatRequest, 10),
		resultChan:      make(chan ChatResult, 10),
		seqCounter:      0,
		nextExpected:    1,
		reorderBuffer:   h,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// getProcessor returns the current vision processor
func (b *Bot) getProcessor() VisionProcessor {
	if b.processorGetter != nil {
		return b.processorGetter.GetProcessor()
	}
	return b.visionProcessor
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
	Type      string `json:"type"`                // "chat" or "chat_stream"
	Data      string `json:"data,omitempty"`      // base64 image
	Text      string `json:"text,omitempty"`      // user message or result text
	Message   string `json:"message,omitempty"`   // alias for Text (frontend compatibility)
	SeqNum    uint64 `json:"seqNum,omitempty"`    // sequence number for streaming
	IsFinal   bool   `json:"isFinal,omitempty"`   // true if this is the last chunk
}

// Start initializes the bot and starts all goroutines
func (b *Bot) Start() {
	log.Println("🎬 Starting Bot goroutines...")

	// Start parallel workers for processing chat
	for i := 0; i < NumWorkers; i++ {
		go b.worker(i)
	}
	log.Printf("✅ Started %d parallel workers", NumWorkers)

	// Goroutine for reordering and sending results
	go b.reorderAndSend()

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

	// Assign sequence number atomically
	seqNum := atomic.AddUint64(&b.seqCounter, 1)

	select {
	case b.chatChan <- ChatRequest{SeqNum: seqNum, ImageData: msg.Data, UserMessage: userMsg}:
		log.Printf("📥 Request queued with SeqNum: %d", seqNum)
	default:
		log.Println("⚠️  Chat channel full, dropping request")
	}
}

// worker processes chat requests in parallel
func (b *Bot) worker(id int) {
	log.Printf("👷 Worker %d started", id)
	for {
		select {
		case <-b.ctx.Done():
			log.Printf("🛑 Worker %d stopped", id)
			return
		case req, ok := <-b.chatChan:
			if !ok {
				log.Printf("🛑 Worker %d: channel closed", id)
				return
			}

			log.Printf("👷 Worker %d processing SeqNum %d: %s", id, req.SeqNum, req.UserMessage)

			processor := b.getProcessor()
			if processor == nil {
				log.Printf("❌ Worker %d: no processor available", id)
				continue
			}

			startTime := time.Now()
			apiCtx, cancel := context.WithTimeout(b.ctx, 30*time.Second)

			// Check if processor supports streaming
			if streamProcessor, ok := processor.(StreamingVisionProcessor); ok {
				// Use streaming mode
				b.processStreaming(id, req, streamProcessor, apiCtx)
				cancel()
				continue
			}

			// Non-streaming mode (legacy)
			result, err := processor.Chat(apiCtx, req.ImageData, req.UserMessage)
			cancel()
			elapsedMs := time.Since(startTime).Milliseconds()

			var chatResult ChatResult
			if err != nil {
				log.Printf("❌ Worker %d error on SeqNum %d: %v (took %dms)", id, req.SeqNum, err, elapsedMs)
				chatResult = ChatResult{
					SeqNum:  req.SeqNum,
					Text:    "죄송합니다, 처리 중 오류가 발생했습니다. 다시 시도해주세요.",
					IsError: true,
				}
			} else {
				log.Printf("✅ Worker %d completed SeqNum %d (took %dms)", id, req.SeqNum, elapsedMs)
				chatResult = ChatResult{
					SeqNum:  req.SeqNum,
					Text:    result,
					IsError: false,
				}
			}

			select {
			case b.resultChan <- chatResult:
			case <-b.ctx.Done():
				return
			}
		}
	}
}

// processStreaming handles streaming responses - sends chunks directly to frontend
func (b *Bot) processStreaming(workerID int, req ChatRequest, processor StreamingVisionProcessor, ctx context.Context) {
	chunks := make(chan string, 10)
	startTime := time.Now()
	firstChunk := true
	var ttftMs int64

	// Start streaming in background
	go func() {
		defer close(chunks)
		if err := processor.ChatStream(ctx, req.ImageData, req.UserMessage, chunks); err != nil {
			log.Printf("❌ Worker %d streaming error on SeqNum %d: %v", workerID, req.SeqNum, err)
		}
	}()

	// Send each chunk to frontend immediately
	for chunk := range chunks {
		if firstChunk {
			ttftMs = time.Since(startTime).Milliseconds()
			log.Printf("⚡ Worker %d TTFT SeqNum %d: %dms (first token)", workerID, req.SeqNum, ttftMs)
			firstChunk = false
		}
		b.sendStreamChunk(req.SeqNum, chunk, false)
	}

	// Send final marker
	b.sendStreamChunk(req.SeqNum, "", true)

	elapsedMs := time.Since(startTime).Milliseconds()
	log.Printf("✅ Worker %d streaming completed SeqNum %d (TTFT: %dms, Total: %dms)", workerID, req.SeqNum, ttftMs, elapsedMs)
}

// sendStreamChunk sends a single chunk to frontend
func (b *Bot) sendStreamChunk(seqNum uint64, text string, isFinal bool) {
	msg := DataChannelMessage{
		Type:    "chat_stream",
		SeqNum:  seqNum,
		Text:    text,
		IsFinal: isFinal,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("❌ Failed to marshal chunk: %v", err)
		return
	}

	err = b.room.LocalParticipant.PublishDataPacket(
		lksdk.UserData(data),
		lksdk.WithDataPublishReliable(true),
	)
	if err != nil {
		log.Printf("❌ Failed to send chunk: %v", err)
	}
}

// reorderAndSend collects results, reorders them by SeqNum, and sends in order
func (b *Bot) reorderAndSend() {
	log.Println("🔄 Reorder buffer started")
	for {
		select {
		case <-b.ctx.Done():
			log.Println("🛑 reorderAndSend goroutine stopped")
			return
		case result, ok := <-b.resultChan:
			if !ok {
				log.Println("🛑 resultChan closed, reorderAndSend exiting")
				return
			}

			b.reorderMu.Lock()

			// Add result to heap
			heap.Push(b.reorderBuffer, result)
			log.Printf("🔄 Buffered SeqNum %d (buffer size: %d, next expected: %d)",
				result.SeqNum, b.reorderBuffer.Len(), b.nextExpected)

			// Send all consecutive results starting from nextExpected
			for b.reorderBuffer.Len() > 0 && (*b.reorderBuffer)[0].SeqNum == b.nextExpected {
				toSend := heap.Pop(b.reorderBuffer).(ChatResult)
				b.reorderMu.Unlock()

				b.sendToFrontend(toSend)
				b.nextExpected++

				b.reorderMu.Lock()
			}

			b.reorderMu.Unlock()
		}
	}
}

// sendToFrontend sends a single result to the frontend
func (b *Bot) sendToFrontend(result ChatResult) {
	msg := DataChannelMessage{
		Type: "chat",
		Text: result.Text,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("❌ Failed to marshal result: %v", err)
		return
	}

	// Send via LiveKit DataChannel
	err = b.room.LocalParticipant.PublishDataPacket(
		lksdk.UserData(data),
		lksdk.WithDataPublishReliable(true),
	)
	if err != nil {
		log.Printf("❌ Failed to send result: %v", err)
		return
	}

	if result.IsError {
		log.Printf("📤 Error response sent for SeqNum %d", result.SeqNum)
	} else {
		log.Printf("📤 Result sent for SeqNum %d", result.SeqNum)
	}
}
