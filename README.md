# codemap

Repository: https://github.com/roshan-ican/codemap

`codemap` is a local browser map for Git repositories. It scans source files,
detects code connections, shows current or branch-level changes, and opens files
in VS Code from the map.

It is built as a standalone command so you can install it once and run it inside
any Git project.

## Demo

<video src="./assets/final-code-map.mov" controls muted width="100%"></video>

[Watch the demo video](./assets/final-code-map.mov).

## Features

- Live local code map for the current Git repository.
- Go and TypeScript/JavaScript relationship detection.
- Frontend, backend, and test scopes.
- Colored connection threads for frontend, backend, tests, and cross-scope links.
- Area select mode for selecting multiple cards with a drag rectangle.
- AI context builder for one selected file or many selected files.
- Branch/base comparison with `-base`, useful after commits are already pushed.
- Recent commit activity, authors, and change summaries.
- VS Code file opening from the map.

## Install

### Install With Go

```bash
go install -tags webembed github.com/roshan-ican/codemap@latest
```

Make sure your Go binary folder is on your `PATH`. On most machines this is:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

Then check:

```bash
codemap -help
```

### Install From Source

```bash
git clone https://github.com/roshan-ican/codemap.git
cd codemap
cd frontend
npm install
npm run build
cd ..
go install -tags webembed .
```

## Use In Another Project

Go to any Git repository and run:

```bash
cd /path/to/your/project
codemap
```

`codemap` starts a local browser app at:

```text
http://127.0.0.1:7331
```

To review changes against a base branch, including changes that are already
pushed:

```bash
codemap -base origin/main
```

Common examples:

```bash
codemap -base main
codemap -base origin/main
codemap -base upstream/main
```

## Map Controls

Toolbar shortcuts:

- `1`: Pan
- `2`: Area select
- `3`: What calls what
- `4`: Changes together
- `5`: Both
- `6`: All files
- `7`: Frontend
- `8`: Backend
- `9`: Tests

Other controls:

- Use `Area select` to drag a rectangle around multiple cards.
- Click `Understand component` for one selected file.
- Click `Understand selection` for multiple selected files.
- Click `Copy AI context` to paste the generated context into Cursor, OpenCode,
  ChatGPT, Groq, or another LLM tool.
- Right-click the `Tests` scope card to pull test files into their own lane.
- Use `Hide` / `Show` on the commit timeline to collapse or expand it.

## Development

Requirements:

- Go 1.25 or newer
- Node.js and npm
- Git
- VS Code, if you want file-open integration

Set up the project:

```bash
git clone https://github.com/roshan-ican/codemap.git
cd codemap
npm install --prefix frontend
```

Run the frontend build:

```bash
npm run build --prefix frontend
```

Run tests:

```bash
go test ./...
go test -tags webembed ./...
```

Run locally while developing:

```bash
go run .
```

Build release-style binaries with embedded frontend assets:

```bash
make build
```

## Project Structure

```text
.
|-- frontend/                 # Svelte UI
|-- map_api.go                # map response, Git diff, activity, summaries
|-- map_web.go                # local HTTP server and browser app
|-- go_analyzer.go            # Go source relationship analyzer
|-- typescript_analyzer.go    # TS/JS import analyzer
|-- context.go                # selected-file AI context builder
`-- *_test.go                 # Go tests
```

## Contributing

Contributions are welcome! If you would like to contribute to this project,
please follow these steps:

1. Fork the repository.
2. Create a new branch for your feature or bug fix.
3. Make the necessary changes and commit them.
4. Push your changes to your fork.
5. Submit a pull request describing your changes.

Good first areas:

- Improve language analyzers.
- Add support for more file types.
- Improve the map layout.
- Add more useful AI context sections.
- Improve tests and fixtures.
- Add packaging, releases, or Homebrew support.

Before opening a pull request:

```bash
npm run build --prefix frontend
go test ./...
go test -tags webembed ./...
```

When changing the UI, commit both:

- the source files in `frontend/src/`
- the rebuilt files in `frontend/dist/`

Pull request guidelines:

- Keep changes focused.
- Explain what problem the PR solves.
- Include screenshots or a short screen recording for UI changes.
- Add or update tests when changing analyzer, Git, server, or context behavior.
- Do not include secrets, API keys, local paths, or `node_modules`.

## Releasing

Build all configured platform binaries:

```bash
make build
```

The output goes into:

```text
bin/
```

## Making The Repo Public

For other people to install with:

```bash
go install -tags webembed github.com/roshan-ican/codemap@latest
```

the GitHub repository must be public, or they must have access to the private
repo.

To publish it as an open-source project, make the repository public in GitHub
settings, then add topics and a short description on the GitHub repo page.

## License

This project is released under the MIT License. See [LICENSE](./LICENSE).
