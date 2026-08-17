# App resources

Shirei apps keep non-code assets (icons, images, data files) in a **`Resources/`**
directory next to `package main`. The same paths work under `go run` and in a
desktop release package: call sites use `app.ResourcePath` and never hardcode
`.app/Contents/Resources` or similar layout details.

Desktop packaging via [`shirei_bundle`](../cmd/shirei_bundle/README.md) copies
that directory into the platform resource root. The runtime API is the source of
truth; the bundler follows the same convention.

Mobile resource packaging is not part of this path yet.

## Layout

```
myapp/
  main.go
  Resources/
    icon.png
    assets/
      …
```

Name the directory **`Resources`** (capital R). Nested folders inside it are
fine; paths passed to `ResourcePath` are relative to that root.

## API (`go.hasen.dev/shirei/app`)

```go
import app "go.hasen.dev/shirei/app"

app.SetupIcon(app.ResourcePath("icon.png"))
Image(app.ResourcePath("assets/photo.png"), Vec2{0, 120})

root := app.ResourcesDir() // absolute directory, or "" if none found
```

| Function | Role |
|----------|------|
| **`ResourcePath(name)`** | Join `name` under the resolved resources root |
| **`ResourcesDir()`** | Absolute path to that root, or `""` if unresolved |
| **`SetResourcesDir(dir)`** | Pin the root for this process (tests / odd layouts); empty clears the pin |

Environment override: **`SHIREI_RESOURCES`** — absolute or relative path to the
root. Useful in tests. Not a bundler setting.

## Resolution order

1. `SetResourcesDir` pin
2. `SHIREI_RESOURCES`
3. macOS `.app`: `Contents/Resources` next to the executable
4. `<exeDir>/Resources` when that directory exists (Linux/Windows packages, loose binaries)
5. Dev probe for `go run ./pkg` from a module or monorepo root:
   - Prefer `<cwd>/<main-package-base>/Resources` when that directory exists
     (main package path from build info, e.g. `gardener` → `./gardener/Resources`)
   - Else, if the cwd has exactly one immediate child with `Resources/`, use it
   - Else walk from the working directory and executable directory (and parents)
     looking for a `Resources` directory

   Package-local lookup runs before the parent walk so a shared monorepo-level
   `Resources/` (fonts, test data, …) does not shadow `<package>/Resources`.

Call sites always go through `ResourcePath` / `ResourcesDir`. Do not open
`Contents/Resources` or `<exeDir>/Resources` by hand.

## Packaging (`shirei_bundle`)

When you add an app in the bundler UI, the icon field defaults to
`Resources/icon.png` if that file exists (otherwise `icon.png` beside the
package). Prefer keeping the dock/launcher icon there so runtime
`SetupIcon(app.ResourcePath("icon.png"))` and packaging share one file.

When `<package>/Resources` exists, desktop builds copy its **contents** into the
platform resource root:

| Platform | Destination |
|----------|-------------|
| macOS | `App.app/Contents/Resources/` |
| Linux | `<exeDir>/Resources/` inside the tarball |
| Windows | `<exeDir>/Resources/` inside the zip |

No bundler config field: presence of the directory is enough. Details and CLI/GUI
workflow: [shirei_bundle README](../cmd/shirei_bundle/README.md).

## Tips

- Prefer `ResourcePath` at every load site (`Image`, `SetupIcon`, `os.ReadFile`, …).
- Large or generated blobs belong in `Resources/` too — they ship beside the
  binary without forcing a Go rebuild of unrelated code when only assets change.
- If `ResourcesDir()` is empty, `ResourcePath` returns the cleaned `name` alone;
  opens will fail until a root is found or pinned.
