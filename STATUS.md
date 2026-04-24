# Fling — Current Status (2026-04-24)

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

## Build environment quirks (important)

- **Use VS 2026 MSBuild** (`C:\Program Files\Microsoft Visual Studio\18\Professional\MSBuild\Current\Bin\MSBuild.exe`), not VS 2022. VS 2022 on the dev box is missing `Microsoft.Build.Packaging.Pri.Tasks.dll` (the `MSBuild\Microsoft\VisualStudio\v17.0\AppxPackage\` folder doesn't exist). VS 2026 has the equivalent at `v18.0\AppxPackage\`. Fix on a new machine: install VS 2026 Preview, or add the **MSIX Packaging Tools** component to whatever VS version you use.
- **Classic-mode vcpkg** at `C:\dev\vcpkg\installed\x64-windows\` is where AWS SDK + nlohmann-json + zlib live. Both `Fling.vcxproj` and `Fling.Tests.vcxproj` hard-reference absolute paths there. Vcpkg manifest-mode install (`vcpkg_installed\` under the repo) fails reproducibly on the dev box with `rename_or_delete` errors — probably Defender interference.
- **Defender exclusions recommended.** Scanning the build output and AWS DLLs every rebuild makes Defender eat 1–2 GB RAM and a CPU core. Add `E:\dev\fling` to Windows Security → Virus & threat protection → Exclusions.

---

## Build / run

```
# Build from repo root
"C:\Program Files\Microsoft Visual Studio\18\Professional\MSBuild\Current\Bin\MSBuild.exe" Fling/Fling.vcxproj -p:Configuration=Debug -p:Platform=x64

# Launch
Fling\x64\Debug\Fling.exe
```

A post-build step in `Fling.vcxproj` copies all vcpkg `x64-windows\[debug\]bin\*.dll` next to `Fling.exe` — Fling won't launch without those.

---

## Key implementation notes

These are the non-obvious fixes from the last session, worth knowing before touching things:

1. **XAML markup compiler output path** is pinned via `<XamlGeneratedOutputPath>$(ProjectDir)$(IntDir)</XamlGeneratedOutputPath>`. Without this, Pass2 and the Copy target interpret a relative `OutputPath` differently and `.xaml.g.h` / `.xbf` files land in mismatched locations.
2. **`Fling/App.xaml.g.h` is an override**, not auto-generated. The WindowsAppSDK 1.6 markup compiler emits an old-style `AppT` for the `ApplicationDefinition` that bypasses `App_base`, which breaks `make<implementation::App>()`. Our hand-written override lives at `Fling/App.xaml.g.h` (ProjectDir wins on the include search before the generator's copy in `$(IntDir)`). If you regenerate / upgrade WindowsAppSDK, check whether the upstream generator emits `App_base`-based code now and delete the override.
3. **`Fling/XamlMetaDataProvider.cpp`** is a hand-written adapter that includes the XAML-compiler-generated `.g.cpp` with pch. The generator also emits a file at `Generated Files\XamlMetaDataProvider.cpp` with the same content, but MSBuild silently deduplicates it out of the ClCompile list (collides with a CppWinRT auto-add). Keeping our copy at a unique path works around this.
4. **Explicit ClCompile entries** in `Fling.vcxproj`: `XamlTypeInfo.g.cpp`, `XamlTypeInfo.Impl.g.cpp`, `module.g.cpp`, `XamlMetaDataProvider.cpp` all need to be called out — CppWinRT doesn't auto-add them in this project setup.
5. **pch.h must `#include` every XAML implementation header** (`MainWindow.xaml.h`, the page headers, the dialog header) or `XamlTypeInfo.g.cpp`'s static_asserts fail.
6. **Preprocessor**: `NOMINMAX;WIN32_LEAN_AND_MEAN;USE_IMPORT_EXPORT` defined project-wide. Without `NOMINMAX` the Windows SDK's `max`/`min` macros garble `std::numeric_limits<...>::max()` in aws-sdk-cpp's TransferManager.h and cascade into dozens of fake parse errors.

---

## Code layout

```
Fling/
  App.xaml(.h/.cpp/.idl/.g.h)      App lifecycle, AWS init, redirected-activation handler
  App.xaml.g.h                     ← OVERRIDE, see note 2 above
  main.cpp                         AppInstance single-instance redirect
  XamlMetaDataProvider.cpp         ← ADAPTER, see note 3 above
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

Tests in `Fling.Tests/` — builds, and all tests compile except `GTEST_SKIP` usages in `S3UploaderIntegrationTests.cpp` (the NuGet gtest is 1.8.1, `GTEST_SKIP` is 1.10+). Bumping the gtest package is a one-line fix.

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
