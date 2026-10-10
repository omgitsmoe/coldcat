default:
    @just --list

# The webui build embeds frontend/build; Node is not needed at runtime.
build:
    npm --prefix frontend ci
    npm --prefix frontend run build
    mkdir -p build
    go build -tags webui -o build/coldcat ./cmd/coldcat

# Build and serve the UI and API together on one loopback origin.
serve $db="coldcat.sqlite" $listen="127.0.0.1:8080": build
    ./build/coldcat --db "$db" serve --listen "$listen"

# Serve directly from the local frontend/build dir for development.
serve-dev:
    go run ./cmd/coldcat/ serve --assets ./frontend/build
