# codemap

Repository: https://github.com/roshan-ican/codemap

`codemap` is a local browser map for Git repositories. It scans source files,
detects code connections, shows current or branch-level changes, and opens files
in VS Code from the map.

## Install From Source

```bash
git clone https://github.com/roshan-ican/codemap.git
cd codemap
cd frontend && npm install && npm run build
cd ..
go install -tags webembed .
```

## Use

Run inside any Git repository:

```bash
codemap
```

To review a branch after commits have already been pushed, compare against a
base revision:

```bash
codemap -base origin/main
```

The map is served locally on `127.0.0.1:7331`.

## Development

```bash
cd frontend && npm install && npm run build
cd ..
go run .
```

For release-style binaries with the UI embedded:

```bash
make build
```
