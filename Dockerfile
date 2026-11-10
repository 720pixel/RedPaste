# Build the frontend, embed it into the Go binary, ship a tiny static image.

# 1. frontend
FROM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm install
COPY web/ ./
RUN npm run build

# 2. server (embeds web/dist via go:embed)
FROM golang:1.25-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
# CGO off keeps it a static binary; modernc sqlite is pure Go so this is fine.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /redpaste-server ./cmd/redpaste-server
# Empty data dir, handed to the nonroot user below so a fresh named volume
# mounted at /data is writable.
RUN mkdir /data

# 3. runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /redpaste-server /redpaste-server
COPY --from=server --chown=65532:65532 /data /data
EXPOSE 8080
USER nonroot:nonroot
ENV REDPASTE_LISTEN_ADDR=:8080 \
    REDPASTE_SQLITE_PATH=/data/redpaste.db
VOLUME ["/data"]
ENTRYPOINT ["/redpaste-server"]
