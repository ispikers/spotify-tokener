FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /build

COPY go.mod ./

RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build -o spotify-tokener github.com/ispikers/spotify-tokener

RUN go list -deps -f '{{with .Module}}{{if .Dir}}{{.Path}} {{.Dir}}{{end}}{{end}}' . | sort -u | \
    while read path dir; do mkdir -p /licenses/$path && cp $dir/LICENSE* /licenses/$path/; done

FROM chromedp/headless-shell

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /licenses /usr/share/licenses
COPY --from=build /build/spotify-tokener /bin/spotify-tokener

ENTRYPOINT ["/bin/spotify-tokener"]
