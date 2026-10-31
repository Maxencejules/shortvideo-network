# Build the development chain binary. The image is local. It is not published.
# Go 1.26.10 matches docs/dependencies.md.
FROM golang:1.26.10-bookworm AS build
WORKDIR /src
COPY chain ./chain
COPY proto ./proto
WORKDIR /src/chain
RUN go build -o /out/svnd ./cmd/svnd

FROM debian:bookworm-slim
RUN useradd --create-home --uid 65532 svn
USER svn
COPY --from=build /out/svnd /usr/local/bin/svnd
EXPOSE 26656 26657 27101
ENTRYPOINT ["svnd"]
CMD ["start", "--home", "/home/svn"]
