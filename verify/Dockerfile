# 验收服务：编译独立 verify 二进制
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go .
RUN CGO_ENABLED=0 go build -buildvcs=false -o /out/verify .

FROM alpine:3.20
COPY --from=build /out/verify /usr/local/bin/verify
CMD ["verify"]
