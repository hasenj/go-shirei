# ioshost

The UIKit host template used by `ios-run.sh` now lives at:

```
shirei/mobilerun/embed/ioshost/
```

That tree is **embedded** into the `mobilerun` binary and extracted to the
user cache on demand. Do not put per-app names or `libshirei.a` here — the
build always copies the template to a scratch `host-work` directory first.

Edit the template under `mobilerun/embed/ioshost/`, then rebuild `mobilerun`.
