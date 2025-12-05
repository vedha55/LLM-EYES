package vision

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// GeminiProcessor handles Gemini Vision API calls with ChatSession
type GeminiProcessor struct {
	client *genai.Client
	model  *genai.GenerativeModel
	chat   *genai.ChatSession
	mu     sync.Mutex // Prevent concurrent ChatSession access
}

// NewGeminiProcessor creates a new Gemini Vision processor
func NewGeminiProcessor(ctx context.Context, apiKey string) (*GeminiProcessor, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	model := client.GenerativeModel("gemini-2.0-flash")

	// Model settings
	model.SetTemperature(0.4)
	model.SetMaxOutputTokens(200)

	// System instruction
	model.SystemInstruction = genai.NewUserContent(genai.Text(
		// "You are an AI assistant watching a real-time video stream. " +
		// 	"When images come in, remember them as context. " +
		// 	"When the user asks a question, answer briefly based on what you've seen.",
		"너는 실시간 영상을 보고 있는 AI 어시스턴트야. " +
        "주기적으로 이미지가 들어오면 컨텍스트로 기억해. " +
        "유저가 질문하면 지금까지 본 이미지들의 맥락을 바탕으로 한국어로 익살스럽고 유머있게 대답해.",
	))

	// Start ChatSession for context retention
	chat := model.StartChat()

	return &GeminiProcessor{
		client: client,
		model:  model,
		chat:   chat,
	}, nil
}

// AddContext adds an image to the chat context without expecting a response
func (g *GeminiProcessor) AddContext(ctx context.Context, dataURL string) error {
	imageBytes, err := g.decodeDataURL(dataURL)
	if err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Add image to context
	_, err = g.chat.SendMessage(ctx,
		genai.ImageData("jpeg", imageBytes),
		genai.Text("(observing image)"),
	)
	if err != nil {
		return fmt.Errorf("failed to add context: %w", err)
	}

	log.Println("📸 Context image added to session")
	return nil
}

// Chat handles user chat with current image context
func (g *GeminiProcessor) Chat(ctx context.Context, dataURL string, userMessage string) (string, error) {
	imageBytes, err := g.decodeDataURL(dataURL)
	if err != nil {
		return "", err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Current image + user question
	resp, err := g.chat.SendMessage(ctx,
		genai.ImageData("jpeg", imageBytes),
		genai.Text(userMessage),
	)
	if err != nil {
		return "", fmt.Errorf("failed to chat: %w", err)
	}

	return g.extractResponse(resp)
}

// AnalyzeImage analyzes a base64-encoded image and returns description (legacy)
func (g *GeminiProcessor) AnalyzeImage(ctx context.Context, dataURL string) (string, error) {
	imageBytes, err := g.decodeDataURL(dataURL)
	if err != nil {
		return "", err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	resp, err := g.chat.SendMessage(ctx,
		genai.ImageData("jpeg", imageBytes),
		genai.Text("What do you see on the screen? Describe briefly."),
	)
	if err != nil {
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	return g.extractResponse(resp)
}

// decodeDataURL parses and decodes a data URL
func (g *GeminiProcessor) decodeDataURL(dataURL string) ([]byte, error) {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid data URL format")
	}

	imageBytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	return imageBytes, nil
}

// extractResponse extracts text from Gemini response
func (g *GeminiProcessor) extractResponse(resp *genai.GenerateContentResponse) (string, error) {
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no response from Gemini")
	}

	result := fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0])
	return result, nil
}

// Close closes the Gemini client
func (g *GeminiProcessor) Close() error {
	return g.client.Close()
}
