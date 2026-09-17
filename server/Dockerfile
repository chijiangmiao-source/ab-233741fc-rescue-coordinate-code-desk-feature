# 编译 Go 服务（modernc.org/sqlite 为纯 Go 驱动，无需 CGO）
FROM golang:1.27-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/sars-server .

FROM alpine:3.21
RUN adduser -D -H appuser && mkdir -p /data && chown appuser /data
COPY --from=build /bin/sars-server /usr/local/bin/sars-server
ENV DB_PATH=/data/sars.db \
    PORT=8080
USER appuser
EXPOSE 8080
CMD ["sars-server"]
