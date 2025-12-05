'use client'

import { useEffect, useRef, useState, useCallback } from 'react'
import {
  LiveKitRoom,
  VideoConference,
  useRoomContext,
  useConnectionState,
} from '@livekit/components-react'
import { RoomEvent, ConnectionState } from 'livekit-client'
import '@livekit/components-styles'

// Constants
const CAPTURE_WIDTH = 320
const CAPTURE_HEIGHT = 240
const JPEG_QUALITY = 0.6

interface ChatMessage {
  role: 'user' | 'ai'
  content: string
  timestamp: Date
}

interface SubtitleData {
  status: string  // Connection status
}

interface TokenResponse {
  token: string
  identity: string
  room: string
}

interface RoomContentProps {
  onSubtitleUpdate: React.Dispatch<React.SetStateAction<SubtitleData>>
  onAddMessage: (message: ChatMessage) => void
  sendChatRef: React.MutableRefObject<((msg: string) => void) | null>
}

// Inner component: operates within Room context
function RoomContent({ onSubtitleUpdate, onAddMessage, sendChatRef }: RoomContentProps) {
  const room = useRoomContext()
  const connectionState = useConnectionState()
  const videoElementRef = useRef<HTMLVideoElement | null>(null)
  const canvasRef = useRef<HTMLCanvasElement | null>(null)

  // Find video source (screen share takes priority)
  const findVideoSource = useCallback((): HTMLVideoElement | null => {
    // 1. Screen share video first
    const screenShare = document.querySelector('video[data-lk-source="screen_share"]') as HTMLVideoElement
    if (screenShare && screenShare.srcObject && screenShare.readyState >= 2) {
      return screenShare
    }

    // 2. Local camera video
    const localVideo = document.querySelector('video[data-lk-local-participant="true"]') as HTMLVideoElement
      || Array.from(document.querySelectorAll('video')).find(v => {
        const el = v as HTMLVideoElement
        return el.srcObject && el.muted
      }) as HTMLVideoElement

    return localVideo || null
  }, [])

  // Capture image function
  const captureImage = useCallback((): string | null => {
    // Find latest video source each time
    const video = findVideoSource()
    if (!video) return null

    videoElementRef.current = video
    if (video.readyState < 3) return null

    try {
      if (!canvasRef.current) {
        canvasRef.current = document.createElement('canvas')
        canvasRef.current.width = CAPTURE_WIDTH
        canvasRef.current.height = CAPTURE_HEIGHT
      }

      const ctx = canvasRef.current.getContext('2d')
      if (!ctx) return null

      ctx.drawImage(video, 0, 0, CAPTURE_WIDTH, CAPTURE_HEIGHT)
      return canvasRef.current.toDataURL('image/jpeg', JPEG_QUALITY)
    } catch (error) {
      console.error('Error capturing video frame:', error)
      return null
    }
  }, [findVideoSource])

  // Send chat (current image + message)
  const sendChat = useCallback((userMessage: string) => {
    if (!room) return

    const dataUrl = captureImage()
    if (!dataUrl) {
      console.error('No image available for chat')
      return
    }

    const encoder = new TextEncoder()
    const message = JSON.stringify({
      type: 'chat',
      data: dataUrl,
      message: userMessage
    })
    const data = encoder.encode(message)

    room.localParticipant.publishData(data, { reliable: true })
    console.log('💬 Chat sent:', userMessage)
  }, [room, captureImage])

  // Connect sendChat to parent via ref
  useEffect(() => {
    sendChatRef.current = sendChat
  }, [sendChat, sendChatRef])

  // DataChannel receive handler
  const handleDataReceived = useCallback((payload: Uint8Array) => {
    try {
      const decoder = new TextDecoder()
      const message = decoder.decode(payload)
      const data = JSON.parse(message)

      console.log('📥 Data received:', data)

      if (data.type === 'chat' && data.text) {
        onAddMessage({
          role: 'ai',
          content: data.text,
          timestamp: new Date()
        })
      }
    } catch (error) {
      console.error('Error parsing data channel message:', error)
    }
  }, [onAddMessage])

  // Register DataChannel listener on room connection
  useEffect(() => {
    if (connectionState !== ConnectionState.Connected || !room) {
      return
    }

    console.log('✅ Connected to room:', room.name)
    onSubtitleUpdate(prev => ({ ...prev, status: 'Connected - Ask a question' }))

    room.on(RoomEvent.DataReceived, handleDataReceived)

    return () => {
      room.off(RoomEvent.DataReceived, handleDataReceived)
    }
  }, [connectionState, room, handleDataReceived, onSubtitleUpdate])

  return <VideoConference />
}

export default function VideoRoom() {
  const [subtitle, setSubtitle] = useState<SubtitleData>({ status: 'Connecting...' })
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [token, setToken] = useState<string | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [chatInput, setChatInput] = useState('')
  const sendChatRef = useRef<((msg: string) => void) | null>(null)
  const chatContainerRef = useRef<HTMLDivElement | null>(null)

  const liveKitUrl = process.env.NEXT_PUBLIC_LIVEKIT_URL
  const tokenApiUrl = process.env.NEXT_PUBLIC_TOKEN_API_URL || 'http://localhost:8080/api/token'

  // Add message to chat history
  const addMessage = useCallback((message: ChatMessage) => {
    setMessages(prev => [...prev, message])
  }, [])

  // Auto-scroll to bottom when new messages arrive
  useEffect(() => {
    if (chatContainerRef.current) {
      chatContainerRef.current.scrollTop = chatContainerRef.current.scrollHeight
    }
  }, [messages])

  // Chat send handler
  const handleSendChat = useCallback(() => {
    if (!chatInput.trim()) return
    if (!sendChatRef.current) return

    // Add user message to history
    addMessage({
      role: 'user',
      content: chatInput,
      timestamp: new Date()
    })
    sendChatRef.current(chatInput)
    setChatInput('')
  }, [chatInput, addMessage])

  // Enter key handler (with IME composition check for Korean/Japanese/Chinese input)
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      handleSendChat()
    }
  }

  // Fetch token
  useEffect(() => {
    const fetchToken = async () => {
      if (!liveKitUrl) {
        setError('NEXT_PUBLIC_LIVEKIT_URL is not configured.')
        setIsLoading(false)
        return
      }

      try {
        const response = await fetch(tokenApiUrl)
        if (!response.ok) {
          throw new Error(`Token fetch failed: ${response.status}`)
        }
        const data: TokenResponse = await response.json()
        console.log('🎫 Token received for:', data.identity)
        setToken(data.token)
      } catch (err) {
        console.error('Failed to fetch token:', err)
        setError(`Failed to fetch token. Make sure the backend server is running.\n\n${err}`)
      } finally {
        setIsLoading(false)
      }
    }

    fetchToken()
  }, [liveKitUrl, tokenApiUrl])

  // Loading state
  if (isLoading) {
    return (
      <div className="w-full max-w-4xl p-8 bg-gray-800 rounded-lg text-center">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-white mx-auto mb-4"></div>
        <p className="text-white">Fetching token from server...</p>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className="w-full max-w-4xl p-8 bg-red-900/50 rounded-lg border border-red-500">
        <h2 className="text-xl font-bold text-red-400 mb-2">Connection Error</h2>
        <p className="text-white whitespace-pre-wrap">{error}</p>
        <pre className="mt-4 p-4 bg-black/50 rounded text-sm text-gray-300">
{`# Run backend server:
cd backend
go run cmd/bot/main.go`}
        </pre>
        <button
          onClick={() => window.location.reload()}
          className="mt-4 px-4 py-2 bg-red-600 text-white rounded hover:bg-red-700"
        >
          Retry
        </button>
      </div>
    )
  }

  // No token state
  if (!token) {
    return (
      <div className="w-full max-w-4xl p-8 bg-yellow-900/50 rounded-lg border border-yellow-500">
        <h2 className="text-xl font-bold text-yellow-400 mb-2">Token Error</h2>
        <p className="text-white">Failed to receive token.</p>
      </div>
    )
  }

  return (
    <div className="w-full max-w-4xl">
      <LiveKitRoom
        video={true}
        audio={true}
        token={token}
        serverUrl={liveKitUrl || ''}
        data-lk-theme="default"
        className="rounded-lg overflow-hidden"
      >
        <RoomContent
          onSubtitleUpdate={setSubtitle}
          onAddMessage={addMessage}
          sendChatRef={sendChatRef}
        />
      </LiveKitRoom>

      {/* Status */}
      <div className="mt-4 flex items-center gap-2">
        <div className={`w-2 h-2 rounded-full ${subtitle.status.includes('Connected') ? 'bg-green-500' : 'bg-yellow-500'}`} />
        <span className="text-sm text-gray-400">{subtitle.status}</span>
      </div>

      {/* Chat History */}
      <div
        ref={chatContainerRef}
        className="mt-4 p-4 bg-gray-800 rounded-lg h-64 overflow-y-auto"
      >
        {messages.length === 0 ? (
          <p className="text-gray-500 text-center">No messages yet. Ask a question!</p>
        ) : (
          <div className="space-y-3">
            {messages.map((msg, idx) => (
              <div
                key={idx}
                className={`flex ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}
              >
                <div
                  className={`max-w-[80%] px-4 py-2 rounded-lg ${
                    msg.role === 'user'
                      ? 'bg-blue-600 text-white'
                      : 'bg-gray-700 text-white'
                  }`}
                >
                  <div className="text-xs opacity-70 mb-1">
                    {msg.role === 'user' ? '👤 You' : '🤖 AI'}
                  </div>
                  <div>{msg.content}</div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Chat Input */}
      <div className="mt-4 flex gap-2">
        <input
          type="text"
          value={chatInput}
          onChange={(e) => setChatInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Ask AI a question... (e.g., What is that?)"
          className="flex-1 px-4 py-3 bg-gray-800 border border-gray-600 rounded-lg text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
        />
        <button
          onClick={handleSendChat}
          disabled={!chatInput.trim()}
          className="px-6 py-3 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          Send
        </button>
      </div>
    </div>
  )
}
