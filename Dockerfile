FROM --platform=$BUILDPLATFORM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/server ./cmd/server && mkdir -p /out/data

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/server /server
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
WORKDIR /data
VOLUME ["/data"]
EXPOSE 5000
ENTRYPOINT ["/server"]
CMD ["-config", "/config/config.json"]
