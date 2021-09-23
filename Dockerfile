FROM golang:1.17-alpine as builder
RUN mkdir -p /app
COPY ./src /app
RUN apk --update --no-cache add gcc g++
RUN cd /app && go install && go build -o main.out

FROM alpine:3
RUN apk --update --no-cache add curl && rm -rf /var/cache/apk/*
RUN mkdir /app
COPY --from=builder /app/main.out /app/main.out

CMD ["/app/main.out"]
EXPOSE 80
