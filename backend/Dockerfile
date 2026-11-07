# Built from the repository root so the web client can be copied in:
#   docker build -f backend/Dockerfile .
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
COPY client/web /web
ENV WEB_ROOT=/web
EXPOSE 8080
ENTRYPOINT ["/server"]
