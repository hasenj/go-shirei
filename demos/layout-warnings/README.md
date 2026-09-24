# Layout warnings

From the repository root:

```sh
SHIREI_LAYOUT_WARN=1 go run ./shirei/demos/layout-warnings
```

The left example uses `Grow(1), Extrinsic, Clip` inside a row. It receives
width but no height, hiding its content. The right example adds `Expand`
to receive the parent's height.

The terminal reports one warning for the left container, including its builder
location and sizes. **Recreate examples** gives the containers new identities,
allowing another warning. Ordinary frames do not repeat it.

Without `SHIREI_LAYOUT_WARN=1`, the same layout renders without warnings.
For a headless run, add `-png /tmp/layout-warnings.png`.

See the [tutorial](../../docs/tutorial.md#layout-warnings-on-stderr) for the
warning conditions and how to interpret builder locations.
