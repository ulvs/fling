# Fling — Current Status (2026-10-14)

Handoff snapshot for continuing development on another machine.

---

## Where we are

Plan phases (from [fling-project-plan.md](fling-project-plan.md)):

| Phase | Status |
|---|---|
| 1 — Project setup & vcpkg | ✅ Done |
| 2 — Core library (ConfigManager, S3Uploader, HistoryManager, helpers) | ✅ Done |
| 3 — Explorer integration | 🟡 SendTo approach landed; MSIX context menu not started |
| 4 — WinUI 3 tray app | 🟡 Window + Settings + History wired; tray icon scaffolded but not called |
| 5 — Polish (multi-file, drag-drop, TransferManager, creds) | ⏳ Not started |

**The end-to-end pipeline works today**: configure S3 creds in Settings → click "Share a file..." on the History page → file uploads to MinIO → presigned URL lands on clipboard → toast notification → entry appears in History.

Verified live against MinIO at `https://s3.intebitter.se` / bucket `fling` / user `fling-app`. Policy has `s3:ListBucket`, `s3:PutObject`, `s3:GetObject`. (Missing `s3:AbortMultipartUpload` — only matters for cancelled large uploads, not blocking.)

---

## Build environment (important)

- **VS 18 (2026) MSBuild + `v145` toolset.** `C:\Program Files\Microsoft Visual Studio\18\Professional\MSBuild\Current\Bin\MSBuild.exe`. Do **not** go back to `v143`: VS 18 then redirects the C++ build to VS 2022's `VC\v170` targets, and the dev box's VS 2022 lacks the WindowsXaml and AppxPackage components. That redirect was the root cause of every XAML build hack from the spring (all removed in the 2026-10 upgrade).
- **Packages:** Windows App SDK 2.5.1, CppWinRT 3.0.260818.1, SDK BuildTools 10.0.26100.9169, WIL 1.0.260126.7.
- **Project shape matches the WinUI C++ template:** `ApplicationType=Windows Store`, `ApplicationTypeRevision=10.0`, `AppContainerApplication=false`, `DISABLE_XAML_GENERATED_MAIN` (we have our own single-instance `main.cpp`), `module.g.cpp` listed explicitly, `DependentUpon` on the XAML code-behind/idl items. Because we use `PackageReference` (not packages.config) we also need the `TargetFramework=native` monikers and `ResolveNuGetPackages=false`.
- **Classic-mode vcpkg** at `C:\dev\vcpkg\installed\x64-windows\` (aws-sdk-cpp, nlohmann-json, zlib, gtest 1.17). Both project files hard-reference these absolute paths — must change before open-sourcing. Manifest mode into the repo failed with `rename_or_delete` errors (probably Defender).
- **Defender exclusions recommended** for `E:\dev\fling`.

---

## Build / run / test

```
# Build from repo root
"C:\Program Files\Microsoft Visual Studio\18\Professional\MSBuild\Current\Bin\MSBuild.exe" Fling/Fling.vcxproj -restore -p:Configuration=Debug -p:Platform=x64

# Launch (output layout is per-project now)
Fling\x64\Debug\Fling\Fling.exe

# Tests (38 unit tests; MinIO integration tests skip unless `docker compose up -d` is running)
"...\MSBuild.exe" Fling.Tests/Fling.Tests.vcxproj -p:Configuration=Debug -p:Platform=x64
Fling.Tests\x64\Debug\Fling.Tests.exe
```

Post-build steps copy vcpkg `x64-windows\[debug\]bin\*.dll` next to both exes.

---

## Key implementation notes

1. **No `App.idl`.** As in the WinUI template, `App` is not a WinRT runtime class; `App.xaml.h` includes the XAML compiler's `App.xaml.g.h`.
2. Page `.xaml.cpp` files must **not** include `*.xaml.g.hpp` — those are produced by markup pass 2 after the main compile and are pulled in by `XamlTypeInfo.g.cpp`.
3. Generated code lives in `Fling\Generated Files\` (git-ignored); `Generated Files\Pages` and `\Dialogs` are on the include path so `X.g.h` can find `X.xaml.g.h`.
4. **Preprocessor**: `NOMINMAX;WIN32_LEAN_AND_MEAN;USE_IMPORT_EXPORT`. Without `NOMINMAX` the Windows `max`/`min` macros break aws-sdk-cpp's TransferManager.h.
5. **Unpackaged toast notifications are broken on WinAppSDK 2.x self-contained:** `AppNotificationManager::Register()` fails loading `Microsoft.WindowsAppRuntime.Insights.Resource.dll`. App catches it and carries on (no toast). Expected to be fixed by moving to MSIX.

---

## Code layout

```
Fling/
  App.xaml(.h/.cpp)                App lifecycle, AWS init, redirected-activation handler
  main.cpp                         AppInstance single-instance redirect
  MainWindow.xaml(.h/.cpp/.idl)    NavigationView + tray icon scaffolding
  Pages/
    SettingsPage.xaml(.h/.cpp/.idl)  S3 creds + SendTo toggle
    HistoryPage.xaml(.h/.cpp/.idl)   History list + "Share a file..." button
  Dialogs/
    UploadProgressDialog.xaml(.h/.cpp/.idl)  Scaffold, not used yet
  Core/
    ConfigManager.cpp              JSON round-trip for FlingConfig
    HistoryManager.cpp             JSON round-trip for UploadEntry[], 500-entry cap
    S3Uploader.cpp                 PutObject + TransferManager + presigned URL + HeadBucket
    ClipboardHelper.cpp            WinRT Clipboard
    ToastHelper.cpp                AppNotificationBuilder
    UploadCoordinator.cpp          Orchestrates config → upload → clipboard → toast → history
    SendToInstaller.cpp            Creates/removes %APPDATA%\Microsoft\Windows\SendTo\Fling.lnk
  Models/
    FlingConfig.h                  + to_json / from_json
    UploadEntry.h                  + to_json / from_json
  pch.h/.cpp                       WinRT + AWS + nlohmann + XAML impl headers
```

Tests in `Fling.Tests/` — gtest 1.17 from vcpkg; compiles `Core/ConfigManager`, `HistoryManager`, `S3Uploader` directly. `AwsTestEnvironment.cpp` does the process-wide `Aws::InitAPI`. `HistoryManagerTests.AddEntry_EnforcesMaxEntries` takes ~30 s (500 disk saves).

---

## Known TODOs (by priority)

### Immediate (to prove SendTo works)
- Toggle "Add Fling to the 'Send to' menu" on the Settings page with a real file on the destination machine; right-click → Send to → Fling → verify URL lands on clipboard. Single-instance redirect is in place, so subsequent Send-Tos reuse the running app.

### MSIX packaging (next big task, user asked for this)
- This is Phase 3 proper. Required for **top-level** Windows 11 context menu integration (`IExplorerCommand` registration via `Package.appxmanifest`'s `desktop4:FileExplorerContextMenus` extension).
- Scope:
  - Flip the vcxproj from `<WindowsPackageType>None</WindowsPackageType>` to a packaged configuration. Current project is an unpackaged WinUI 3 app.
  - Fill out `Package.appxmanifest` identity (Publisher needs to match the code-signing cert), capabilities, and the context-menu extension.
  - Create a self-signed dev cert, sign the package, install via `Add-AppxPackage -Register`.
  - Possibly implement an `IExplorerCommand` COM class (lightweight shell extension) or use the Verb/Clsid delegation approach to invoke `Fling.exe "%1"` directly.
  - Existing `App::ProcessFilePath` / `AppInstance` redirect code will keep working under MSIX — the new piece is the shell integration declaration.

### Phase 4 polish
- `MainWindow::SetupTrayIcon()` exists but is never called from the constructor; wire it up so the tray icon actually appears.
- Progress dialog during upload (currently the UI freezes during sync upload — workaround via `apartment_context` + `resume_background` in the History page's Share button handler, but CLI path still blocks).
- Implement Cancel button on upload progress.

### Phase 5
- Multi-file Send To (Windows passes multiple paths space-separated to the target — `CommandLineToArgvW` already handles this, just need to iterate).
- Drag-and-drop onto window or tray icon.
- Credential storage via Windows Credential Manager instead of plain-text JSON in `%LOCALAPPDATA%\Fling\config.json`.
- Add `s3:AbortMultipartUpload` to the MinIO policy before testing files >100 MB with mid-flight cancellation.

---

## Git

- Branch: `main`
- Remote: `https://github.com/ulvs/fling.git`
- This commit is the first real landing after the initial empty commit.
