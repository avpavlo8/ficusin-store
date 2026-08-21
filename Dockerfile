FROM node:24-bookworm-slim AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
COPY public/ /src/public/
RUN npm run build

FROM golang:1.26-bookworm AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ficusin-api ./cmd/api

# Timeweb build workers have intermittently failed while reaching Debian apt
# mirrors. The runtime image therefore performs no package-manager/network
# operations at build time. minidocks/poppler already contains pdftotext,
# BusyBox wget and CA certificates, which are the runtime tools we need.
FROM minidocks/poppler:latest
WORKDIR /app
COPY --from=backend /out/ficusin-api /app/ficusin-api
COPY --from=frontend /src/frontend/dist /app/web
COPY timeweb/migrations /app/migrations
ENV PORT=3000
ENV STATIC_DIR=/app/web
ENV MIGRATIONS_DIR=/app/migrations
EXPOSE 3000
HEALTHCHECK --interval=5s --timeout=3s --start-period=30s --retries=5 CMD wget -qO- http://127.0.0.1:3000/api/v1/health >/dev/null || exit 1
USER 65532:65532
ENTRYPOINT ["/app/ficusin-api"]
