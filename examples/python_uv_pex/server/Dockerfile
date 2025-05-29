# No need for a python image as we bundle it in the binary
FROM debian:bookworm-slim

COPY ./dist/bin /app.bin

ENTRYPOINT /app.bin
