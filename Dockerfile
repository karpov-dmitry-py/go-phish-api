FROM golang:1.17 as build
COPY . /app
WORKDIR /app
RUN go mod download
RUN go build -o phish-api cmd/api/main.go

FROM alpine
WORKDIR /opt
COPY --from=build /app/phish-api /opt
COPY --from=build /app/configs/config.yaml /opt
RUN apk add --no-cache libc6-compat
EXPOSE 8080
CMD /opt/phish-api -cfg config.yaml