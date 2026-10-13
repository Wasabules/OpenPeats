# Build Directory

The build directory is used to house all the build files and assets for your application. 

The structure is:

* bin - Output directory
* darwin - macOS specific files
* windows - Windows specific files

## Icon

`appicon.svg` is the source of the OpenPeats icon; the other icon files are
rendered from it and committed, so a build needs no SVG renderer.

- `appicon.png` — `appicon.svg` at 1024 × 1024. Wails builds the macOS icon
  from it.
- `windows/icon.ico` — 16, 20 and 24 px from `appicon-small.svg`, the same
  drawing redrawn on a 16 px grid so it stays legible; 32 to 256 px from
  `appicon.svg`. Every entry is PNG-compressed, as Wails' own default was.
  Wails would regenerate a missing `icon.ico` from `appicon.png` alone, losing
  the small drawing: replace the file, never delete it.
- The Linux package installs `appicon.svg` itself, as a scalable icon.
- The Android adaptive icon
  (`android/app/src/main/res/drawable/ic_launcher_*.xml`) carries the same
  paths in the same 1024-unit coordinates; a change to the drawing goes into
  both.

Render each size from the SVG directly rather than shrinking the 1024 px PNG,
which blurs the small ones.

## Mac

The `darwin` directory holds files specific to Mac builds.
These may be customised and used as part of the build. To return these files to the default state, simply delete them
and
build with `wails build`.

The directory contains the following files:

- `Info.plist` - the main plist file used for Mac builds. It is used when building using `wails build`.
- `Info.dev.plist` - same as the main plist file but used when building using `wails dev`.

## Windows

The `windows` directory contains the manifest and rc files used when building with `wails build`.
These may be customised for your application. To return these files to the default state, simply delete them and
build with `wails build`.

- `icon.ico` - The icon used for the application. This is used when building using `wails build`. If you wish to
  use a different icon, simply replace this file with your own. If it is missing, a new `icon.ico` file
  will be created using the `appicon.png` file in the build directory.
- `installer/*` - The files used to create the Windows installer. These are used when building using `wails build`.
- `info.json` - Application details used for Windows builds. The data here will be used by the Windows installer,
  as well as the application itself (right click the exe -> properties -> details)
- `wails.exe.manifest` - The main application manifest file.