FROM golang:1.19 AS build
WORKDIR /root/src
COPY go.* ./
RUN \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download
COPY . .
ENV CGO_ENABLED=0
RUN \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o containerd-runtime-installer

FROM scratch as runtime

FROM scratch
COPY --from=build /root/src/containerd-runtime-installer /
COPY --from=runtime / /
ARG RUNTIME_NAME RUNTIME_BINARY
ENV RUNTIME_NAME=${RUNTIME_NAME} RUNTIME_BINARY=${RUNTIME_BINARY}
ENTRYPOINT ["/containerd-runtime-installer"]
