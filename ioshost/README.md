# ioshost

The UIKit host template used by `ios-run.sh` lives at:

```
shirei/cmd/shirei_mobilerun/embed/ioshost/
```

That tree is **embedded** into the `shirei_mobilerun` binary and extracted to
the user cache on demand. Do not put per-app names or `libshirei.a` here — the
build always copies the template to a scratch `host-work` directory first.

Edit the template under `cmd/shirei_mobilerun/embed/ioshost/`, then rebuild:

```
go install go.hasen.dev/shirei/cmd/shirei_mobilerun@latest
```
