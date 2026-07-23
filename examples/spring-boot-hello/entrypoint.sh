#!/bin/sh
# The platform injects PORT (from app.yaml's `port`). Spring reads --server.port,
# so map one to the other here — this keeps app.yaml the single source of truth.
# `exec` makes the JVM PID 1 so it receives SIGTERM directly for graceful shutdown.
# -Xmx256m caps the heap; a rolling deploy briefly runs two copies, so budget peak.
exec java -Xmx256m -jar /app.jar --server.port="${PORT:-8080}"
