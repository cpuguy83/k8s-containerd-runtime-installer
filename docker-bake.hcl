group "default" {
    targets = ["wasi-shim-installer"]
}

target "wasi-shim-build" {
    context = "https://github.com/deislabs/runwasi.git"
    dockerfile-inline = <<EOF
FROM rust:1.64 AS build
WORKDIR /app
COPY . .
ENV RUSTFLAGS="-C target-feature=+crt-static"
RUN \
    --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/app/target/release/incremental \
    --mount=type=cache,target=/app/target/release/deps \
    cargo build --release --target x86_64-unknown-linux-gnu

FROM scratch
COPY --from=build /app/target/x86_64-unknown-linux-gnu/release/containerd-shim-wasmtime-v1 /
EOF
}

target "wasi-shim-installer" {
    args = {
        "RUNTIME_NAME" = "wasi"
        "RUNTIME_BINARY" = "containerd-shim-wasmtime-v1"
    }
    context = "."
    contexts = {
        "runtime" = "target:wasi-shim-build"
    }
    tags= ["cpuguy83/wasi-shim-installer:latest"]
    output = ["type=registry"]
}