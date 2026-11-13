# Windows VC runtime

`vcruntime140.dll` is the Microsoft-signed x64 redistributable distributed in
the pinned official Camoufox 158.0.1-beta.41 Windows archive. Its file version
is 14.51.36247.0 and its SHA-256 is
`d1f4225df2cd877dbf130d5668a021dce3f94118455ff5ec952061c30afc9ce7`.
The pinned archive and its SHA-256 are recorded in `driver/bridge.mjs`.

The DLL is installed beside impit's native module so it loads even when the
machine does not have the Visual C++ Redistributable installed system-wide.
