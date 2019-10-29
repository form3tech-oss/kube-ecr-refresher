FROM golang:1.13.3 AS builder
ENV GO111MODULE on
WORKDIR /go/src/github.com/form3tech-oss/kube-ecr-refresher
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./main.go
COPY internal/ ./internal/
RUN go build -o /kube-ecr-refresher -v ./main.go

FROM gcr.io/distroless/base
USER nobody:nobody
WORKDIR /
COPY --from=builder /kube-ecr-refresher /kube-ecr-refresher
ENTRYPOINT ["/kube-ecr-refresher"]