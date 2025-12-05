# LLM-EYES 👁️

Real-time AI Vision Assistant powered by WebRTC and Multi-Model Support

> **Incheon DevFest 2025** - "WebRTC in the AI Era" Demo Project

## What is this?

LLM-EYES is a demo application showcasing how to build real-time multimodal AI pipelines with WebRTC. An AI bot joins your video call, watches what you're showing (camera or screen share), and answers questions about what it sees.

The project demonstrates key concepts from the presentation:
- **Parallel Workers** with Go's goroutines and channels
- **Reorder Buffer** using Min-Heap for deterministic output ordering
- **Multi-Model Support** comparing different AI providers and architectures

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Browser   │────▶│   LiveKit   │◀────│   Go Bot    │
│  (WebRTC)   │     │   Cloud     │     │  (AI Model) │
└─────────────┘     └─────────────┘     └─────────────┘
      │                                        │
      │◀──────── "That's a coffee mug" ────────│
```

## Features

- 📹 **Camera & Screen Share** - AI analyzes whatever you show
- 💬 **Chat Interface** - Ask questions about what AI sees
- ⚡ **Real-time** - Powered by LiveKit WebRTC
- 🔄 **Multi-Model** - Switch between Groq, Gemini Flash, Gemini Live Streaming
- 🎯 **Parallel Processing** - Worker pool with reorder buffer for consistent results

## Model Performance Comparison

Real benchmark results from the demo:

| Model | Avg Latency | TTFT | Speed | Protocol | Context |
|-------|-------------|------|-------|----------|---------|
| Groq Llama4 Scout | 411ms | - | ⚡⚡⚡⚡⚡ | REST | ✅ Session |
| Gemini 2.0 Flash | 1,747ms | - | ⚡⚡⚡ | REST | ✅ Session |
| Gemini Live Streaming | 2,055ms | 1.6s | ⚡⚡⚡⚡ | WebSocket | ✅ Real-time |
| Gemini Live | 2,718ms | - | ⚡⚡ | WebSocket | ✅ Session |
| Gemini 2.5 Flash Lite | 2,810ms | - | ⚡ | REST | ✅ Session |

> **Key Finding**: "Lite" ≠ "Fast" - Gemini 2.5 Flash Lite is cost-optimized, not speed-optimized!

### Architecture Comparison

**REST + ChatSession (Groq, Gemini Flash)**
- Frame sent only at chat time
- Session context retained (remembers previous conversations)
- Full response returned at once

**Gemini Live Streaming (Native WebSocket)**
- Continuous 1 FPS frame streaming → AI "keeps watching"
- Real-time context (remembers past frames)
- Streaming response with typing effect
- Can answer "What just happened?" questions

### Use Case Recommendations

| Use Case | Recommended Model | Reason |
|----------|-------------------|--------|
| 🚀 Maximum Speed | Groq Llama4 Scout | 411ms, fastest |
| 🎯 Reliability + Quality | Gemini 2.0 Flash | Google infrastructure, battle-tested |
| 💬 Natural Conversation | Gemini Live Streaming | Typing effect + real-time context |
| 💰 Cost Optimization | Gemini 2.5 Flash Lite | Cheapest per token (but slowest) |

## Tech Stack

| Layer | Technology |
|-------|------------|
| Frontend | Next.js 14, LiveKit Components, TypeScript |
| Backend | Go 1.22+, LiveKit Server SDK |
| AI Models | Groq (Llama4), Google Gemini (Flash, Live) |
| WebRTC | LiveKit Cloud |
| Concurrency | Goroutines, Channels, Min-Heap Reorder Buffer |

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Async Parallel Pipeline                  │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   DataChannel Input                                         │
│        │                                                    │
│        ▼                                                    │
│   ┌─────────┐   ┌─────────┐   ┌─────────┐                  │
│   │ Req #1  │   │ Req #2  │   │ Req #3  │  (with SeqNum)   │
│   └────┬────┘   └────┬────┘   └────┬────┘                  │
│        │             │             │                        │
│        ▼             ▼             ▼                        │
│   ┌─────────────────────────────────────┐                  │
│   │         Worker Pool (N=3)           │                  │
│   │   ┌─────┐   ┌─────┐   ┌─────┐      │                  │
│   │   │ W1  │   │ W2  │   │ W3  │      │  Parallel AI     │
│   │   └──┬──┘   └──┬──┘   └──┬──┘      │  API Calls       │
│   └──────┼─────────┼─────────┼─────────┘                  │
│          │         │         │                             │
│          ▼         ▼         ▼                             │
│   ┌─────────────────────────────────────┐                  │
│   │      Reorder Buffer (Min-Heap)      │                  │
│   │      [SeqNum: 1, 2, 3, ...]         │  Deterministic   │
│   └─────────────────┬───────────────────┘  Ordering        │
│                     │                                       │
│                     ▼                                       │
│              Ordered Output                                 │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- Node.js 20+
- Go 1.22+
- [LiveKit Cloud](https://cloud.livekit.io/) account (free tier available)
- API Key (at least one):
  - [Google AI Studio](https://aistudio.google.com/) for Gemini
  - [Groq Console](https://console.groq.com/) for Llama4 Scout

### 1. Clone & Setup

```bash
git clone https://github.com/Glitch-jar/llm-eyes.git
cd llm-eyes
```

### 2. Backend Setup

```bash
cd backend
cp .env.example .env
# Edit .env with your credentials
go mod download
go run cmd/bot/main.go
```

### 3. Frontend Setup

```bash
cd frontend
cp .env.example .env.local
# Edit .env.local with your credentials
pnpm install
pnpm dev
```

### 4. Open Browser

Navigate to `http://localhost:3000`, allow camera access, and start chatting!

## Environment Variables

### Backend (`backend/.env`)

```env
LIVEKIT_URL=wss://your-project.livekit.cloud
LIVEKIT_API_KEY=your-api-key
LIVEKIT_API_SECRET=your-api-secret
LIVEKIT_ROOM_NAME=llm-eyes-demo

# At least one AI provider required
GEMINI_API_KEY=your-gemini-key
GROQ_API_KEY=your-groq-key
```

### Frontend (`frontend/.env.local`)

```env
NEXT_PUBLIC_LIVEKIT_URL=wss://your-project.livekit.cloud
NEXT_PUBLIC_TOKEN_API_URL=http://localhost:8080/api/token
NEXT_PUBLIC_API_BASE_URL=http://localhost:8080
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/token` | GET | Generate LiveKit access token |
| `/api/models` | GET | List available AI models |
| `/api/models/switch` | POST | Switch active AI model |
| `/api/frame` | POST | Send frame for streaming mode |

## Project Structure

```
llm-eyes/
├── frontend/                # Next.js app
│   ├── app/                # App router pages
│   └── components/
│       └── VideoRoom.tsx   # Main video room with chat & model selector
│
├── backend/                # Go application
│   ├── cmd/bot/           # Entry point
│   │   └── main.go        # Server, token API, model management
│   └── internal/
│       ├── bot/           # LiveKit bot logic
│       │   └── bot.go     # Worker pool, reorder buffer
│       └── vision/        # AI model integrations
│           ├── gemini.go  # Gemini Flash/Live
│           └── groq.go    # Groq Llama4
│
└── docker-compose.yml     # Container setup
```

## Key Implementation Details

### Parallel Workers (bot.go)
```go
const NumWorkers = 3

func (b *Bot) Start() {
    for i := 0; i < NumWorkers; i++ {
        go b.worker(i)  // Each worker processes from shared channel
    }
    go b.reorderAndSend()
}
```

### Reorder Buffer (bot.go)
```go
// Min-Heap ensures results are sent in request order
type ResultHeap []ChatResult

func (b *Bot) reorderAndSend() {
    for result := range b.resultChan {
        heap.Push(b.reorderBuffer, result)

        // Send all consecutive results
        for (*b.reorderBuffer)[0].SeqNum == b.nextExpected {
            toSend := heap.Pop(b.reorderBuffer).(ChatResult)
            b.sendToFrontend(toSend)
            b.nextExpected++
        }
    }
}
```

### Error Handling
```go
if err != nil {
    chatResult = ChatResult{
        SeqNum:  req.SeqNum,
        Text:    "죄송합니다, 처리 중 오류가 발생했습니다. 다시 시도해주세요.",
        IsError: true,
    }
}
```

## License

MIT

## Acknowledgments

- [LiveKit](https://livekit.io/) - WebRTC infrastructure
- [Google Gemini](https://ai.google.dev/) - Multimodal AI
- [Groq](https://groq.com/) - Ultra-fast inference
- Incheon DevFest 2025 organizers
