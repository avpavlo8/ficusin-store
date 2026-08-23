FROM node:24-bookworm-slim@sha256:3638d9a6fe4030bd716be989438248074489337ba3275657f93595428be4fc03 AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
COPY public/ /src/public/
RUN npm run build

# Exportable target for production smoke. Building the expected bundle with
# the same pinned Node image as Timeweb avoids false hash mismatches caused by
# a different Node patch version on the GitHub runner.
FROM scratch AS frontend-dist
COPY --from=frontend /src/frontend/dist /

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
# blocks the release on "Waiting for container healthcheck to pass". That gate
# never opened. Eight releases, including the whole catalogue v2, were rolled
# back after exactly 180 seconds while the container had already logged
# "bootstrap health endpoint started" and "api ready". It is not the network:
# curl to 127.0.0.1:3000 from inside a container on that platform answers 200
# under the same uid 65532. It is not the probe either: a check that only reads
# /proc/net/tcp, and so cannot fail while the process listens, was rolled back
# the same way at the same 180 seconds. The platform simply never reads the
# result, so any HEALTHCHECK here keeps production frozen.
#
# Readiness is asserted three times over HTTP, from outside, the way a customer
# reaches the shop: Timeweb polls /api/v1/health (see the app's deploy
# settings), the image job in CI boots this image and waits for a ready
# response, and the production smoke workflow checks the live site after merge.
# Until migrations finish that endpoint answers {"status":"starting"}; only the
# fully wired router answers {"status":"ok"}.
USER 65532:65532
ENTRYPOINT ["/app/ficusin-api"]
