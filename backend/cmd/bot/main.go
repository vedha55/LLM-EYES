package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"llm-eyes/internal/bot"
	"llm-eyes/internal/vision"

	"github.com/joho/godotenv"
	"github.com/livekit/protocol/auth"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found")
	}

	// Check required environment variables
	liveKitURL := os.Getenv("LIVEKIT_URL")
	apiKey := os.Getenv("LIVEKIT_API_KEY")
	apiSecret := os.Getenv("LIVEKIT_API_SECRET")
	roomName := os.Getenv("LIVEKIT_ROOM_NAME")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	if liveKitURL == "" || apiKey == "" || apiSecret == "" || roomName == "" {
		log.Fatal("Missing required environment variables. Check .env file.")
	}

	log.Println("🤖 Starting LLM-EYES Vision Bot...")

	// Start token API server in separate goroutine
	go startTokenServer(apiKey, apiSecret, roomName)

	// If no Gemini API key, only run token server
	if geminiKey == "" {
		log.Println("⚠️  GEMINI_API_KEY not set - Vision Bot will not analyze images")
		log.Println("🚀 Token server is running on http://localhost:8080")

		// Wait for graceful shutdown
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("🛑 Shutting down...")
		return
	}

	// Initialize Gemini Vision processor
	ctx := context.Background()
	visionProcessor, err := vision.NewGeminiProcessor(ctx, geminiKey)
	if err != nil {
		log.Fatalf("Failed to initialize Gemini: %v", err)
	}
	defer visionProcessor.Close()

	log.Println("✅ Gemini Vision processor initialized")

	// Use atomic.Pointer to prevent race condition
	var aiBot atomic.Pointer[bot.Bot]

	// Set up LiveKit Room callback
	roomCallback := &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnDataPacket: func(data lksdk.DataPacket, params lksdk.DataReceiveParams) {
				if b := aiBot.Load(); b != nil {
					b.HandleDataPacket(data, params)
				}
			},
		},
	}

	// Connect to LiveKit Room
	room, err := lksdk.ConnectToRoom(liveKitURL, lksdk.ConnectInfo{
		APIKey:              apiKey,
		APISecret:           apiSecret,
		RoomName:            roomName,
		ParticipantIdentity: "vision-bot",
		ParticipantName:     "AI Vision Bot",
	}, roomCallback)

	if err != nil {
		log.Fatalf("Failed to connect to room: %v", err)
	}
	defer room.Disconnect()

	log.Printf("✅ Connected to room: %s\n", roomName)

	// Initialize and start bot
	newBot := bot.NewBot(room, visionProcessor)
	aiBot.Store(newBot)
	newBot.Start()

	log.Println("🚀 Vision Bot is running...")

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("🛑 Shutting down bot...")

	if b := aiBot.Load(); b != nil {
		b.Stop()
	}
}

// startTokenServer starts the HTTP server for token generation
func startTokenServer(apiKey, apiSecret, roomName string) {
	mux := http.NewServeMux()

	// CORS middleware
	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			h(w, r)
		}
	}

	// Token generation endpoint
	mux.HandleFunc("/api/token", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		// Generate participant identity using timestamp
		identity := "user-" + time.Now().Format("150405")

		// Generate LiveKit token
		at := auth.NewAccessToken(apiKey, apiSecret)
		grant := &auth.VideoGrant{
			RoomJoin: true,
			Room:     roomName,
		}
		at.AddGrant(grant).
			SetIdentity(identity).
			SetValidFor(24 * time.Hour)

		token, err := at.ToJWT()
		if err != nil {
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			log.Printf("❌ Token generation failed: %v", err)
			return
		}

		log.Printf("🎫 Token generated for %s", identity)

		// JSON response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"token":    token,
			"identity": identity,
			"room":     roomName,
		})
	}))

	log.Println("🎫 Token server starting on http://localhost:8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("Failed to start token server: %v", err)
	}
}
