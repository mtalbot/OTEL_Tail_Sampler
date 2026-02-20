# Gemini Project Configuration

## Project Info
- **Language:** Go
- **Name:** OTEL_Tail_Sampler
- **Description:** A service to collect, buffer and then either tail sample or provide rollups, supports all key otel signals

## General Instructions
- Structs and any implmented intreface should be in the same file
- Different components should be in seperate files
- Side effects should be minimized and a functional style followed
- No global variables
- Everything should have tests
- Each feature should have acceptance tests
- Any bug should have a regression test before fixing
- Always think first, create a user story in a markdown doc, in a folder called stories, ask for my consent and then perform the story
- Before committing anything always fully execute the build and tests

## Coding Style
- Use tabs for whitespace sepertation
- curly braces should start at the end of the line and be a seperate line for the closing brace
- lower camel case for private, upper cammel case for public

## Work Plan

The work plan is stored in @PLANNING.md

## Commands
- **build**: go build ./...
- **test**: go test ./...
- **format**: go fmt ./...
- **lint**: go vet ./...

## File Watching
- **Include**:
  - `**/*.go`
  - `go.mod`
  - `go.sum`
- **Exclude**:
  - `**/*_test.go`
