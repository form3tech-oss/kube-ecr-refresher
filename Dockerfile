FROM golang:1.12.9-buster AS builder
ENV GO111MODULE on
WORKDIR /go/src/github.com/form3tech-oss/kube-ecr-refresher
COPY go.mod go.sum ./
RUN go mod vendor
COPY main.go .
RUN go build -o /kube-ecr-refresher -v ./main.go

FROM gcr.io/distroless/base
USER nobody:nobody
WORKDIR /
COPY --from=builder /kube-ecr-refresher /kube-ecr-refresher
ENTRYPOINT ["/kube-ecr-refresher"]