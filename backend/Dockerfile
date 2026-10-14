# syntax=docker/dockerfile:1

# ---------- Stage 1: build ----------
# Full Go toolchain (~300MB). Used only to compile; it is thrown away afterwards.
FROM golang:1.26-alpine AS build

WORKDIR /src

# Copy dependency files first so Docker can cache the downloaded modules.
# As long as go.mod/go.sum do not change, this layer is reused and builds are fast.
# (go.sum* uses a glob so the build still works before go.sum exists.)
COPY go.mod go.sum* ./
RUN go mod download

# Now copy the source code (changes often, so it comes after the cached layer).
COPY . .

# CGO_ENABLED=0 -> pure static binary, no dependency on C libraries in the final image
# -ldflags="-s -w" -> strip debug symbols to make the binary smaller
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/api ./cmd/api

# ---------- Stage 2: runtime ----------
# Distroless static: no shell, no package manager, only CA certs + tzdata + a non-root user.
FROM gcr.io/distroless/static-debian13:nonroot

WORKDIR /app
COPY --from=build /out/api /app/api

# Run as an unprivileged user (uid 65532) instead of root.
USER nonroot:nonroot

EXPOSE 8080
ENTRYPOINT ["/app/api"]
