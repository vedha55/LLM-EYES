# LLM-EYES Frontend

Real-time AI vision and speech-to-text frontend built with Next.js 14 and LiveKit.

## Features

- Real-time video streaming using LiveKit
- Vision AI analysis (sends video frames every 1 second)
- Speech-to-text transcription display
- Live subtitle display for both vision and STT results

## Setup

1. Install dependencies:
```bash
npm install
```

2. Create `.env.local` file (copy from `.env.local.example`):
```bash
cp .env.local.example .env.local
```

3. Configure LiveKit credentials in `.env.local`:
```
NEXT_PUBLIC_LIVEKIT_URL=wss://your-project.livekit.cloud
NEXT_PUBLIC_LIVEKIT_TOKEN=your-token-here
```

4. Run development server:
```bash
npm run dev
```

Open [http://localhost:3000](http://localhost:3000) with your browser.

## Architecture

### Data Channel Protocol

**Sending (Vision Request):**
```json
{
  "type": "vision",
  "data": "data:image/jpeg;base64,..."
}
```

**Receiving (AI Response):**
```json
{
  "type": "vision",
  "text": "I see a person wearing..."
}
```
```json
{
  "type": "stt",
  "text": "안녕하세요"
}
```

### Video Capture

- Canvas size: 320x240
- JPEG quality: 0.6
- Capture interval: 1000ms (1 second)

### Components

- `app/page.tsx` - Main page with title
- `components/VideoRoom.tsx` - LiveKit room with video conference and data channel handling
- `app/globals.css` - Subtitle container styles

## Tech Stack

- Next.js 14 (App Router)
- TypeScript
- Tailwind CSS
- LiveKit React Components
- LiveKit Client SDK
