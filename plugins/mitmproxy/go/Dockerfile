# Personal single-node image (no cloud/team services).
FROM node:22-bookworm AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-bookworm AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist && cp -a web/dist/. internal/webui/dist/
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.Version=${VERSION}" -o /out/proxying ./cmd/proxying

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/proxying /proxying
USER nonroot:nonroot
EXPOSE 8080 9090
VOLUME ["/data"]
ENTRYPOINT ["/proxying"]
CMD ["-proxy-addr", "0.0.0.0:8080", "-api-addr", "0.0.0.0:9090", "-data-dir", "/data"]
