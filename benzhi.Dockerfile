# Official Go image with the complete toolchain for both amd64 and arm64 builds.
FROM golang:1.22

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build ./...

CMD ["bash"]
