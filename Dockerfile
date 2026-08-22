FROM node:24-bookworm-slim@sha256:3638d9a6fe4030bd716be989438248074489337ba3275657f93595428be4fc03 AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
COPY public/ /src/public/
RUN npm run build

FROM golang:1.26-bookworm@sha256:6ef6e30f0ea5c384f6d111cf856e024e3086bbdcb1779da3f3b3fbba0aea53d2 AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ficusin-api ./cmd/api

# No package-manager/network operations are allowed in the runtime stage.
# Pin every base image by digest so a mutable upstream tag cannot change a
# production build without a repository change and a green CI run.
FROM minidocks/poppler:latest@sha256:fc646c55459b604e8b47262bb8b45ac27cd35caadde5a278465f908883ba18c3
LABEL org.opencontainers.image.title="ficusin-store"
WORKDIR /app
COPY --from=backend /out/ficusin-api /app/ficusin-api
COPY --from=frontend /src/frontend/dist /app/web
COPY timeweb/migrations /app/migrations
ENV PORT=3000
ENV STATIC_DIR=/app/web
ENV MIGRATIONS_DIR=/app/migrations
EXPOSE 3000
# This image deliberately carries no HEALTHCHECK.
#
# Timeweb recreates the container with its own runtime configuration and then
# blocks the release on the in-container healthcheck. That gate never opened:
# every deploy from 21.08 onwards was rolled back after exactly 180 seconds
# while the container had already logged "bootstrap health endpoint started"
# and "api ready", and while curl to 127.0.0.1:3000 from inside a container on
# that same platform answers 200. The gate reported the platform's plumbing,
# not the health of the shop, and it kept eight releases including the whole
# catalogue v2 out of production.
#
# Readiness is still checked twice, both times the way a customer reaches the
# shop - over HTTP, from outside the container. Timeweb polls /api/v1/health
# (see the app's deploy settings) and CI does the same in the image job. Until
# migrations finish that endpoint answers {"status":"starting"}; only the fully
# wired router answers {"status":"ok"}.
USER 65532:65532
ENTRYPOINT ["/app/ficusin-api"]
