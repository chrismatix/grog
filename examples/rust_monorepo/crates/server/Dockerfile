# syntax=docker/dockerfile:1.7
FROM rust:1-slim-bookworm AS builder
WORKDIR /src
COPY . .
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/src/target \
    cargo build --release --locked -p server && cp target/release/server /server

FROM debian:bookworm-slim
COPY --from=builder /server /usr/local/bin/server
ENV ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["server"]
