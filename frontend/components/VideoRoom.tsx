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
  isStreaming?: boolean  // true while AI is still typing
}

interface SubtitleData {
  status: string  // Connection status
}

interface TokenResponse {
  token: string
  identity: string
  room: string
}

interface ModelInfo {
  id: string
  name: string
  provider: string
  description: string
}

interface ModelsResponse {
  models: ModelInfo[]
  current: string
}

interface RoomContentProps {
  onSubtitleUpdate: React.Dispatch<React.SetStateAction<SubtitleData>>
  setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>
  sendChatRef: React.MutableRefObject<((msg: string) => void) | null>
  currentModel: string
  apiBaseUrl: string
}

// Inner component: operates within Room context
function RoomContent({ onSubtitleUpdate, setMessages, sendChatRef, currentModel, apiBaseUrl }: RoomContentProps) {
  const room = useRoomContext()
  const connectionState = useConnectionState()
  const videoElementRef = useRef<HTMLVideoElement | null>(null)
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const frameIntervalRef = useRef<NodeJS.Timeout | null>(null)

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

  // Track streaming messages by seqNum
  const streamingMessagesRef = useRef<Map<number, string>>(new Map())

  // DataChannel receive handler
  const handleDataReceived = useCallback((payload: Uint8Array) => {
    try {
      const decoder = new TextDecoder()
      const message = decoder.decode(payload)
      const data = JSON.parse(message)

      console.log('📥 Data received:', data)

      // Handle streaming chunks
      if (data.type === 'chat_stream') {
        const seqNum = data.seqNum || 0

        if (data.isFinal) {
          // Streaming complete - mark message as not streaming
          streamingMessagesRef.current.delete(seqNum)
          setMessages(prev => {
            // Find and update the streaming message to mark as complete
            return prev.map((msg, idx) =>
              msg.isStreaming && idx === prev.length - 1
                ? { ...msg, isStreaming: false }
                : msg
            )
          })
        } else if (data.text) {
          // Append chunk to existing message or create new one
          const existingText = streamingMessagesRef.current.get(seqNum) || ''
          const newText = existingText + data.text
          streamingMessagesRef.current.set(seqNum, newText)

          setMessages(prev => {
            // Check if last message is streaming AI message
            const lastMsg = prev[prev.length - 1]
            if (lastMsg?.role === 'ai' && lastMsg?.isStreaming) {
              // Update existing streaming message
              return [
                ...prev.slice(0, -1),
                { ...lastMsg, content: newText }
              ]
            } else {
              // Create new streaming message
              return [
                ...prev,
                {
                  role: 'ai' as const,
                  content: newText,
                  timestamp: new Date(),
                  isStreaming: true
                }
              ]
            }
          })
        }
        return
      }

      // Handle legacy non-streaming response
      if (data.type === 'chat' && data.text) {
        setMessages(prev => [
          ...prev,
          {
            role: 'ai' as const,
            content: data.text,
            timestamp: new Date()
          }
        ])
      }
    } catch (error) {
      console.error('Error parsing data channel message:', error)
    }
  }, [setMessages])

  // Send frame to backend for streaming mode
  const sendFrame = useCallback(async () => {
    const dataUrl = captureImage()
    if (!dataUrl) return

    try {
      await fetch(`${apiBaseUrl}/api/frame`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ frame: dataUrl })
      })
    } catch (err) {
      // Silently ignore frame errors
    }
  }, [captureImage, apiBaseUrl])

  // Start/stop frame streaming based on model
  useEffect(() => {
    const isStreamingModel = currentModel === 'gemini-live-streaming'

    if (isStreamingModel && connectionState === ConnectionState.Connected) {
      console.log('🎬 Starting 1 FPS frame streaming...')
      // Send frames at 1 FPS
      frameIntervalRef.current = setInterval(sendFrame, 1000)
      onSubtitleUpdate(prev => ({ ...prev, status: 'Connected - Streaming 1 FPS' }))
    } else {
      if (frameIntervalRef.current) {
        console.log('🛑 Stopping frame streaming')
        clearInterval(frameIntervalRef.current)
        frameIntervalRef.current = null
      }
    }

    return () => {
      if (frameIntervalRef.current) {
        clearInterval(frameIntervalRef.current)
        frameIntervalRef.current = null
      }
    }
  }, [currentModel, connectionState, sendFrame, onSubtitleUpdate])

  // Register DataChannel listener on room connection
  useEffect(() => {
    if (connectionState !== ConnectionState.Connected || !room) {
      return
    }

    console.log('✅ Connected to room:', room.name)
    const isStreamingModel = currentModel === 'gemini-live-streaming'
    onSubtitleUpdate(prev => ({
      ...prev,
      status: isStreamingModel ? 'Connected - Streaming 1 FPS' : 'Connected - Ask a question'
    }))

    room.on(RoomEvent.DataReceived, handleDataReceived)

    return () => {
      room.off(RoomEvent.DataReceived, handleDataReceived)
    }
  }, [connectionState, room, handleDataReceived, onSubtitleUpdate, currentModel])

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

  // Model state
  const [models, setModels] = useState<ModelInfo[]>([])
  const [currentModel, setCurrentModel] = useState<string>('')
  const [isSwitchingModel, setIsSwitchingModel] = useState(false)

  const liveKitUrl = process.env.NEXT_PUBLIC_LIVEKIT_URL
  const tokenApiUrl = process.env.NEXT_PUBLIC_TOKEN_API_URL || 'http://localhost:8080/api/token'
  const apiBaseUrl = process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:8080'

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

  // Fetch models
  const fetchModels = useCallback(async () => {
    try {
      const response = await fetch(`${apiBaseUrl}/api/models`)
      if (response.ok) {
        const data: ModelsResponse = await response.json()
        setModels(data.models || [])
        setCurrentModel(data.current || '')
      }
    } catch (err) {
      console.error('Failed to fetch models:', err)
    }
  }, [apiBaseUrl])

  // Switch model
  const switchModel = useCallback(async (modelId: string) => {
    if (modelId === currentModel || isSwitchingModel) return

    setIsSwitchingModel(true)
    try {
      const response = await fetch(`${apiBaseUrl}/api/models/switch`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model_id: modelId })
      })

      if (response.ok) {
        setCurrentModel(modelId)
        setMessages([]) // Clear chat history on model switch
        console.log('🔄 Model switched to:', modelId)
      } else {
        const errorText = await response.text()
        console.error('Failed to switch model:', errorText)
      }
    } catch (err) {
      console.error('Failed to switch model:', err)
    } finally {
      setIsSwitchingModel(false)
    }
  }, [apiBaseUrl, currentModel, isSwitchingModel])

  // Fetch token and models
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

        // Fetch available models
        await fetchModels()
      } catch (err) {
        console.error('Failed to fetch token:', err)
        setError(`Failed to fetch token. Make sure the backend server is running.\n\n${err}`)
      } finally {
        setIsLoading(false)
      }
    }

    fetchToken()
  }, [liveKitUrl, tokenApiUrl, fetchModels])

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
          setMessages={setMessages}
          sendChatRef={sendChatRef}
          currentModel={currentModel}
          apiBaseUrl={apiBaseUrl}
        />
      </LiveKitRoom>

      {/* Status & Model Selector */}
      <div className="mt-4 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <div className={`w-2 h-2 rounded-full ${subtitle.status.includes('Connected') ? 'bg-green-500' : 'bg-yellow-500'}`} />
          <span className="text-sm text-gray-400">{subtitle.status}</span>
        </div>

        {/* Model Selector */}
        {models.length > 0 && (
          <div className="flex items-center gap-2">
            <span className="text-sm text-gray-400">Model:</span>
            <select
              value={currentModel}
              onChange={(e) => switchModel(e.target.value)}
              disabled={isSwitchingModel}
              className="px-3 py-1.5 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:border-blue-500 disabled:opacity-50"
            >
              {models.map((model) => (
                <option key={model.id} value={model.id}>
                  {model.name} ({model.provider})
                </option>
              ))}
            </select>
            {isSwitchingModel && (
              <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-white"></div>
            )}
          </div>
        )}
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
                    {msg.isStreaming && <span className="ml-2 text-green-400">typing...</span>}
                  </div>
                  <div>
                    {msg.content}
                    {msg.isStreaming && <span className="inline-block w-2 h-4 ml-1 bg-green-400 animate-pulse" />}
                  </div>
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
