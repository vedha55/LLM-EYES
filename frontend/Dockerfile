# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

# Dependencies
COPY package.json pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile

# Build args for environment variables
ARG NEXT_PUBLIC_LIVEKIT_URL
ARG NEXT_PUBLIC_TOKEN_API_URL

ENV NEXT_PUBLIC_LIVEKIT_URL=$NEXT_PUBLIC_LIVEKIT_URL
ENV NEXT_PUBLIC_TOKEN_API_URL=$NEXT_PUBLIC_TOKEN_API_URL

# Build
COPY . .
RUN pnpm build

# Runtime stage
FROM node:20-alpine

WORKDIR /app

COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public

EXPOSE 3000

ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

CMD ["node", "server.js"]
