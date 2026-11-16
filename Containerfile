# Built only by CI (.github/workflows/deploy.yml); see DEPLOYMENT.md.
FROM docker.io/library/golang:1.27-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/site ./cmd/site

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/site /site
# Root inside a scratch image (no shell, no package manager) under Bunny's
# gVisor sandbox, so the persistent volume mounted at /data is writable
# whatever ownership the platform gives it.
ENV PORT=8080 DATA_DIR=/data
EXPOSE 8080
ENTRYPOINT ["/site"]
