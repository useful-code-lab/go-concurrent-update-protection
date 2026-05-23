# Шаг сборки
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o main .

# Шаг запуска (минимальный образ)
FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/main .
EXPOSE 8080
CMD ["./main"]
