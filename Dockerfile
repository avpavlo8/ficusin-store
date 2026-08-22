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
# swapped in, and on a live database DDL can legitimately wait for short-lived
# locks, so give startup enough runway instead of marking a healthy app failed.
#
# The probe deliberately performs no request. Timeweb blocks the release on
# this healthcheck, and an HTTP probe never passed there: every deploy from
# 21.08 onwards was rolled back after exactly 180 seconds while the container
# had already logged "bootstrap health endpoint started" and "api ready", and
# while curl to 127.0.0.1:3000 from inside a container on that same platform
# answers 200. Whatever the platform does to the probe's environment, reading
# /proc/net/tcp does not depend on it: 0BB8 is port 3000, so a match means our
# own process is listening. What the shop actually answers is asserted over
# HTTP from outside - by Timeweb (/api/v1/health in the app's deploy settings),
# by the image job in CI, and by the production smoke workflow.
HEALTHCHECK --interval=5s --timeout=3s --start-period=120s --retries=12 CMD grep -qi ':0BB8' /proc/net/tcp /proc/net/tcp6 2>/dev/null || exit 1
USER 65532:65532
ENTRYPOINT ["/app/ficusin-api"]
