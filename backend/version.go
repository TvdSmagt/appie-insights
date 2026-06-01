package main

// Version is the application version. It is a var (not a const) so it can be
// overridden at build time via ldflags, e.g.:
//
//	go build -ldflags "-X main.Version=$(git describe --tags)"
//
// The default applies to plain `go build` / `go run` (i.e. unstamped builds);
// release builds override it with the git tag. See backend/Dockerfile.
var Version = "development"
