#!/usr/bin/env bash
# dev.sh - AuxiTalk Dashboard development helper
# Usage: ./dev.sh <command>

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Config
BINARY_NAME="dashboard"
PORT="${DASHBOARD_PORT:-8080}"
PID_FILE=".dashboard.pid"
LOG_FILE="dashboard.log"

print_header() {
    echo -e "${BLUE}▶ $1${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}! $1${NC}"
}

usage() {
    echo "AuxiTalk Dashboard - Development Helper"
    echo ""
    echo "Usage: ./dev.sh <command>"
    echo ""
    echo "Commands:"
    echo "  build     Generate templates and build binary"
    echo "  run       Kill previous, rebuild and run in foreground"
    echo "  dev       Alias for 'run'"
    echo "  stop      Stop running dashboard"
    echo "  clean     Remove build artifacts and logs"
    echo "  status    Show if dashboard is running"
    echo "  logs      Tail dashboard logs"
    echo "  test      Run go tests"
    echo "  templ     Generate templ files only"
    echo "  help      Show this help"
    echo ""
}

templ_gen() {
    print_header "Generating Templ files..."
    if command -v templ &> /dev/null; then
        templ generate
    else
        $(go env GOPATH)/bin/templ generate
    fi
    print_success "Templates generated"
}

build() {
    print_header "Building dashboard..."
    templ_gen
    go build -o "$BINARY_NAME" ./cmd/dashboard
    print_success "Build complete: ./$BINARY_NAME"
}

run() {
    stop 2>/dev/null || true

    build

    print_header "Starting dashboard on port $PORT..."
    DASHBOARD_PORT="$PORT" nohup ./$BINARY_NAME > "$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"
    sleep 1

    if kill -0 $(cat "$PID_FILE") 2>/dev/null; then
        print_success "Dashboard started (PID: $(cat $PID_FILE))"
        echo "  Access: http://localhost:$PORT"
        echo "  Logs:   tail -f $LOG_FILE"
    else
        print_error "Failed to start dashboard"
        cat "$LOG_FILE"
        exit 1
    fi
}

stop() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if kill -0 "$PID" 2>/dev/null; then
            print_header "Stopping dashboard (PID: $PID)..."
            kill "$PID"
            sleep 1
            print_success "Dashboard stopped"
        else
            print_warning "Process $PID not running"
        fi
        rm -f "$PID_FILE"
    else
        print_warning "No PID file found"
    fi
}

clean() {
    print_header "Cleaning build artifacts..."
    rm -f "$BINARY_NAME" "$PID_FILE" "$LOG_FILE"
    rm -f internal/templates/*_templ.go
    print_success "Clean complete"
}

status() {
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if kill -0 "$PID" 2>/dev/null; then
            print_success "Dashboard is running (PID: $PID, Port: $PORT)"
        else
            print_warning "PID file exists but process is not running"
        fi
    else
        print_warning "Dashboard is not running"
    fi
}

logs() {
    if [ -f "$LOG_FILE" ]; then
        tail -f "$LOG_FILE"
    else
        print_error "Log file not found: $LOG_FILE"
        exit 1
    fi
}

test() {
    print_header "Running tests..."
    go test ./...
    print_success "Tests passed"
}

case "${1:-help}" in
    build) build ;;
    run|dev) run ;;
    stop) stop ;;
    clean) clean ;;
    status) status ;;
    logs) logs ;;
    test) test ;;
    templ) templ_gen ;;
    help|*) usage ;;
esac
