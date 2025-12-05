package vision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
)

// GroqProcessor handles Groq Vision API calls
type GroqProcessor struct {
	apiKey     string
	model      string
	httpClient *http.Client
	messages   []groqMessage
	mu         sync.Mutex
}

type groqMessage struct {
	Role    string        `json:"role"`
	Content []groqContent `json:"content,omitempty"`
}

type groqContent struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *groqImageURL `json:"image_url,omitempty"`
}

type groqImageURL struct {
	URL string `json:"url"`
}

type groqRequest struct {
	Model       string        `json:"model"`
	Messages    []groqMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
}

type groqResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// NewGroqProcessor creates a new Groq Vision processor
func NewGroqProcessor(apiKey string) (*GroqProcessor, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("GROQ_API_KEY is required")
	}

	processor := &GroqProcessor{
		apiKey:     apiKey,
		model:      "meta-llama/llama-4-scout-17b-16e-instruct",
		httpClient: &http.Client{},
		messages:   make([]groqMessage, 0),
	}

	// Add system message
	processor.messages = append(processor.messages, groqMessage{
		Role: "system",
		Content: []groqContent{
			{
				Type: "text",
				Text: "너는 실시간 영상을 보고 있는 AI 어시스턴트야. " +
					"주기적으로 이미지가 들어오면 컨텍스트로 기억해. " +
					"유저가 질문하면 지금까지 본 이미지들의 맥락을 바탕으로 한국어로 익살스럽고 유머있게 대답해.",
			},
		},
	})

	return processor, nil
}

// AddContext adds an image to the chat context
func (g *GroqProcessor) AddContext(ctx context.Context, dataURL string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.messages = append(g.messages, groqMessage{
		Role: "user",
		Content: []groqContent{
			{Type: "text", Text: "(observing image)"},
			{Type: "image_url", ImageURL: &groqImageURL{URL: dataURL}},
		},
	})

	// Keep context manageable (max 10 context images)
	if len(g.messages) > 12 {
		// Keep system message + last 10 messages
		g.messages = append(g.messages[:1], g.messages[len(g.messages)-10:]...)
	}

	log.Println("📸 Context image added to Groq session")
	return nil
}

// Chat handles user chat with current image context
func (g *GroqProcessor) Chat(ctx context.Context, dataURL string, userMessage string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Add user message with image
	userMsg := groqMessage{
		Role: "user",
		Content: []groqContent{
			{Type: "image_url", ImageURL: &groqImageURL{URL: dataURL}},
			{Type: "text", Text: userMessage},
		},
	}

	messages := append(g.messages, userMsg)

	// Make API request
	reqBody := groqRequest{
		Model:       g.model,
		Messages:    messages,
		MaxTokens:   200,
		Temperature: 0.4,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var groqResp groqResponse
	if err := json.Unmarshal(body, &groqResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if groqResp.Error != nil {
		return "", fmt.Errorf("groq API error: %s", groqResp.Error.Message)
	}

	if len(groqResp.Choices) == 0 {
		return "", fmt.Errorf("no response from Groq")
	}

	result := groqResp.Choices[0].Message.Content

	// Add to context
	g.messages = append(g.messages, userMsg)
	g.messages = append(g.messages, groqMessage{
		Role: "assistant",
		Content: []groqContent{
			{Type: "text", Text: result},
		},
	})

	// Keep context manageable
	if len(g.messages) > 12 {
		g.messages = append(g.messages[:1], g.messages[len(g.messages)-10:]...)
	}

	return result, nil
}

// AnalyzeImage analyzes a base64-encoded image (legacy)
func (g *GroqProcessor) AnalyzeImage(ctx context.Context, dataURL string) (string, error) {
	return g.Chat(ctx, dataURL, "What do you see on the screen? Describe briefly.")
}

// Close closes the Groq processor
func (g *GroqProcessor) Close() error {
	return nil
}
