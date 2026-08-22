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
# Startup applies pending PostgreSQL migrations before the full router is
# swapped in. On a live database DDL can legitimately wait for short-lived
# locks, so give startup enough runway instead of marking a healthy app as
# failed.
#
# The probe must not assume that the container's own loopback is usable.
# Timeweb recreates this container with its own networking and then blocks the
# release on this healthcheck; a request to 127.0.0.1 never succeeded there, so
# every deploy from 21.08 onwards was rolled back after exactly 180 seconds
# while the application had already bound its port and logged "api ready".
# CI could not catch it because it boots the image with --network host, where
# 127.0.0.1 is the host's loopback. Falling back to the container's own address
# keeps this a real health check instead of a report about the platform's
# network layout.
HEALTHCHECK --interval=5s --timeout=3s --start-period=120s --retries=12 CMD \
  wget -qO- http://127.0.0.1:3000/api/v1/health >/dev/null 2>&1 \
  || wget -qO- "http://$(hostname -i | cut -d' ' -f1):3000/api/v1/health" >/dev/null 2>&1 \
  || exit 1
USER 65532:65532
ENTRYPOINT ["/app/ficusin-api"]
