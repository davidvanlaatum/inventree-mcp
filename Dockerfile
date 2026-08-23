FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETPLATFORM

COPY ${TARGETPLATFORM}/inventree-mcp /usr/bin/inventree-mcp

USER nonroot:nonroot
ENTRYPOINT ["/usr/bin/inventree-mcp"]
CMD ["serve", "--transport", "http", "--listen", "0.0.0.0:28686", "--path", "/mcp"]
