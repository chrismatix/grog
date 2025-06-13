FROM alpine:3.18
COPY backend/bin/app /usr/local/bin/app
COPY frontend/dist /usr/local/share/frontend
ENV STATIC_DIR=/usr/local/share/frontend
CMD ["/usr/local/bin/app"]
