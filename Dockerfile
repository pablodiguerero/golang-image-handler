FROM golang:1.14-alpine as builder
RUN mkdir -p /app
COPY ./src /app
RUN cd /app && go install && go build -o main.out

FROM alpine:3
RUN mkdir /app
COPY --from=builder /app/main.out /app/main.out

CMD ["/app/main.out"]
