# LLM-EYES 👁️

Real-time AI Vision Assistant powered by WebRTC and Gemini

> **Incheon DevFest 2025** - "WebRTC in the AI Era" Demo Project

## What is this?

LLM-EYES is a demo application that shows how to integrate AI vision capabilities with WebRTC. An AI bot joins your video call, watches what you're showing (camera or screen share), and answers questions about what it sees.

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Browser   │────▶│   LiveKit   │◀────│   Go Bot    │
│  (WebRTC)   │     │   Cloud     │     │  (Gemini)   │
└─────────────┘     └─────────────┘     └─────────────┘
      │                                        │
      │◀──────── "That's a coffee mug" ────────│
```

## Features

- 📹 **Camera & Screen Share** - AI analyzes whatever you show
- 💬 **Chat Interface** - Ask questions about what AI sees
- ⚡ **Real-time** - Powered by LiveKit WebRTC
- 🤖 **Gemini Vision** - Google's multimodal AI

## Tech Stack

| Layer | Technology |
|-------|------------|
| Frontend | Next.js 14, LiveKit Components |
| Backend | Go, LiveKit Server SDK |
| AI | Google Gemini 2.0 Flash |
| WebRTC | LiveKit Cloud |

## Quick Start

### Prerequisites

- Node.js 20+
- Go 1.22+
- [LiveKit Cloud](https://cloud.livekit.io/) account (free tier available)
- [Google AI Studio](https://aistudio.google.com/) API key

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
GEMINI_API_KEY=your-gemini-key
```

### Frontend (`frontend/.env.local`)

```env
NEXT_PUBLIC_LIVEKIT_URL=wss://your-project.livekit.cloud
NEXT_PUBLIC_TOKEN_API_URL=http://localhost:8080/api/token
```

## Architecture

```
Frontend (Next.js)
    │
    ├── Captures video frame (320x240 JPEG)
    ├── Sends via DataChannel with user question
    │
    ▼
LiveKit Cloud (SFU)
    │
    ├── Routes DataChannel messages
    │
    ▼
Backend (Go Bot)
    │
    ├── Receives image + question
    ├── Calls Gemini Vision API
    ├── Returns AI response via DataChannel
    │
    ▼
Frontend displays response
```

## Project Structure

```
llm-eyes/
├── frontend/           # Next.js app
│   ├── app/           # App router pages
│   └── components/    # React components
│       └── VideoRoom.tsx  # Main video room with chat
│
├── backend/           # Go application
│   ├── cmd/bot/       # Entry point
│   └── internal/
│       ├── bot/       # LiveKit bot logic
│       └── vision/    # Gemini API integration
│
└── docker-compose.yml # Container setup
```

## License

MIT

## Acknowledgments

- [LiveKit](https://livekit.io/) - WebRTC infrastructure
- [Google Gemini](https://ai.google.dev/) - Vision AI
- Incheon DevFest 2025 organizers
