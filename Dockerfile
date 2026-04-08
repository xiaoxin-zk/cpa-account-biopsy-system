FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY . .
RUN go mod download && CGO_ENABLED=0 GOOS=linux go build -o /out/cpa-account-biopsy-system ./cmd/cpa-account-biopsy-system

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/cpa-account-biopsy-system /app/cpa-account-biopsy-system
EXPOSE 18317
CMD ["/app/cpa-account-biopsy-system"]
